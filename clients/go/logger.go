package orisun

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// LogLevel represents the severity level of a log message
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
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
	default:
		return "UNKNOWN"
	}
}

// IsEnabled returns true if this level is enabled for the given target level
func (l LogLevel) IsEnabled(target LogLevel) bool {
	return l <= target
}

// Logger interface for the Orisun client
type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
	Errorf(msg string, err error, args ...interface{})
	
	// Check if debug logging is enabled
	IsDebugEnabled() bool
	
	// Check if info logging is enabled
	IsInfoEnabled() bool
}

// DefaultLogger is a default implementation of Logger that outputs to console
type DefaultLogger struct {
	level  LogLevel
	logger *log.Logger
	mu     sync.RWMutex
}

// NewDefaultLogger creates a new DefaultLogger with the specified log level
func NewDefaultLogger(level LogLevel) *DefaultLogger {
	return &DefaultLogger{
		level:  level,
		logger: log.New(os.Stdout, "", 0),
	}
}

// NewDefaultLoggerWithOutput creates a new DefaultLogger with the specified log level and output
func NewDefaultLoggerWithOutput(level LogLevel, output *os.File) *DefaultLogger {
	return &DefaultLogger{
		level:  level,
		logger: log.New(output, "", 0),
	}
}

// Debug logs a debug message if debug level is enabled
func (l *DefaultLogger) Debug(msg string, args ...interface{}) {
	if l.IsDebugEnabled() {
		l.log(DEBUG, msg, nil, args...)
	}
}

// Info logs an info message if info level is enabled
func (l *DefaultLogger) Info(msg string, args ...interface{}) {
	if l.IsInfoEnabled() {
		l.log(INFO, msg, nil, args...)
	}
}

// Warn logs a warning message
func (l *DefaultLogger) Warn(msg string, args ...interface{}) {
	l.log(WARN, msg, nil, args...)
}

// Error logs an error message
func (l *DefaultLogger) Error(msg string, args ...interface{}) {
	l.log(ERROR, msg, nil, args...)
}

// Errorf logs an error message with an error
func (l *DefaultLogger) Errorf(msg string, err error, args ...interface{}) {
	l.log(ERROR, msg, err, args...)
}

// IsDebugEnabled returns true if debug logging is enabled
func (l *DefaultLogger) IsDebugEnabled() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level.IsEnabled(DEBUG)
}

// IsInfoEnabled returns true if info logging is enabled
func (l *DefaultLogger) IsInfoEnabled() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level.IsEnabled(INFO)
}

// SetLevel sets the log level
func (l *DefaultLogger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// GetLevel returns the current log level
func (l *DefaultLogger) GetLevel() LogLevel {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level
}

// log is the internal logging method
func (l *DefaultLogger) log(level LogLevel, msg string, err error, args ...interface{}) {
	l.mu.RLock()
	currentLevel := l.level
	logger := l.logger
	l.mu.RUnlock()

	if !currentLevel.IsEnabled(level) {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	
	// Format message with arguments
	formattedMsg := msg
	if len(args) > 0 {
		// Replace {} with %s for Go formatting
		goMsg := strings.ReplaceAll(msg, "{}", "%s")
		formattedMsg = fmt.Sprintf(goMsg, args...)
	}
	
	logLine := fmt.Sprintf("[%s] %s [goroutine] %s", timestamp, level.String(), formattedMsg)
	
	if err != nil {
		logLine = fmt.Sprintf("%s: %v", logLine, err)
	}
	
	if level == ERROR || level == WARN {
		logger.SetOutput(os.Stderr)
		logger.Println(logLine)
		logger.SetOutput(os.Stdout)
	} else {
		logger.Println(logLine)
	}
}

// NoOpLogger is a logger that does nothing
type NoOpLogger struct{}

// NewNoOpLogger creates a new NoOpLogger
func NewNoOpLogger() *NoOpLogger {
	return &NoOpLogger{}
}

func (l *NoOpLogger) Debug(msg string, args ...interface{}) {}
func (l *NoOpLogger) Info(msg string, args ...interface{})  {}
func (l *NoOpLogger) Warn(msg string, args ...interface{})  {}
func (l *NoOpLogger) Error(msg string, args ...interface{}) {}
func (l *NoOpLogger) Errorf(msg string, err error, args ...interface{}) {}
func (l *NoOpLogger) IsDebugEnabled() bool                     { return false }
func (l *NoOpLogger) IsInfoEnabled() bool                      { return false }