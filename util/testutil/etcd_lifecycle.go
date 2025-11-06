package testutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// IsGitHubActions returns true if running in GitHub Actions CI environment
func IsGitHubActions() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true"
}

// isDockerAvailable checks if Docker is available on the system
func isDockerAvailable() bool {
	cmd := exec.Command("docker", "ps")
	err := cmd.Run()
	return err == nil
}

// isSystemdAvailable checks if systemd is available on the system
func isSystemdAvailable() bool {
	cmd := exec.Command("systemctl", "--version")
	err := cmd.Run()
	return err == nil
}

// StopEtcd stops the etcd service
// It tries Docker first (if available), then falls back to systemctl
// Returns an error if neither method is available or if stopping fails
func StopEtcd() error {
	// Try Docker first
	if isDockerAvailable() {
		// Check if there's an etcd container running
		checkCmd := exec.Command("docker", "ps", "--filter", "name=etcd", "--format", "{{.Names}}")
		output, err := checkCmd.Output()
		if err == nil && strings.TrimSpace(string(output)) != "" {
			containerName := strings.TrimSpace(string(output))
			cmd := exec.Command("docker", "stop", containerName)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to stop etcd container: %w", err)
			}
			return nil
		}
	}

	// Try systemctl
	if isSystemdAvailable() {
		cmd := exec.Command("sudo", "systemctl", "stop", "etcd")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to stop etcd service: %w", err)
		}
		return nil
	}

	return fmt.Errorf("neither Docker nor systemd is available to stop etcd")
}

// StartEtcd starts the etcd service
// It tries Docker first (if available), then falls back to systemctl
// Returns an error if neither method is available or if starting fails
func StartEtcd() error {
	// Try Docker first
	if isDockerAvailable() {
		// Check if there's an etcd container that's stopped
		checkCmd := exec.Command("docker", "ps", "-a", "--filter", "name=etcd", "--format", "{{.Names}}")
		output, err := checkCmd.Output()
		if err == nil && strings.TrimSpace(string(output)) != "" {
			containerName := strings.TrimSpace(string(output))
			cmd := exec.Command("docker", "start", containerName)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to start etcd container: %w", err)
			}
			return nil
		}
	}

	// Try systemctl
	if isSystemdAvailable() {
		cmd := exec.Command("sudo", "systemctl", "start", "etcd")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to start etcd service: %w", err)
		}
		return nil
	}

	return fmt.Errorf("neither Docker nor systemd is available to start etcd")
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
				_, err2 := cli.Get(ctx2, "/health-check")
				cancel2()
				cli.Close()
				
				if err2 == nil {
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
