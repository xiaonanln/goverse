package testutil

import (
	"fmt"
	"net"
)

// GetFreePort returns an available TCP port on localhost by binding to port 0
// and immediately releasing it. The port should be available for immediate reuse.
func GetFreePort() (int, error) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, fmt.Errorf("failed to get free port: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	return addr.Port, nil
}

// GetFreeAddress returns an available TCP address (localhost:port) by binding to port 0
// and immediately releasing it. The address should be available for immediate reuse.
func GetFreeAddress() (string, error) {
	port, err := GetFreePort()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("localhost:%d", port), nil
}
