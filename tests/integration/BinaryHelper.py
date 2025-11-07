#!/usr/bin/env python3
"""Binary building helper for Goverse integration tests."""
import os
import subprocess
from pathlib import Path
from typing import List

# Repo root (tests/integration/BinaryHelper.py -> repo root)
REPO_ROOT = Path(__file__).parent.parent.parent.resolve()


class BinaryHelper:
    """Helper class for building Go binaries with optional coverage instrumentation."""

    @staticmethod
    def build_binary(source_path: str, output_path: str, name: str) -> bool:
        """Build a Go binary.

        Args:
            source_path: Path to the Go source directory (relative to repo root)
            output_path: Path where the binary should be output
            name: Human-readable name for logging

        Returns:
            True if build succeeded, False otherwise
        """
        print(f"Building {name}...")
        try:
            # Check if coverage is enabled via environment variable
            enable_coverage = os.environ.get('ENABLE_COVERAGE', '').lower() in ('true', '1', 'yes')
            # Check if race detector is enabled via environment variable
            enable_race = os.environ.get('ENABLE_RACE', '').lower() in ('true', '1', 'yes')

            cmd: List[str] = ['go', 'build', '-buildvcs=false']
            if enable_coverage:
                cmd.append('-cover')
                print(f"  Coverage instrumentation enabled for {name}")
            if enable_race:
                cmd.append('-race')
                print(f"  Race detector enabled for {name}")

            cmd.extend(['-o', output_path, source_path])

            subprocess.run(cmd, check=True, capture_output=True, text=True, cwd=str(REPO_ROOT))
            print(f"✅ {name} built successfully")
            return True
        except subprocess.CalledProcessError as e:
            print(f"❌ Failed to build {name}")
            print(f"Error: {e.stderr}")
            return False
