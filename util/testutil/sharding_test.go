package testutil

import "testing"

func TestTestNumShards(t *testing.T) {
	if TestNumShards != 64 {
		t.Fatalf("TestNumShards should be 64, got %d", TestNumShards)
	}
}
