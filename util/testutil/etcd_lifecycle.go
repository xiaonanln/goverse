package testutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// IsGitHubActions returns true if running in GitHub Actions CI environment
func IsGitHubActions() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true"
}

// IsDockerEnvironment returns true if running in Docker environment
// It checks for the existence of Docker-specific etcd scripts
func IsDockerEnvironment() bool {
	// Check if both Docker etcd scripts exist
	_, startErr := os.Stat("/app/script/docker/start-etcd.sh")
	_, stopErr := os.Stat("/app/script/docker/stop-etcd.sh")
	return startErr == nil && stopErr == nil
}

// StopEtcd stops the etcd service
// Returns an error if stopping fails
func StopEtcd() error {
	// First try Docker script if in Docker environment
	if IsDockerEnvironment() {
		cmd := exec.Command("/bin/bash", "/app/script/docker/stop-etcd.sh")
		if err := cmd.Run(); err == nil {
			return nil
		} else {
			// Log the error but continue to fallback methods
			fmt.Fprintf(os.Stderr, "Warning: Docker stop-etcd.sh failed: %v, trying other methods\n", err)
		}
		// If Docker script fails, fall through to other methods
	}

	// Try systemctl (for CI environment)
	cmd := exec.Command("sudo", "systemctl", "stop", "etcd")
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Fall back to killing the process (for local development)
	cmd = exec.Command("pkill", "-9", "etcd")
	if err := cmd.Run(); err != nil {
		// pkill returns error if no processes found, which is ok
		// We only care if we can't run the command itself
		if _, ok := err.(*exec.ExitError); !ok {
			return fmt.Errorf("failed to stop etcd: %w", err)
		}
	}
	return nil
}

// StartEtcd starts the etcd service
// Returns an error if starting fails
func StartEtcd() error {
	// First try Docker script if in Docker environment
	if IsDockerEnvironment() {
		cmd := exec.Command("/bin/bash", "/app/script/docker/start-etcd.sh")
		if err := cmd.Run(); err == nil {
			return nil
		} else {
			// Log the error but continue to fallback methods
			fmt.Fprintf(os.Stderr, "Warning: Docker start-etcd.sh failed: %v, trying other methods\n", err)
		}
		// If Docker script fails, fall through to other methods
	}

	// Try systemctl (for CI environment)
	cmd := exec.Command("sudo", "systemctl", "start", "etcd")
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Fall back to starting as background process (for local development)
	// Clean up any stale etcd data directory
	if _, err := os.Stat("/app/default.etcd"); err == nil {
		os.RemoveAll("/app/default.etcd")
	}

	// Start etcd in background
	cmd = exec.Command("etcd",
		"--listen-client-urls", "http://0.0.0.0:2379",
		"--advertise-client-urls", "http://0.0.0.0:2379")
	
	// Redirect output to /tmp/etcd.log
	logFile, err := os.OpenFile("/tmp/etcd.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open etcd log file: %w", err)
	}
	
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("failed to start etcd: %w", err)
	}
	
	// Don't wait for the command to finish (it runs in background)
	go func() {
		cmd.Wait()
		logFile.Close()
	}()
	
	return nil
}

// WaitForEtcd waits for etcd to be available at the given endpoint
// It uses exponential backoff with a maximum timeout
// Returns an error if etcd doesn't become available within the timeout
func WaitForEtcd(endpoint string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Initial delay
	delay := 100 * time.Millisecond
	maxDelay := 2 * time.Second

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for etcd at %s to be available", endpoint)
		default:
			// Try to connect to etcd
			cli, err := clientv3.New(clientv3.Config{
				Endpoints:   []string{endpoint},
				DialTimeout: 2 * time.Second,
			})
			if err == nil {
				// Try to get a key to verify connection
				ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
				_, healthCheckErr := cli.Get(ctx2, "/health-check")
				cancel2()
				cli.Close()
				
				if healthCheckErr == nil {
					// Successfully connected
					return nil
				}
			}

			// Wait before retrying with exponential backoff
			time.Sleep(delay)
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		}
	}
}
