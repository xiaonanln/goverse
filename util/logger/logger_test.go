package logger

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// --- Level string representation test ---
func TestLogLevelString(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{DEBUG, "DEBUG"},
		{INFO, "INFO"},
		{WARN, "WARN"},
		{ERROR, "ERROR"},
		{FATAL, "FATAL"},
		{LogLevel(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		if got := tt.level.String(); got != tt.expected {
			t.Errorf("LogLevel.String() = %s; want %s", got, tt.expected)
		}
	}
}

// --- SetLevel and GetLevel test ---
func TestSetAndGetLevel(t *testing.T) {
	l := NewLogger("test")
	l.SetLevel(DEBUG)
	if got := l.GetLevel(); got != DEBUG {
		t.Errorf("GetLevel() = %v; want %v", got, DEBUG)
	}
}

// --- Level-based logging behavior ---
func TestLoggerLevels(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger("")
	l.logger = log.New(&buf, "", 0)
	l.SetLevel(DEBUG)

	l.Debugf("debug msg")
	l.Infof("info msg")
	l.Warnf("warn msg")
	l.Errorf("error msg")

	logs := buf.String()
	for _, msg := range []string{"debug msg", "info msg", "warn msg", "error msg"} {
		if !strings.Contains(logs, msg) {
			t.Errorf("Expected log to contain %q", msg)
		}
	}
}

// --- Level filtering ---
func TestLoggerLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger("")
	l.logger = log.New(&buf, "", 0)
	l.SetLevel(WARN)

	l.Debugf("debug msg")
	l.Infof("info msg")
	l.Warnf("warn msg")

	logs := buf.String()
	if strings.Contains(logs, "debug msg") || strings.Contains(logs, "info msg") {
		t.Errorf("Unexpected log entries at level WARN")
	}
	if !strings.Contains(logs, "warn msg") {
		t.Errorf("Expected WARN log to be present")
	}
}

// --- Fatalf test using subprocess isolation ---
func TestFatalf(t *testing.T) {
	if os.Getenv("TEST_FATAL") == "1" {
		l := NewLogger("test")
		l.Fatalf("fatal error occurred")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestFatalf")
	cmd.Env = append(os.Environ(), "TEST_FATAL=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr

	err := cmd.Run()

	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
		t.Errorf("Expected exit code 1, got %v", err)
	}

	output := stderr.String()
	if !strings.Contains(output, "fatal error occurred") || !strings.Contains(output, "goroutine") {
		t.Errorf("Fatalf did not log expected output or stack trace:\n%s", output)
	}
}

// --- SetPrefix and GetPrefix test ---
func TestSetAndGetPrefix(t *testing.T) {
	l := NewLogger("initial")

	// Check initial prefix
	if got := l.GetPrefix(); got != "initial" {
		t.Errorf("GetPrefix() = %v; want %v", got, "initial")
	}

	// Change prefix
	l.SetPrefix("changed")
	if got := l.GetPrefix(); got != "changed" {
		t.Errorf("GetPrefix() after SetPrefix = %v; want %v", got, "changed")
	}
}

// --- Test prefix in log output ---
func TestPrefixInLogOutput(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger("test-prefix")
	l.logger = log.New(&buf, "", 0)
	l.SetLevel(INFO)

	l.Infof("test message")

	output := buf.String()
	if !strings.Contains(output, "test-prefix") {
		t.Errorf("Expected log output to contain prefix 'test-prefix', got: %s", output)
	}

	// Change prefix and log again
	buf.Reset()
	l.SetPrefix("new-prefix")
	l.Infof("another message")

	output = buf.String()
	if !strings.Contains(output, "new-prefix") {
		t.Errorf("Expected log output to contain prefix 'new-prefix', got: %s", output)
	}
	if strings.Contains(output, "test-prefix") {
		t.Errorf("Expected log output not to contain old prefix 'test-prefix', got: %s", output)
	}
}

// --- Concurrency test for prefix changes ---
func TestConcurrentPrefixChanges(t *testing.T) {
	l := NewLogger("initial")
	const goroutines = 100
	const operations = 100

	// Channel to signal completion
	done := make(chan bool, goroutines)

	// Concurrent prefix changes
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			for j := 0; j < operations; j++ {
				prefix := strings.Repeat("x", id%10+1)
				l.SetPrefix(prefix)
				got := l.GetPrefix()
				// Verify we get a valid prefix (should be one of the concurrent writes)
				if len(got) == 0 || len(got) > 10 {
					t.Errorf("Invalid prefix length: %d", len(got))
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// --- Concurrency test for level and prefix together ---
func TestConcurrentLevelAndPrefixChanges(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger("initial")
	l.logger = log.New(&buf, "", 0)
	l.SetLevel(INFO)

	const goroutines = 50
	const operations = 100
	done := make(chan bool, goroutines*2)

	// Concurrent prefix changes
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			for j := 0; j < operations; j++ {
				prefix := strings.Repeat("p", id%5+1)
				l.SetPrefix(prefix)
				_ = l.GetPrefix()
			}
			done <- true
		}(i)
	}

	// Concurrent level changes
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			for j := 0; j < operations; j++ {
				level := LogLevel(id % 4)
				l.SetLevel(level)
				_ = l.GetLevel()
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < goroutines*2; i++ {
		<-done
	}
}

// --- Concurrency test for logging while changing prefix ---
func TestConcurrentLoggingAndPrefixChanges(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger("initial")
	l.logger = log.New(&buf, "", 0)
	l.SetLevel(INFO)

	const goroutines = 50
	const operations = 100
	done := make(chan bool, goroutines*2)

	// Concurrent logging
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			for j := 0; j < operations; j++ {
				l.Infof("test message %d-%d", id, j)
			}
			done <- true
		}(i)
	}

	// Concurrent prefix changes
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			for j := 0; j < operations; j++ {
				prefix := strings.Repeat("x", id%10+1)
				l.SetPrefix(prefix)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < goroutines*2; i++ {
		<-done
	}

	// Verify no crashes occurred and some logs were written
	if buf.Len() == 0 {
		t.Error("Expected some log output")
	}
}
