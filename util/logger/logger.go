package logger

import (
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"time"
)

// LogLevel represents the logging level
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	case FATAL:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// Logger represents a logger with configurable log level
type Logger struct {
	level  LogLevel
	prefix string
	logger *log.Logger
}

// NewLogger creates a new Logger instance with default INFO level
func NewLogger(prefix string) *Logger {
	return &Logger{
		level:  INFO,
		prefix: prefix,
		logger: log.New(os.Stdout, "", 0),
	}
}

// SetLevel sets the logging level
func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

// GetLevel returns the current logging level
func (l *Logger) GetLevel() LogLevel {
	return l.level
}

// log is the internal logging method
func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	prefix := fmt.Sprintf("[%s] [%s] [%s] ", timestamp, level.String(), l.prefix)

	message := fmt.Sprintf(format, args...)
	l.logger.Print(prefix + message)

	if level == FATAL {
		l.logger.Print(string(debug.Stack()))
		os.Exit(1)
	}
}

// Debugf logs a debug message
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.log(DEBUG, format, args...)
}

// Infof logs an info message
func (l *Logger) Infof(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// Warnf logs a warning message
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.log(WARN, format, args...)
}

// Errorf logs an error message
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.log(ERROR, format, args...)
}

// Fatalf logs a fatal message and exits the program
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.log(FATAL, format, args...)
}
