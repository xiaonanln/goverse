#!/usr/bin/env python3
"""Port allocation helper for Goverse integration tests.

Provides dynamic port allocation to prevent port conflicts during parallel
or sequential test runs. Implements a pattern similar to testutil.GetFreePort()
in Go, with tracking of recently allocated ports to prevent immediate reuse.
"""
import socket
import threading
from typing import Set


# Module-level state for tracking recently allocated ports
_recent_ports: Set[int] = set()
_recent_ports_lock = threading.Lock()
_MAX_TRACKED_PORTS = 1000


def get_free_port() -> int:
    """Get an available TCP port on localhost.
    
    Binds to port 0 to let the OS allocate an available port,
    then immediately releases it. Tracks recently allocated ports
    to prevent collisions when called rapidly in succession.
    
    Returns:
        An available port number
        
    Raises:
        RuntimeError: If unable to allocate a unique port after max retries
    """
    max_retries = 100
    
    with _recent_ports_lock:
        for _ in range(max_retries):
            # Let the OS assign a free port
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
                sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                sock.bind(('localhost', 0))
                port = sock.getsockname()[1]
            
            # Check if this port was recently allocated
            if port not in _recent_ports:
                # Mark this port as recently used
                _recent_ports.add(port)
                
                # Keep only the most recent ports
                if len(_recent_ports) > _MAX_TRACKED_PORTS:
                    # Remove approximately half of the oldest ports
                    # (sets are unordered, so we just remove some)
                    ports_to_remove = list(_recent_ports)[:_MAX_TRACKED_PORTS // 2]
                    for p in ports_to_remove:
                        _recent_ports.discard(p)
                
                return port
        
        raise RuntimeError(f"Failed to get unique free port after {max_retries} attempts")


def get_free_address() -> str:
    """Get an available TCP address (localhost:port).
    
    Returns:
        An address string in the format "localhost:port"
    """
    port = get_free_port()
    return f"localhost:{port}"
