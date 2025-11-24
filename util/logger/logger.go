package logger

import (
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"sync/atomic"
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
	level  atomic.Value // stores LogLevel
	prefix atomic.Value // stores string
	logger *log.Logger
}

// NewLogger creates a new Logger instance with default INFO level
func NewLogger(prefix string) *Logger {
	l := &Logger{
		logger: log.New(os.Stdout, "", 0),
	}
	l.level.Store(INFO)
	l.prefix.Store(prefix)
	return l
}

// SetLevel sets the logging level
func (l *Logger) SetLevel(level LogLevel) {
	l.level.Store(level)
}

// GetLevel returns the current logging level
func (l *Logger) GetLevel() LogLevel {
	return l.level.Load().(LogLevel)
}

// SetPrefix sets the logging prefix
func (l *Logger) SetPrefix(prefix string) {
	l.prefix.Store(prefix)
}

// GetPrefix returns the current logging prefix
func (l *Logger) GetPrefix() string {
	return l.prefix.Load().(string)
}

// log is the internal logging method
func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	currentLevel := l.level.Load().(LogLevel)
	currentPrefix := l.prefix.Load().(string)

	if level < currentLevel {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	prefix := fmt.Sprintf("[%s] [%s] [%s] ", timestamp, level.String(), currentPrefix)

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
