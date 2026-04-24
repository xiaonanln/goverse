#!/usr/bin/env python3
"""Shared subprocess wrapper for sample server binaries used in stress tests.

DemoServer (sharding_demo) and WalletServer (wallet) had identical lifecycle
logic (config parsing, coverage-dir setup, SIGTERM-grace-then-raise). This
module is the single source of truth; each sample's thin wrapper only has
to provide its display name, binary path, and Go build-source path.
"""
import os
import signal
import subprocess
import sys
import time
from pathlib import Path

# tests/samples/ServerProcess.py -> repo root
REPO_ROOT = Path(__file__).parent.parent.parent.resolve()
sys.path.insert(0, str(REPO_ROOT))
SAMPLES_DIR = REPO_ROOT / 'tests' / 'samples'
sys.path.insert(0, str(SAMPLES_DIR))

from BinaryHelper import BinaryHelper


class ServerStopTimeout(RuntimeError):
    """Raised when a sample server fails to exit within the SIGTERM grace.

    Hanging on SIGTERM means the node is stuck somewhere in shutdown
    (blocked persistence flush, deadlocked goroutine, lost shutdown
    signal) — a real bug we want to surface loudly. We deliberately do
    NOT follow up with SIGKILL: that would mask the problem and the
    leaked process is useful evidence.
    """


# Keep grace aligned with DemoServer's historical value: must cover
# Node.Stop() persisting every in-memory object to Postgres. gRPC
# shutdown itself is near-instant; the dominant cost is proportional
# to the number of dirty objects.
_STOP_GRACE_SECONDS = 20


class ServerProcess:
    """Base wrapper for a single sample server subprocess.

    Subclasses should pass:
      kind              - display name, e.g. "Wallet" or "Demo"
      binary_path       - default binary location, e.g. '/tmp/wallet_server'
      build_source_path - Go source path passed to BinaryHelper.build_binary,
                          e.g. './samples/wallet/server/'
      coverage_prefix   - per-server GOCOVERDIR subdir prefix, e.g. 'wallet_server'
    """

    process: subprocess.Popen | None

    def __init__(self, kind, binary_path, build_source_path, coverage_prefix,
                 server_index=0, config_file=None, node_id=None,
                 build_if_needed=True):
        if not config_file or not node_id:
            raise ValueError("config_file and node_id are required")

        self.kind = kind
        self.binary_path = binary_path
        self.build_source_path = build_source_path
        self.coverage_prefix = coverage_prefix
        self.server_index = server_index
        self.config_file = config_file
        self.node_id = node_id

        import yaml
        with open(config_file, 'r') as f:
            config = yaml.safe_load(f)

        node_config = None
        for node in config.get('nodes', []):
            if node.get('id') == node_id:
                node_config = node
                break
        if not node_config:
            raise ValueError(f"Node ID '{node_id}' not found in config file")

        grpc_addr = node_config.get('grpc_addr', '')
        self.listen_port = int(grpc_addr.split(':')[1]) if ':' in grpc_addr else 0

        self.process = None
        self.name = f"{kind} Server {server_index + 1}"

        if build_if_needed and not os.path.exists(self.binary_path):
            description = f"{kind.lower()} server"
            if not BinaryHelper.build_binary(self.build_source_path, self.binary_path, description):
                raise RuntimeError(f"Failed to build {description} binary at {self.binary_path}")

    def start(self) -> None:
        if self.process is not None:
            print(f"⚠️  {self.name} is already running")
            return

        cmd = [self.binary_path, '--config', self.config_file, '--node-id', self.node_id]

        env = os.environ.copy()
        cov_dir = os.environ.get('GOCOVERDIR', '').strip()
        if cov_dir:
            server_cov_dir = os.path.join(cov_dir, f'{self.coverage_prefix}_{self.server_index}')
            os.makedirs(server_cov_dir, exist_ok=True)
            env['GOCOVERDIR'] = server_cov_dir

        print(f"Starting {self.name} (node_id: {self.node_id}, port: {self.listen_port})...")
        self.process = subprocess.Popen(
            cmd,
            stdout=None,
            stderr=None,
            env=env,
            text=True,
            bufsize=1,
        )

        time.sleep(0.5)

        if self.process.poll() is not None:
            stdout, _ = self.process.communicate()
            raise RuntimeError(f"{self.name} failed to start. Output:\n{stdout}")

    def wait_for_ready(self, timeout: float = 30) -> bool:
        print(f"Waiting for {self.name} to be ready...")
        start = time.time()
        while time.time() - start < timeout:
            if self.process and self.process.poll() is not None:
                print(f"❌ {self.name} terminated unexpectedly")
                return False
            time.sleep(0.5)
            if time.time() - start > 1:
                print(f"✅ {self.name} is ready")
                return True
        print(f"⚠️  {self.name} wait timeout")
        return False

    def close(self) -> None:
        if self.process is None:
            return

        print(f"Stopping {self.name}...")
        try:
            self.process.send_signal(signal.SIGTERM)
            self.process.wait(timeout=_STOP_GRACE_SECONDS)
        except subprocess.TimeoutExpired:
            pid = self.process.pid
            raise ServerStopTimeout(
                f"{self.name} (pid={pid}) did not exit within {_STOP_GRACE_SECONDS}s of SIGTERM; "
                "investigate the hang before re-running"
            )

        self.process = None
        print(f"✅ {self.name} stopped")
