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
	l := &Logger{
		level:  DEBUG,
		logger: log.New(&buf, "", 0),
	}

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
	l := &Logger{
		level:  WARN,
		logger: log.New(&buf, "", 0),
	}

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
