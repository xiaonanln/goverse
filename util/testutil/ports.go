package testutil

import (
	"fmt"
	"net"
	"sync"
)

var (
	// recentPorts tracks recently allocated ports to prevent immediate reuse
	// Uses both a map (for O(1) lookup) and slice (for maintaining order)
	recentPortsMap  = make(map[int]bool)
	recentPortsList []int
	recentPortsMu   sync.Mutex
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

		// Check if this port was recently allocated (O(1) map lookup)
		if !recentPortsMap[port] {
			// Mark this port as recently used
			recentPortsMap[port] = true
			recentPortsList = append(recentPortsList, port)

			// Keep only the most recent ports (remove oldest if list is too long)
			// This gradual cleanup maintains collision protection for recent ports
			if len(recentPortsList) > maxTrackedPorts {
				// Remove the oldest entry from both map and list
				oldestPort := recentPortsList[0]
				delete(recentPortsMap, oldestPort)
				recentPortsList = recentPortsList[1:]
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
