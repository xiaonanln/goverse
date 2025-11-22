package testutil

import (
	"fmt"
	"net"
)

// GetFreePort returns an available TCP port on localhost by binding to port 0
// and immediately releasing it. The port should be available for immediate reuse.
// Panics if unable to allocate a port (should never happen in normal conditions).
func GetFreePort() int {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		panic(fmt.Sprintf("failed to get free port: %v", err))
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	return addr.Port
}

// GetFreeAddress returns an available TCP address (localhost:port) by binding to port 0
// and immediately releasing it. The address should be available for immediate reuse.
// Panics if unable to allocate a port (should never happen in normal conditions).
func GetFreeAddress() string {
	port := GetFreePort()
	return fmt.Sprintf("localhost:%d", port)
}
