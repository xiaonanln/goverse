package testutil

// TestNumShards is the number of shards to use in tests.
// Using a smaller number (64) instead of production default (8192)
// makes tests faster and reduces resource usage.
const TestNumShards = 64
