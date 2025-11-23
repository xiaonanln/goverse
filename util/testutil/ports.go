package testutil

import (
	"fmt"
	"net"
	"sync"
)

var (
	// recentPorts tracks recently allocated ports to prevent immediate reuse
	recentPorts   = make(map[int]bool)
	recentPortsMu sync.Mutex
)

// GetFreePort returns an available TCP port on localhost by binding to port 0
// and immediately releasing it. The port should be available for immediate reuse.
// To prevent collisions when called rapidly in succession, it tracks recently
// allocated ports and retries if a duplicate is detected.
// Panics if unable to allocate a port (should never happen in normal conditions).
func GetFreePort() int {
	const maxRetries = 100

	recentPortsMu.Lock()
	defer recentPortsMu.Unlock()

	for attempt := 0; attempt < maxRetries; attempt++ {
		listener, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			panic(fmt.Sprintf("failed to get free port: %v", err))
		}
		addr := listener.Addr().(*net.TCPAddr)
		port := addr.Port
		listener.Close()

		// Check if this port was recently allocated
		if !recentPorts[port] {
			// Mark this port as recently used
			recentPorts[port] = true

			// Clean up old entries if map gets too large (keep last 1000)
			if len(recentPorts) > 1000 {
				// Clear the map to prevent unbounded growth
				recentPorts = make(map[int]bool)
				recentPorts[port] = true
			}

			return port
		}
		// Port was recently allocated, try again
	}

	panic(fmt.Sprintf("failed to get unique free port after %d attempts", maxRetries))
}

// GetFreeAddress returns an available TCP address (localhost:port) by binding to port 0
// and immediately releasing it. The address should be available for immediate reuse.
// To prevent collisions when called rapidly in succession, it uses GetFreePort which
// tracks recently allocated ports.
// Panics if unable to allocate a port (should never happen in normal conditions).
func GetFreeAddress() string {
	port := GetFreePort()
	return fmt.Sprintf("localhost:%d", port)
}
