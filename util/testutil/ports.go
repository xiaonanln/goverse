package testutil

import (
	"fmt"
	"net"
	"sync"
)

var (
	// recentPorts tracks recently allocated ports to prevent immediate reuse
	// This is an append-only list where newer ports are added to the end
	recentPorts   []int
	recentPortsMu sync.Mutex
)

// GetFreePort returns an available TCP port on localhost by binding to port 0
// and immediately releasing it. The port should be available for immediate reuse.
// To prevent collisions when called rapidly in succession, it tracks recently
// allocated ports and retries if a duplicate is detected.
// Panics if unable to allocate a port (should never happen in normal conditions).
func GetFreePort() int {
	const maxRetries = 100
	const maxTrackedPorts = 1000

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
		isRecent := false
		for _, recentPort := range recentPorts {
			if recentPort == port {
				isRecent = true
				break
			}
		}

		if !isRecent {
			// Mark this port as recently used by adding to the list
			recentPorts = append(recentPorts, port)

			// Keep only the most recent ports (remove oldest if list is too long)
			// This gradual cleanup maintains collision protection for recent ports
			if len(recentPorts) > maxTrackedPorts {
				// Remove the oldest entry (first element)
				recentPorts = recentPorts[1:]
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
