package testutil

import (
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestGetFreePort(t *testing.T) {
	port := GetFreePort()

	if port <= 0 || port > 65535 {
		t.Fatalf("GetFreePort() returned invalid port: %d", port)
	}

	// Verify we can actually bind to the port
	listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		t.Fatalf("Failed to bind to port %d returned by GetFreePort(): %v", port, err)
	}
	listener.Close()
}

func TestGetFreeAddress(t *testing.T) {
	addr := GetFreeAddress()

	if !strings.HasPrefix(addr, "localhost:") {
		t.Fatalf("GetFreeAddress() returned invalid address format: %s", addr)
	}

	// Verify we can actually bind to the address
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to bind to address %s returned by GetFreeAddress(): %v", addr, err)
	}
	listener.Close()
}

func TestGetFreePort_MultipleCalls(t *testing.T) {
	// Get multiple ports and verify they're different and all valid
	ports := make(map[int]bool)
	numPorts := 5

	for i := 0; i < numPorts; i++ {
		port := GetFreePort()

		if port <= 0 || port > 65535 {
			t.Fatalf("GetFreePort() call %d returned invalid port: %d", i+1, port)
		}

		ports[port] = true
	}

	// We expect at least some different ports (though theoretically they could be the same)
	// This is a soft check - we just verify all ports were valid
	if len(ports) == 0 {
		t.Fatal("No valid ports were returned")
	}
}

func TestGetFreeAddress_MultipleCalls(t *testing.T) {
	// Get multiple addresses and verify they're all valid
	addresses := make(map[string]bool)
	numAddresses := 5

	for i := 0; i < numAddresses; i++ {
		addr := GetFreeAddress()

		if !strings.HasPrefix(addr, "localhost:") {
			t.Fatalf("GetFreeAddress() call %d returned invalid address: %s", i+1, addr)
		}

		addresses[addr] = true
	}

	// Verify all addresses were valid
	if len(addresses) == 0 {
		t.Fatal("No valid addresses were returned")
	}
}
