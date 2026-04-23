#!/usr/bin/env python3
"""WalletServer helper for managing the wallet server process in tests."""
import os
import signal
import subprocess
import sys
import time
from pathlib import Path

REPO_ROOT = Path(__file__).parent.parent.parent.resolve()
sys.path.insert(0, str(REPO_ROOT))
SAMPLES_DIR = REPO_ROOT / 'tests' / 'samples'
sys.path.insert(0, str(SAMPLES_DIR))

from BinaryHelper import BinaryHelper


class WalletServerStopTimeout(RuntimeError):
    """Raised when a wallet server fails to exit within the SIGTERM grace."""


class WalletServer:
    """Manages a Goverse wallet server process for the stress test."""

    process: subprocess.Popen | None

    def __init__(self, server_index=0, binary_path=None, config_file=None,
                 node_id=None, build_if_needed=True):
        self.server_index = server_index
        self.binary_path = binary_path or '/tmp/wallet_server'
        self.config_file = config_file
        self.node_id = node_id

        if not config_file or not node_id:
            raise ValueError("config_file and node_id are required")

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

        grpc_addr = node_config.get('grpc_addr', '0.0.0.0:9311')
        self.listen_port = int(grpc_addr.split(':')[1]) if ':' in grpc_addr else 9311

        self.process = None
        self.name = f"Wallet Server {server_index + 1}"

        if build_if_needed and not os.path.exists(self.binary_path):
            if not BinaryHelper.build_binary('./samples/wallet/server/', self.binary_path, 'wallet server'):
                raise RuntimeError(f"Failed to build wallet server binary at {self.binary_path}")

    def start(self) -> None:
        if self.process is not None:
            print(f"⚠️  {self.name} is already running")
            return

        cmd = [self.binary_path, '--config', self.config_file, '--node-id', self.node_id]

        cov_dir = os.environ.get('GOCOVERDIR', '').strip()
        env = os.environ.copy()
        if cov_dir:
            server_cov_dir = os.path.join(cov_dir, f'wallet_server_{self.server_index}')
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
        # Hanging on SIGTERM indicates a real shutdown bug (stuck persistence
        # flush, deadlocked goroutine). Do NOT SIGKILL on timeout — raise so
        # the stuck process remains for post-mortem, matching DemoServer.
        if self.process is None:
            return

        print(f"Stopping {self.name}...")
        try:
            self.process.send_signal(signal.SIGTERM)
            self.process.wait(timeout=20)
        except subprocess.TimeoutExpired:
            pid = self.process.pid
            raise WalletServerStopTimeout(
                f"{self.name} (pid={pid}) did not exit within 20s of SIGTERM; "
                "investigate the hang before re-running"
            )

        self.process = None
        print(f"✅ {self.name} stopped")
