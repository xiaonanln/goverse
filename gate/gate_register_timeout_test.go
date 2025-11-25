package gate

import (
	"context"
	"testing"
	"time"

	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestRegisterGateTimeoutConfiguration tests that the RegisterGate timeout is
// properly configured and defaults are set correctly.
func TestRegisterGateTimeoutConfiguration(t *testing.T) {
	t.Run("DefaultTimeoutSet", func(t *testing.T) {
		config := &GateConfig{
			AdvertiseAddress: testutil.GetFreeAddress(),
			EtcdAddress:      "localhost:2379",
		}

		gate, err := NewGate(config)
		if err != nil {
			t.Fatalf("Failed to create gate: %v", err)
		}
		defer gate.Stop()

		expectedTimeout := 30 * time.Second
		if gate.config.DefaultRegisterGateTimeout != expectedTimeout {
			t.Errorf("Expected DefaultRegisterGateTimeout to be %v, got %v",
				expectedTimeout, gate.config.DefaultRegisterGateTimeout)
		}
	})

	t.Run("CustomTimeoutPreserved", func(t *testing.T) {
		customTimeout := 60 * time.Second
		config := &GateConfig{
			AdvertiseAddress:           testutil.GetFreeAddress(),
			EtcdAddress:                "localhost:2379",
			DefaultRegisterGateTimeout: customTimeout,
		}

		gate, err := NewGate(config)
		if err != nil {
			t.Fatalf("Failed to create gate: %v", err)
		}
		defer gate.Stop()

		if gate.config.DefaultRegisterGateTimeout != customTimeout {
			t.Errorf("Expected DefaultRegisterGateTimeout to be preserved at %v, got %v",
				customTimeout, gate.config.DefaultRegisterGateTimeout)
		}
	})
}

// TestRegisterGateTimeoutBehavior tests that the RegisterGate RPC respects the timeout
// configuration. When connecting to an unavailable server, the call should timeout
// within the configured duration.
func TestRegisterGateTimeoutBehavior(t *testing.T) {
	t.Run("RegisterGateTimesOutOnUnavailableNode", func(t *testing.T) {
		// Use a very short timeout for testing
		shortTimeout := 500 * time.Millisecond
		config := &GateConfig{
			AdvertiseAddress:           testutil.GetFreeAddress(),
			EtcdAddress:                "localhost:2379",
			DefaultRegisterGateTimeout: shortTimeout,
		}

		gate, err := NewGate(config)
		if err != nil {
			t.Fatalf("Failed to create gate: %v", err)
		}
		defer gate.Stop()

		ctx := context.Background()
		err = gate.Start(ctx)
		if err != nil {
			t.Fatalf("Gate.Start() returned error: %v", err)
		}

		// Use a dynamically allocated address that is not in use
		// This ensures we get a unique port that nothing is listening on
		unavailableAddr := testutil.GetFreeAddress()
		conn, err := grpc.NewClient(unavailableAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			t.Fatalf("Failed to create connection: %v", err)
		}
		defer conn.Close()

		client := goverse_pb.NewGoverseClient(conn)
		connections := map[string]goverse_pb.GoverseClient{
			unavailableAddr: client,
		}

		// Record start time
		startTime := time.Now()

		// Call RegisterWithNodes - this will start a goroutine that tries to connect
		gate.RegisterWithNodes(ctx, connections)

		// Poll for the registration to be cleaned up (goroutine completed)
		// Use a polling approach with a maximum wait time to be more deterministic
		maxWaitTime := shortTimeout + 1*time.Second
		pollInterval := 50 * time.Millisecond
		registrationCleanedUp := false

		for time.Since(startTime) < maxWaitTime {
			gate.nodeRegMu.RLock()
			_, exists := gate.nodeRegs[unavailableAddr]
			gate.nodeRegMu.RUnlock()

			if !exists {
				registrationCleanedUp = true
				break
			}
			time.Sleep(pollInterval)
		}

		elapsed := time.Since(startTime)

		if !registrationCleanedUp {
			t.Errorf("Expected registration to be cleaned up after timeout, but it still exists after %v", elapsed)
		}

		// Verify the elapsed time is reasonable (around the timeout value)
		// Allow for some variance due to connection attempt timing
		if elapsed > 2*time.Second {
			t.Errorf("Expected registration to complete within reasonable time, took %v", elapsed)
		}

		t.Logf("RegisterGate timeout behavior verified: registration completed after %v", elapsed)
	})
}

// TestRegisterGateTimeoutDoesNotAffectActiveStream tests that after the stream
// is established, the timeout doesn't affect the ongoing stream operations.
// This verifies that we only apply timeout to connection establishment, not stream lifetime.
func TestRegisterGateTimeoutDoesNotAffectActiveStream(t *testing.T) {
	// This test manually simulates an established connection to verify
	// that the timeout context is properly cancelled after stream establishment

	shortTimeout := 100 * time.Millisecond
	config := &GateConfig{
		AdvertiseAddress:           testutil.GetFreeAddress(),
		EtcdAddress:                "localhost:2379",
		DefaultRegisterGateTimeout: shortTimeout,
	}

	gate, err := NewGate(config)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}
	defer gate.Stop()

	ctx := context.Background()
	err = gate.Start(ctx)
	if err != nil {
		t.Fatalf("Gate.Start() returned error: %v", err)
	}

	// Manually add a mock registration with an active stream to simulate
	// what happens after RegisterGate succeeds
	mockNodeAddr := "test-node:8080"
	ctx1, cancel1 := context.WithCancel(ctx)
	mockStream := &mockGateStream{ctx: ctx1}

	gate.nodeRegMu.Lock()
	gate.nodeRegs[mockNodeAddr] = &nodeReg{
		stream: mockStream,
		cancel: cancel1,
		ctx:    ctx1,
	}
	gate.nodeRegMu.Unlock()

	// Wait longer than the configured timeout
	time.Sleep(shortTimeout * 2)

	// Verify the stream context is still active (not affected by timeout)
	select {
	case <-ctx1.Done():
		t.Error("Stream context should not be cancelled by the timeout")
	default:
		// Expected - context is still active
	}

	// Verify registration still exists
	gate.nodeRegMu.RLock()
	_, exists := gate.nodeRegs[mockNodeAddr]
	gate.nodeRegMu.RUnlock()

	if !exists {
		t.Error("Registration should still exist after timeout duration")
	}

	t.Logf("Verified that timeout does not affect established streams")
}
