#!/usr/bin/env python3
"""Stress test for the bomberman sample.

Spawns a 3-node / 2-gate cluster with an auto-loaded MatchmakingQueue,
then drives N concurrent clients through repeated JoinQueue → wait-for-
match-to-end → JoinQueue cycles. Matches are configured to end as
draws after BOMBERMAN_MATCH_TIME_LIMIT_TICKS (default 50 = 5 s) so
the loop turns over quickly and we exercise many end-of-match
RecordMatchResult reliable calls per CI window.

Invariants:

1. **Exactly-once stat updates**: each JoinQueue cycle increments the
   player's total_matches by exactly 1. Reliable-call dedup is the only
   thing keeping this true — if Match.recordResults retried without
   stable call_ids, total_matches would jump by 2 or more.

2. **Cycle accounting**: each client's recorded cycle count equals the
   final total_matches read from the server. Cumulative across all
   clients, the matchmaker's view should be consistent.

3. **No abandoned players**: every client that completed at least one
   cycle has total_matches > 0 at the end.

Postgres + etcd must be running (reliable-call dedup persists in
Postgres; cluster coordination uses etcd).
"""

import argparse
import os
import random
import signal
import socket
import sys
import threading
import time
import urllib.error
import urllib.request
from collections import defaultdict
from dataclasses import dataclass, field
from pathlib import Path
from typing import Dict, List, Optional

# --- path bootstrap ---------------------------------------------------
REPO_ROOT = Path(__file__).parent.parent.parent.resolve()
sys.path.insert(0, str(REPO_ROOT))
COMMON_DIR = REPO_ROOT / 'samples' / 'common'
sys.path.insert(0, str(COMMON_DIR))
CHAT_DIR = REPO_ROOT / 'tests' / 'samples' / 'chat'
sys.path.insert(0, str(CHAT_DIR))
BOMBERMAN_DIR = Path(__file__).parent.resolve()
sys.path.insert(0, str(BOMBERMAN_DIR))

from Inspector import Inspector
from Gateway import Gateway
from BombermanServer import BombermanServer, ServerStopTimeout

from client.goverseclient_python.client import Client, ClientOptions
from samples.bomberman.proto import bomberman_pb2

POSTGRES_HOST = "127.0.0.1"
POSTGRES_PORT = 5432

NUM_NODES = 3
NUM_GATES = 2

# Stress-mode match length: short enough that clients turn over many
# matches per minute. The bomberman server reads this env var on
# start (BombermanServer.py forwards it to the subprocess via
# inheriting os.environ).
DEFAULT_MATCH_TIME_LIMIT_TICKS = 50

# Polling cadence on Player.GetStats while we wait for total_matches
# to advance after a JoinQueue. Low frequency is fine — match end is
# a slow event from the client's perspective.
STATS_POLL_INTERVAL = 0.5
# How long we'll wait for a single match to conclude before declaring
# the cycle stuck. Generous: server-side time limit is 5 s by default
# plus end-of-match RecordMatchResult retry budget (60 s) plus padding.
MAX_MATCH_WAIT_SECONDS = 90.0

# Inter-cycle pause so clients don't all hammer JoinQueue in lockstep.
MIN_REJOIN_DELAY = 0.0
MAX_REJOIN_DELAY = 0.5


def wait_for_cluster_ready(gate_port: int, timeout: float = 60.0) -> bool:
    """Poll Player.GetStats for a probe player until the cluster is ready.

    The gate and nodes start asynchronously — shards have a TargetNode
    assigned by the leader but CurrentNode is empty until each node claims
    ownership. During that window every CallObject returns an error. This
    function spins until one GetStats call succeeds (or timeout expires),
    ensuring clients don't start generating rpc_errors while the cluster
    is still warming up.
    """
    probe_player = "stress-player-000"
    req = bomberman_pb2.GetPlayerStatsRequest()
    deadline = time.time() + timeout
    attempt = 0
    print(f"Waiting for cluster to be ready (probe: Player-{probe_player} via port {gate_port})...")
    while time.time() < deadline:
        attempt += 1
        client: Optional[Client] = None
        try:
            client = Client([f"localhost:{gate_port}"], ClientOptions(call_timeout=3.0))
            client.connect(timeout=3.0)
            resp_any = client.call_object(
                "Player", f"Player-{probe_player}", "GetStats", req, timeout=3.0
            )
            if resp_any is not None:
                elapsed = timeout - (deadline - time.time())
                print(f"✅ Cluster ready after {elapsed:.1f}s ({attempt} probe(s))")
                return True
        except Exception:
            pass
        finally:
            if client is not None:
                try:
                    client.close()
                except Exception:
                    pass
        time.sleep(0.5)
    print(f"❌ Cluster not ready after {timeout}s ({attempt} probes)")
    return False


def probe_http_gate(http_url: str, timeout: float = 5.0) -> bool:
    """Open an SSE connection to the gate's /api/v1/events/stream and
    assert we get the expected `event: register` greeting within timeout
    seconds. Catches the failure mode where a gate config forgets to
    set http_addr (gate runs gRPC-only, the web UI hangs on
    'Connecting…'). The stress test itself uses gRPC, so without this
    probe the HTTP path can rot silently between releases.
    """
    sse_url = http_url.rstrip("/") + "/api/v1/events/stream"
    print(f"Probing HTTP/SSE at {sse_url} (timeout={timeout}s)...")
    try:
        req = urllib.request.Request(sse_url, headers={"Accept": "text/event-stream"})
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            if resp.status != 200:
                print(f"❌ HTTP/SSE: gate responded with status {resp.status}")
                return False
            deadline = time.time() + timeout
            while time.time() < deadline:
                line = resp.readline()
                if not line:
                    break
                if line.strip() == b"event: register":
                    print("✅ HTTP/SSE: gate produced the register event")
                    return True
            print("❌ HTTP/SSE: opened stream but no register event within timeout")
            return False
    except urllib.error.URLError as e:
        print(f"❌ HTTP/SSE: gate at {sse_url} not reachable: {e}")
        print("   Hint: ensure the first gate in stress_config.yml has http_addr set.")
        return False
    except Exception as e:
        print(f"❌ HTTP/SSE probe failed: {e}")
        return False


def ensure_postgres_ready() -> bool:
    try:
        with socket.create_connection((POSTGRES_HOST, POSTGRES_PORT), timeout=1.0):
            pass
    except OSError as e:
        print(f"❌ Postgres not reachable at {POSTGRES_HOST}:{POSTGRES_PORT}: {e}")
        print("   Local dev: run `docker compose up -d postgres`.")
        print("   CI: ensure the workflow provisions Postgres before this step.")
        return False
    print(f"✅ Postgres reachable on {POSTGRES_HOST}:{POSTGRES_PORT}")
    return True


@dataclass
class ClientStats:
    cycles_completed: int = 0
    rpc_errors: int = 0
    timeouts: int = 0
    invariant_violations: int = 0
    final_total_matches: Optional[int] = None
    final_wins: Optional[int] = None
    final_losses: Optional[int] = None


class StressClient:
    """One stress-test client. Joins queue → waits for stat increment → repeats."""

    def __init__(self, client_id: int, gate_port: int):
        self.client_id = client_id
        self.gate_port = gate_port
        self.player_id = f"stress-player-{client_id:03d}"
        self.client: Optional[Client] = None
        self._thread: Optional[threading.Thread] = None
        self.stats = ClientStats()

    def connect(self) -> None:
        self.client = Client([f"localhost:{self.gate_port}"], ClientOptions(call_timeout=15.0))
        self.client.connect(timeout=10.0)

    def close(self) -> None:
        if self.client is not None:
            try:
                self.client.close()
            except Exception:
                pass
            self.client = None

    def start(self, stop_event: threading.Event) -> None:
        self._thread = threading.Thread(target=self._run, args=(stop_event,), daemon=True)
        self._thread.start()

    def join(self, timeout: float = 30.0) -> None:
        if self._thread is not None:
            self._thread.join(timeout=timeout)

    def _run(self, stop_event: threading.Event) -> None:
        while not stop_event.is_set():
            try:
                self._one_cycle(stop_event)
            except Exception as e:
                self.stats.rpc_errors += 1
                print(f"[client-{self.client_id}] cycle error: {e}")
            time.sleep(random.uniform(MIN_REJOIN_DELAY, MAX_REJOIN_DELAY))

    def _one_cycle(self, stop_event: threading.Event) -> None:
        # Capture pre-join total_matches so we can detect the increment.
        before = self._fetch_total_matches()
        if before is None:
            return  # transient: counted as rpc_error inside fetch

        # Join the queue.
        join_req = bomberman_pb2.JoinQueueRequest(
            player_id=self.player_id, client_id=self.client.client_id
        )
        resp_any = self.client.call_object(
            "MatchmakingQueue", "MatchmakingQueue", "JoinQueue", join_req, timeout=10.0
        )
        if resp_any is None:
            self.stats.rpc_errors += 1
            return
        resp = bomberman_pb2.JoinQueueResponse()
        resp_any.Unpack(resp)
        if not resp.ok:
            self.stats.rpc_errors += 1
            print(f"[client-{self.client_id}] JoinQueue rejected: {resp.reason}")
            return

        # Wait for total_matches to advance — that's our "match ended"
        # signal. Bounded by MAX_MATCH_WAIT_SECONDS so a stuck cycle
        # surfaces as a timeout rather than hanging the whole run.
        deadline = time.time() + MAX_MATCH_WAIT_SECONDS
        while time.time() < deadline and not stop_event.is_set():
            time.sleep(STATS_POLL_INTERVAL)
            after = self._fetch_total_matches()
            if after is None:
                continue
            if after > before:
                # The killer invariant: exactly one increment per cycle.
                # If reliable-call dedup is broken, the recordResults
                # retry path could fire RecordMatchResult more than once
                # and after - before would exceed 1.
                if after - before != 1:
                    self.stats.invariant_violations += 1
                    print(
                        f"[client-{self.client_id}] EXACTLY-ONCE VIOLATION: "
                        f"player {self.player_id} total_matches jumped from {before} to {after} "
                        f"after one JoinQueue cycle (expected +1)"
                    )
                self.stats.cycles_completed += 1
                return
        if not stop_event.is_set():
            self.stats.timeouts += 1
            print(f"[client-{self.client_id}] cycle timeout (waited {MAX_MATCH_WAIT_SECONDS}s)")

    def _fetch_total_matches(self) -> Optional[int]:
        if self.client is None:
            return None
        req = bomberman_pb2.GetPlayerStatsRequest()
        try:
            resp_any = self.client.call_object(
                "Player", f"Player-{self.player_id}", "GetStats", req, timeout=5.0
            )
        except Exception as e:
            self.stats.rpc_errors += 1
            print(f"[client-{self.client_id}] GetStats failed: {e}")
            return None
        if resp_any is None:
            return None
        resp = bomberman_pb2.GetPlayerStatsResponse()
        resp_any.Unpack(resp)
        return int(resp.stats.total_matches)

    def fetch_final_stats(self) -> None:
        if self.client is None:
            return
        req = bomberman_pb2.GetPlayerStatsRequest()
        try:
            resp_any = self.client.call_object(
                "Player", f"Player-{self.player_id}", "GetStats", req, timeout=5.0
            )
        except Exception as e:
            print(f"[client-{self.client_id}] final GetStats failed: {e}")
            return
        if resp_any is None:
            return
        resp = bomberman_pb2.GetPlayerStatsResponse()
        resp_any.Unpack(resp)
        self.stats.final_total_matches = int(resp.stats.total_matches)
        self.stats.final_wins = int(resp.stats.wins)
        self.stats.final_losses = int(resp.stats.losses)


def print_stats(clients: List[StressClient]) -> None:
    cycles  = sum(c.stats.cycles_completed for c in clients)
    errors  = sum(c.stats.rpc_errors for c in clients)
    tos     = sum(c.stats.timeouts for c in clients)
    viol    = sum(c.stats.invariant_violations for c in clients)
    print(
        f"\ncycles={cycles}  rpc_errors={errors}  "
        f"timeouts={tos}  exactly_once_violations={viol}"
    )


def assert_invariants(clients: List[StressClient]) -> bool:
    print("\n" + "=" * 80)
    print("INVARIANT CHECK")
    print("=" * 80)
    failures = 0
    for c in clients:
        c.fetch_final_stats()
        s = c.stats
        if s.invariant_violations > 0:
            print(f"❌ {c.player_id}: {s.invariant_violations} exactly-once violations during run")
            failures += 1
            continue
        if s.cycles_completed == 0:
            print(f"⚠️  {c.player_id}: never completed a cycle (errors={s.rpc_errors} timeouts={s.timeouts})")
            # Not a hard failure — could be flake on a small run.
            continue
        if s.final_total_matches is None:
            print(f"❌ {c.player_id}: could not fetch final stats")
            failures += 1
            continue
        # Cycle accounting: each completed cycle must map to exactly one
        # match in Player.total_matches. We allow server total to exceed
        # cycles_completed by at most 1 — that handles the end-of-test
        # race where a match ends just as the stop signal fires and the
        # client poll loop exits before observing the increment. A real
        # dedup regression would produce invariant_violations > 0 (caught
        # above) or a gap larger than 1.
        drift = s.final_total_matches - s.cycles_completed
        if drift < 0 or drift > 1:
            print(
                f"❌ {c.player_id}: cycles_completed={s.cycles_completed} but server "
                f"total_matches={s.final_total_matches} (drift={drift:+d} indicates double or missed credit)"
            )
            failures += 1
            continue
        if drift == 1:
            print(
                f"⚠️  {c.player_id}: {s.cycles_completed} cycles, server total={s.final_total_matches} "
                f"(+1 end-of-test boundary match — not a dedup bug)"
            )
        else:
            print(
                f"✅ {c.player_id}: {s.cycles_completed} cycles, "
                f"server total={s.final_total_matches} wins={s.final_wins} losses={s.final_losses}"
            )
        # Data invariant from the proto: total_matches == wins + losses.
        if s.final_total_matches != (s.final_wins or 0) + (s.final_losses or 0):
            print(
                f"❌ {c.player_id}: total_matches={s.final_total_matches} != "
                f"wins({s.final_wins}) + losses({s.final_losses})"
            )
            failures += 1
            continue
    return failures == 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--duration", type=int, default=60)
    parser.add_argument("--clients", type=int, default=8)
    parser.add_argument("--config", default=str(BOMBERMAN_DIR / "stress_config.yml"))
    parser.add_argument("--match-ticks", type=int, default=DEFAULT_MATCH_TIME_LIMIT_TICKS,
                        help="Per-match tick limit. 50 = 5 s; 1800 = 3 min (production).")
    args = parser.parse_args()

    if not ensure_postgres_ready():
        return 1

    config_file = Path(args.config).resolve()
    if not config_file.is_file():
        print(f"❌ config file not found: {config_file}")
        return 1

    # Forward the short-match override into the bomberman server
    # subprocess via the inherited environment.
    os.environ["BOMBERMAN_MATCH_TIME_LIMIT_TICKS"] = str(args.match_ticks)

    import yaml
    with open(config_file) as f:
        cfg = yaml.safe_load(f)
    node_ids = [n["id"] for n in cfg["nodes"]][:NUM_NODES]
    gate_ids = [g["id"] for g in cfg["gates"]][:NUM_GATES]
    gate_ports = [int(g["grpc_addr"].rsplit(":", 1)[1]) for g in cfg["gates"]][:NUM_GATES]
    # First gate's http_addr (if any) is what the web UI talks to.
    # Used by the HTTP/SSE smoke probe below.
    http_gate_url = None
    for g in cfg["gates"][:NUM_GATES]:
        addr = g.get("http_addr") or ""
        if not addr:
            continue
        host, _, port = addr.rpartition(":")
        if host in ("", "0.0.0.0", "*"):
            host = "127.0.0.1"
        http_gate_url = f"http://{host}:{port}"
        break

    inspector: Optional[Inspector] = None
    servers: List[BombermanServer] = []
    gateways: List[Gateway] = []
    clients: List[StressClient] = []
    stop_event = threading.Event()

    shutting_down = False

    def shutdown():
        nonlocal shutting_down
        if shutting_down:
            return
        shutting_down = True
        print("\n--- SHUTDOWN ---")
        stop_event.set()
        for c in clients:
            try:
                c.join(timeout=10.0)
            except Exception:
                pass
            c.close()
        for gw in gateways:
            try:
                gw.close()
            except Exception as e:
                print(f"gateway close error: {e}")
        for srv in servers:
            try:
                srv.close()
            except ServerStopTimeout as e:
                print(f"❌ {e}")
            except Exception as e:
                print(f"server close error: {e}")
        if inspector is not None:
            try:
                inspector.close()
            except Exception as e:
                print(f"inspector close error: {e}")

    def handle_signal(signum, frame):
        shutdown()
        sys.exit(1)

    signal.signal(signal.SIGINT, handle_signal)
    signal.signal(signal.SIGTERM, handle_signal)

    try:
        print("=" * 80)
        print(f"STARTING CLUSTER (match_ticks={args.match_ticks})")
        print("=" * 80)
        inspector = Inspector(config_file=str(config_file))
        inspector.start()
        if not inspector.wait_for_ready(timeout=30):
            print("❌ Inspector failed to start")
            return 1

        for i, node_id in enumerate(node_ids):
            srv = BombermanServer(server_index=i, config_file=str(config_file), node_id=node_id)
            srv.start()
            servers.append(srv)
        for srv in servers:
            if not srv.wait_for_ready(timeout=30):
                print(f"❌ {srv.name} failed to start")
                return 1

        time.sleep(5)

        for gid in gate_ids:
            gw = Gateway(config_file=str(config_file), gate_id=gid)
            gw.start()
            gateways.append(gw)

        if http_gate_url is not None:
            if not probe_http_gate(http_gate_url):
                print("❌ HTTP gate probe failed — aborting before clients connect")
                return 1

        if not wait_for_cluster_ready(gate_ports[0]):
            print("❌ Cluster not ready — aborting before clients connect")
            return 1

        print("\n" + "=" * 80)
        print(f"LAUNCHING {args.clients} CLIENTS FOR {args.duration}s")
        print("=" * 80)
        for i in range(args.clients):
            c = StressClient(i, gate_ports[i % len(gate_ports)])
            c.connect()
            clients.append(c)
        for c in clients:
            c.start(stop_event)

        deadline = time.time() + args.duration
        while time.time() < deadline:
            time.sleep(5)
            print_stats(clients)

        stop_event.set()
        for c in clients:
            c.join(timeout=15.0)

        print_stats(clients)

        if not assert_invariants(clients):
            print("\n❌ INVARIANT CHECK FAILED")
            return 1
        print("\n✅ bomberman stress test passed")
        return 0
    finally:
        shutdown()


if __name__ == "__main__":
    sys.exit(main())
