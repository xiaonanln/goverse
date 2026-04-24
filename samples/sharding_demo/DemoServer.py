#!/usr/bin/env python3
"""DemoServer helper — thin wrapper around the shared ServerProcess base."""
import sys
from pathlib import Path

# samples/sharding_demo/DemoServer.py -> repo root
REPO_ROOT = Path(__file__).parent.parent.parent.resolve()
sys.path.insert(0, str(REPO_ROOT))
SAMPLES_DIR = REPO_ROOT / 'tests' / 'samples'
sys.path.insert(0, str(SAMPLES_DIR))

from ServerProcess import ServerProcess, ServerStopTimeout

# Back-compat alias: existing callers import DemoServerStopTimeout.
DemoServerStopTimeout = ServerStopTimeout


class DemoServer(ServerProcess):
    """Manages a Goverse demo server (samples/sharding_demo) subprocess."""

    def __init__(self, server_index=0, binary_path=None, config_file=None,
                 node_id=None, build_if_needed=True):
        super().__init__(
            kind="Demo",
            binary_path=binary_path or '/tmp/demo_server',
            build_source_path='./samples/sharding_demo/',
            coverage_prefix='demo_server',
            server_index=server_index,
            config_file=config_file,
            node_id=node_id,
            build_if_needed=build_if_needed,
        )
