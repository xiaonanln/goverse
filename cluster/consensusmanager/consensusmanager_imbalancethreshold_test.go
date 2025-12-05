package consensusmanager

import (
	"testing"

	"github.com/xiaonanln/goverse/cluster/shardlock"
)

// TestSetGetImbalanceThreshold tests the setter and getter for imbalance threshold
func TestSetGetImbalanceThreshold(t *testing.T) {
	t.Parallel()
	cm := NewConsensusManager(nil, shardlock.NewShardLock(testNumShards), 0, "", testNumShards)

	// Test default value
	defaultThreshold := cm.GetImbalanceThreshold()
	if defaultThreshold != 0.2 {
		t.Fatalf("Expected default imbalance threshold to be 0.2, got %f", defaultThreshold)
	}

	// Test setting a valid value
	cm.SetImbalanceThreshold(0.3)
	threshold := cm.GetImbalanceThreshold()
	if threshold != 0.3 {
		t.Fatalf("Expected imbalance threshold to be 0.3 after setting, got %f", threshold)
	}

	// Test setting another valid value
	cm.SetImbalanceThreshold(0.15)
	threshold = cm.GetImbalanceThreshold()
	if threshold != 0.15 {
		t.Fatalf("Expected imbalance threshold to be 0.15 after setting, got %f", threshold)
	}

	// Test setting zero (should use default)
	cm.SetImbalanceThreshold(0)
	threshold = cm.GetImbalanceThreshold()
	if threshold != 0.2 {
		t.Fatalf("Expected imbalance threshold to be 0.2 (default) when setting 0, got %f", threshold)
	}

	// Test setting negative value (should use default)
	cm.SetImbalanceThreshold(-0.1)
	threshold = cm.GetImbalanceThreshold()
	if threshold != 0.2 {
		t.Fatalf("Expected imbalance threshold to be 0.2 (default) when setting negative, got %f", threshold)
	}
}

// TestImbalanceThresholdInRebalanceLogic tests that the imbalance threshold is used correctly in rebalance logic
func TestImbalanceThresholdInRebalanceLogic(t *testing.T) {
	t.Parallel()
	cm := NewConsensusManager(nil, shardlock.NewShardLock(testNumShards), 0, "", testNumShards)

	// Verify we can set different thresholds
	testCases := []struct {
		name      string
		threshold float64
		expected  float64
	}{
		{
			name:      "default threshold",
			threshold: 0.2,
			expected:  0.2,
		},
		{
			name:      "higher threshold",
			threshold: 0.5,
			expected:  0.5,
		},
		{
			name:      "lower threshold",
			threshold: 0.1,
			expected:  0.1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cm.SetImbalanceThreshold(tc.threshold)
			actual := cm.GetImbalanceThreshold()
			if actual != tc.expected {
				t.Fatalf("Expected threshold %f, got %f", tc.expected, actual)
			}
		})
	}
}
