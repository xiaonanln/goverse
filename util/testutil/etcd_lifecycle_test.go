package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsDockerEnvironment tests the IsDockerEnvironment function
func TestIsDockerEnvironment(t *testing.T) {
	// This test verifies the IsDockerEnvironment detection logic
	
	// In most test environments, we won't be in Docker (unless running in Docker container)
	// The function should return false if the scripts don't exist
	result := IsDockerEnvironment()
	
	// Check if the scripts actually exist
	_, startErr := os.Stat("/app/script/docker/start-etcd.sh")
	_, stopErr := os.Stat("/app/script/docker/stop-etcd.sh")
	
	expectedResult := (startErr == nil && stopErr == nil)
	
	if result != expectedResult {
		t.Errorf("IsDockerEnvironment() = %v, want %v", result, expectedResult)
		t.Logf("start-etcd.sh exists: %v (err: %v)", startErr == nil, startErr)
		t.Logf("stop-etcd.sh exists: %v (err: %v)", stopErr == nil, stopErr)
	}
}

// TestIsDockerEnvironment_Consistency tests that IsDockerEnvironment is consistent
func TestIsDockerEnvironment_Consistency(t *testing.T) {
	// This test verifies that IsDockerEnvironment returns consistent results
	// The actual result depends on whether /app/script/docker scripts exist
	
	// Call the function multiple times to ensure consistency
	result1 := IsDockerEnvironment()
	result2 := IsDockerEnvironment()
	
	if result1 != result2 {
		t.Errorf("IsDockerEnvironment() returned inconsistent results: %v != %v", result1, result2)
	}
	
	// Verify the logic: both files must exist for true result
	_, startErr := os.Stat("/app/script/docker/start-etcd.sh")
	_, stopErr := os.Stat("/app/script/docker/stop-etcd.sh")
	expectedResult := (startErr == nil && stopErr == nil)
	
	if result1 != expectedResult {
		t.Errorf("IsDockerEnvironment() = %v, but expected %v based on file existence", result1, expectedResult)
		t.Logf("start-etcd.sh exists: %v", startErr == nil)
		t.Logf("stop-etcd.sh exists: %v", stopErr == nil)
	}
}

// TestIsGitHubActions tests the IsGitHubActions function
func TestIsGitHubActions(t *testing.T) {
	// Save original environment variable
	originalValue := os.Getenv("GITHUB_ACTIONS")
	defer func() {
		if originalValue != "" {
			os.Setenv("GITHUB_ACTIONS", originalValue)
		} else {
			os.Unsetenv("GITHUB_ACTIONS")
		}
	}()
	
	// Test when GITHUB_ACTIONS is not set
	os.Unsetenv("GITHUB_ACTIONS")
	if IsGitHubActions() {
		t.Error("IsGitHubActions() = true when env var not set, want false")
	}
	
	// Test when GITHUB_ACTIONS is set to "true"
	os.Setenv("GITHUB_ACTIONS", "true")
	if !IsGitHubActions() {
		t.Error("IsGitHubActions() = false when env var is 'true', want true")
	}
	
	// Test when GITHUB_ACTIONS is set to other value
	os.Setenv("GITHUB_ACTIONS", "false")
	if IsGitHubActions() {
		t.Error("IsGitHubActions() = true when env var is 'false', want false")
	}
}

// TestDockerScriptsPaths verifies the paths used by IsDockerEnvironment are correct
func TestDockerScriptsPaths(t *testing.T) {
	// This test documents the expected paths for Docker scripts
	expectedStartScript := "/app/script/docker/start-etcd.sh"
	expectedStopScript := "/app/script/docker/stop-etcd.sh"
	
	// Get the actual project root (we're in util/testutil)
	projectRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Failed to get project root: %v", err)
	}
	
	// Verify the scripts exist in the repo (relative to project root)
	repoStartScript := filepath.Join(projectRoot, "script", "docker", "start-etcd.sh")
	repoStopScript := filepath.Join(projectRoot, "script", "docker", "stop-etcd.sh")
	
	if _, err := os.Stat(repoStartScript); err != nil {
		t.Errorf("start-etcd.sh not found at %s: %v", repoStartScript, err)
	} else {
		t.Logf("✓ Found start-etcd.sh at %s", repoStartScript)
	}
	
	if _, err := os.Stat(repoStopScript); err != nil {
		t.Errorf("stop-etcd.sh not found at %s: %v", repoStopScript, err)
	} else {
		t.Logf("✓ Found stop-etcd.sh at %s", repoStopScript)
	}
	
	t.Logf("IsDockerEnvironment() checks for scripts at:")
	t.Logf("  - %s", expectedStartScript)
	t.Logf("  - %s", expectedStopScript)
	t.Logf("In Docker container with /app mount, these would be the actual paths")
}
