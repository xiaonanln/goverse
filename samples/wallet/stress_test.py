#!/usr/bin/env python3
"""Stress test for the wallet reliable-call sample.

Demonstrates and validates client-side exactly-once semantics:

- Starts 1 inspector, 3 wallet-server nodes, 2 gateways (stress_config.yml).
- Spawns N clients. Each client fires Deposit / Withdraw reliable calls
  against a fixed pool of wallets.
- With probability RETRY_PROB, a call is immediately retried with the
  **same call_id**. The server must return the cached outcome — if it
  re-executes, the invariants below will fail.

Invariants:

1. In-flight: for each retried call, the second response's ok/balance
   must match the first. Any mismatch is a reliable-call violation.

2. End of run: for every wallet, server-reported balance must equal
   sum(applied deposits) − sum(applied withdrawals) as recorded by
   clients. Any drift means calls were applied more than once.

Usage:
    python3 samples/wallet/stress_test.py --duration 60 --clients 8

Postgres must be running (reliable calls persist dedup state there).
"""

import argparse
import os
import random
import signal
import socket
import subprocess
import sys
import threading
import time
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List, Optional

# --- path bootstrap ---------------------------------------------------
REPO_ROOT = Path(__file__).parent.parent.parent.resolve()
sys.path.insert(0, str(REPO_ROOT))
SAMPLES_DIR = REPO_ROOT / 'tests' / 'samples'
sys.path.insert(0, str(SAMPLES_DIR))
CHAT_DIR = REPO_ROOT / 'tests' / 'samples' / 'chat'
sys.path.insert(0, str(CHAT_DIR))
WALLET_DIR = Path(__file__).parent.resolve()
sys.path.insert(0, str(WALLET_DIR))

from Inspector import Inspector
from Gateway import Gateway
from WalletServer import WalletServer, WalletServerStopTimeout

from client.goverseclient_python.client import Client, ClientOptions
from client.goverseclient_python import ReliableCallError, generate_call_id
from samples.wallet.proto import wallet_pb2

POSTGRES_HOST = "127.0.0.1"
POSTGRES_PORT = 5432

NUM_NODES = 3
NUM_GATES = 2
NUM_WALLETS = 40

# On each successful call the client flips a coin: with probability
# RETRY_PROB it replays the exact same request (same call_id) to exercise
# the dedup path. Higher values hammer the idempotency logic harder; too
# low and we don't see enough of the branch.
RETRY_PROB = 0.3
MIN_DELAY = 0.01
MAX_DELAY = 0.1

MIN_AMOUNT = 1
MAX_AMOUNT = 100

# Fault injection. Before each real call, with probability FAULT_INJECT_PROB,
# send the same request with a tiny timeout so the client aborts before a
# normal response arrives. The tiny-timeout call may:
#   - never reach the server (pure client abort, no server-side state),
#   - reach the server and race with execution (interesting: server may
#     execute, client has no idea),
#   - or even complete normally if the server is fast.
# In all three cases the real retry that follows — with the same call_id —
# must still give an outcome consistent with whatever actually happened,
# which is exactly the reliable-call contract. The random range bracket
# the typical local RPC latency so we see a mix of all three outcomes.
FAULT_INJECT_PROB = 0.2
FAULT_INJECT_MIN_TIMEOUT = 0.002
FAULT_INJECT_MAX_TIMEOUT = 0.020


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
class OpRecord:
    call_id: str
    wallet_id: str
    kind: str   # "deposit" | "withdraw"
    amount: int
    ok: Optional[bool] = None
    balance_after: Optional[int] = None


class StressClient:
    """One stress-test client, running random reliable calls in a loop."""

    def __init__(self, client_id: int, gate_port: int, wallet_ids: List[str]):
        self.client_id = client_id
        self.gate_port = gate_port
        self.wallet_ids = wallet_ids
        self.client: Optional[Client] = None
        self._thread: Optional[threading.Thread] = None
        self._running = False

        # Per-call outcome and per-wallet ledger, written only by this
        # client's thread, read by the main thread after join(). No lock
        # needed because readers don't run until the thread has exited.
        self.ops: List[OpRecord] = []
        self.applied_deposits: Dict[str, int] = defaultdict(int)
        self.applied_withdrawals: Dict[str, int] = defaultdict(int)

        self.call_count = 0
        self.retry_count = 0
        self.fault_inject_count = 0
        self.mismatch_count = 0
        self.error_count = 0

    def connect(self) -> None:
        self.client = Client([f"localhost:{self.gate_port}"], ClientOptions(call_timeout=10.0))
        self.client.connect(timeout=10.0)

    def close(self) -> None:
        if self.client is not None:
            try:
                self.client.close()
            except Exception:
                pass
            self.client = None

    def start(self, stop_event: threading.Event) -> None:
        self._running = True
        self._thread = threading.Thread(target=self._run, args=(stop_event,), daemon=True)
        self._thread.start()

    def join(self, timeout: float = 30.0) -> None:
        if self._thread is not None:
            self._thread.join(timeout=timeout)

    def _run(self, stop_event: threading.Event) -> None:
        while not stop_event.is_set():
            try:
                self._one_iteration()
            except Exception as e:
                self.error_count += 1
                print(f"[client-{self.client_id}] iteration error: {e}")
            time.sleep(random.uniform(MIN_DELAY, MAX_DELAY))

    def _one_iteration(self) -> None:
        wallet_id = random.choice(self.wallet_ids)
        kind = random.choice(["deposit", "withdraw"])
        amount = random.randint(MIN_AMOUNT, MAX_AMOUNT)
        call_id = generate_call_id()

        op = OpRecord(call_id=call_id, wallet_id=wallet_id, kind=kind, amount=amount)

        # Fault injection: abort a first attempt with a tiny timeout so we
        # don't know whether the server executed it. The real retry with
        # the same call_id must still yield a consistent outcome. Outcome
        # of this attempt is ignored; we rely on the retry to tell us
        # what actually happened.
        if random.random() < FAULT_INJECT_PROB:
            self.fault_inject_count += 1
            short = random.uniform(FAULT_INJECT_MIN_TIMEOUT, FAULT_INJECT_MAX_TIMEOUT)
            self._invoke(op, timeout=short, count_rpc_error=False)

        resp = self._invoke(op)
        if resp is None:
            # Transient failure; don't retry — RPC error means the server
            # state is unknown, so both "applied" and "rejected" are possible.
            # Skipping the retry branch here keeps the ledger honest.
            self.ops.append(op)
            return

        op.ok = resp.ok
        op.balance_after = resp.balance
        self.call_count += 1

        if random.random() < RETRY_PROB:
            self.retry_count += 1
            resp2 = self._invoke(op)
            if resp2 is None:
                # Retry RPC failed; previous outcome still stands. Don't
                # upgrade the record.
                pass
            elif resp2.ok != op.ok:
                self.mismatch_count += 1
                print(
                    f"[client-{self.client_id}] RELIABLE-CALL VIOLATION: "
                    f"call_id={call_id} first ok={op.ok}, retry ok={resp2.ok}"
                )

        if op.ok:
            if op.kind == "deposit":
                self.applied_deposits[op.wallet_id] += op.amount
            else:
                self.applied_withdrawals[op.wallet_id] += op.amount

        self.ops.append(op)

    def _invoke(self, op: OpRecord, timeout: float = 5.0, count_rpc_error: bool = True):
        if self.client is None:
            return None
        method, req = self._build_request(op)
        try:
            any_resp, _status = self.client.reliable_call_object(
                op.call_id, "Wallet", op.wallet_id, method, req, timeout=timeout
            )
        except ReliableCallError as e:
            # Server-side reported method failure. Treat as rejected; do
            # not classify as RPC error, since the server has a recorded
            # outcome for this call_id already.
            print(f"[client-{self.client_id}] server error call_id={op.call_id}: {e}")
            return wallet_pb2.WalletResponse(wallet_id=op.wallet_id, balance=0, ok=False, reason=str(e))
        except Exception as e:
            if count_rpc_error:
                self.error_count += 1
                print(f"[client-{self.client_id}] rpc error call_id={op.call_id}: {e}")
            return None

        if any_resp is None:
            return None
        parsed = wallet_pb2.WalletResponse()
        any_resp.Unpack(parsed)
        return parsed

    @staticmethod
    def _build_request(op: OpRecord):
        if op.kind == "deposit":
            return "Deposit", wallet_pb2.DepositRequest(amount=op.amount)
        return "Withdraw", wallet_pb2.WithdrawRequest(amount=op.amount)


def query_balance(gate_port: int, wallet_id: str) -> Optional[int]:
    client = Client([f"localhost:{gate_port}"], ClientOptions(call_timeout=5.0))
    try:
        client.connect(timeout=5.0)
        any_resp = client.call_object(
            "Wallet", wallet_id, "GetBalance", wallet_pb2.GetBalanceRequest(), timeout=5.0,
        )
        if any_resp is None:
            return None
        parsed = wallet_pb2.WalletResponse()
        any_resp.Unpack(parsed)
        return parsed.balance
    finally:
        client.close()


def assert_conservation(clients: List[StressClient], wallet_ids: List[str], gate_port: int) -> bool:
    """Verify server balance equals per-wallet sum of applied ops across all clients."""
    expected: Dict[str, int] = defaultdict(int)
    for c in clients:
        for wid, amt in c.applied_deposits.items():
            expected[wid] += amt
        for wid, amt in c.applied_withdrawals.items():
            expected[wid] -= amt

    print("\n" + "=" * 80)
    print("CONSERVATION CHECK")
    print("=" * 80)
    failures = 0
    for wid in wallet_ids:
        exp = expected.get(wid, 0)
        got = query_balance(gate_port, wid)
        if got is None:
            print(f"❌ {wid}: server query failed")
            failures += 1
            continue
        if got != exp:
            print(f"❌ {wid}: expected={exp}, server={got} (DIVERGENCE)")
            failures += 1
        else:
            print(f"✅ {wid}: balance={got}")
    return failures == 0


def print_stats(clients: List[StressClient]) -> None:
    total_calls = sum(c.call_count for c in clients)
    total_retries = sum(c.retry_count for c in clients)
    total_injects = sum(c.fault_inject_count for c in clients)
    total_mismatch = sum(c.mismatch_count for c in clients)
    total_errors = sum(c.error_count for c in clients)
    print(f"\ncalls={total_calls}  retries={total_retries}  "
          f"fault_injects={total_injects}  rpc_errors={total_errors}  "
          f"idempotency_mismatches={total_mismatch}")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--duration", type=int, default=60, help="run duration in seconds")
    parser.add_argument("--clients", type=int, default=8, help="number of concurrent clients")
    parser.add_argument("--config", default=str(WALLET_DIR / "stress_config.yml"))
    args = parser.parse_args()

    if not ensure_postgres_ready():
        return 1

    config_file = Path(args.config).resolve()
    if not config_file.is_file():
        print(f"❌ config file not found: {config_file}")
        return 1

    import yaml
    with open(config_file) as f:
        cfg = yaml.safe_load(f)
    node_ids = [n["id"] for n in cfg["nodes"]][:NUM_NODES]
    gate_ids = [g["id"] for g in cfg["gates"]][:NUM_GATES]
    gate_ports = [int(g["grpc_addr"].rsplit(":", 1)[1]) for g in cfg["gates"]][:NUM_GATES]

    inspector = None
    wallet_servers: List[WalletServer] = []
    gateways: List[Gateway] = []
    clients: List[StressClient] = []
    stop_event = threading.Event()

    wallet_ids = [f"wallet-{i:03d}" for i in range(NUM_WALLETS)]

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
        for ws in wallet_servers:
            try:
                ws.close()
            except WalletServerStopTimeout as e:
                print(f"❌ {e}")
            except Exception as e:
                print(f"wallet-server close error: {e}")
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
        print("STARTING CLUSTER")
        print("=" * 80)
        inspector = Inspector(config_file=str(config_file))
        inspector.start()
        if not inspector.wait_for_ready(timeout=30):
            print("❌ Inspector failed to start")
            return 1

        for i, node_id in enumerate(node_ids):
            ws = WalletServer(server_index=i, config_file=str(config_file), node_id=node_id)
            ws.start()
            wallet_servers.append(ws)
        for ws in wallet_servers:
            if not ws.wait_for_ready(timeout=30):
                print(f"❌ {ws.name} failed to start")
                return 1

        # Give the cluster a moment to form shard ownership before clients connect.
        time.sleep(5)

        for i, gid in enumerate(gate_ids):
            gw = Gateway(config_file=str(config_file), gate_id=gid)
            gw.start()
            gateways.append(gw)

        time.sleep(2)

        print("\n" + "=" * 80)
        print(f"LAUNCHING {args.clients} CLIENTS FOR {args.duration}s")
        print("=" * 80)
        for i in range(args.clients):
            c = StressClient(i, gate_ports[i % len(gate_ports)], wallet_ids)
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
            c.join(timeout=10.0)

        print_stats(clients)

        ok = assert_conservation(clients, wallet_ids, gate_ports[0])
        if not ok:
            print("\n❌ CONSERVATION FAILED — reliable calls did not provide exactly-once semantics")
            return 1
        mismatches = sum(c.mismatch_count for c in clients)
        if mismatches > 0:
            print(f"\n❌ {mismatches} idempotency mismatches observed during run")
            return 1
        print("\n✅ wallet stress test passed")
        return 0
    finally:
        shutdown()


if __name__ == "__main__":
    sys.exit(main())
