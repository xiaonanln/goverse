#!/usr/bin/env python3
"""BombermanServer helper — thin wrapper around the shared ServerProcess base."""
import os
import sys
from pathlib import Path

# samples/bomberman/BombermanServer.py -> repo root
REPO_ROOT = Path(__file__).parent.parent.parent.resolve()
sys.path.insert(0, str(REPO_ROOT))
COMMON_DIR = REPO_ROOT / 'samples' / 'common'
sys.path.insert(0, str(COMMON_DIR))

from ServerProcess import ServerProcess, ServerStopTimeout

# Back-compat alias for any future callers that key on the
# sample-specific exception name.
BombermanServerStopTimeout = ServerStopTimeout


class BombermanServer(ServerProcess):
    """Manages a Goverse bomberman server (samples/bomberman) subprocess.

    Honours BOMBERMAN_MATCH_TIME_LIMIT_TICKS in the parent environment —
    the stress test sets it low (e.g. 50 ticks = 5 s) so each match
    concludes quickly as a draw, exercising the end-of-match
    RecordMatchResult reliable-call path many times in a CI window.
    """

    def __init__(self, server_index=0, binary_path=None, config_file=None,
                 node_id=None, build_if_needed=True):
        super().__init__(
            kind="Bomberman",
            binary_path=binary_path or '/tmp/bomberman_server',
            build_source_path='./samples/bomberman/server/',
            coverage_prefix='bomberman_server',
            server_index=server_index,
            config_file=config_file,
            node_id=node_id,
            build_if_needed=build_if_needed,
        )
