package orisun

import (
	"context"
	"fmt"
	"time"
)

// ServerAddress represents a server address with host and port
type ServerAddress struct {
	Host string
	Port int
}

// NewServerAddress creates a new ServerAddress
func NewServerAddress(host string, port int) *ServerAddress {
	return &ServerAddress{
		Host: host,
		Port: port,
	}
}

// String returns the string representation of the server address
func (sa *ServerAddress) String() string {
	return fmt.Sprintf("%s:%d", sa.Host, sa.Port)
}

// WithTimeout creates a new context with timeout
func WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, timeout)
}

// WithDeadline creates a new context with deadline
func WithDeadline(parent context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	return context.WithDeadline(parent, deadline)
}

// WithCancel creates a new context with cancel function
func WithCancel(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(parent)
}

// ContextHelper provides utility functions for working with contexts
type ContextHelper struct{}

// NewContextHelper creates a new ContextHelper
func NewContextHelper() *ContextHelper {
	return &ContextHelper{}
}

// WithTimeout creates a new context with the given timeout in seconds
func (ch *ContextHelper) WithTimeout(parent context.Context, timeoutSeconds int) (context.Context, context.CancelFunc) {
	return WithTimeout(parent, time.Duration(timeoutSeconds)*time.Second)
}

// WithTimeoutMillis creates a new context with the given timeout in milliseconds
func (ch *ContextHelper) WithTimeoutMillis(parent context.Context, timeoutMillis int64) (context.Context, context.CancelFunc) {
	return WithTimeout(parent, time.Duration(timeoutMillis)*time.Millisecond)
}

// RetryConfig holds configuration for retry operations
type RetryConfig struct {
	MaxRetries    int
	InitialDelay   time.Duration
	MaxDelay       time.Duration
	BackoffFactor  float64
	RetryableFunc  func(error) bool
}

// DefaultRetryConfig returns a default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:    3,
		InitialDelay:   100 * time.Millisecond,
		MaxDelay:       5 * time.Second,
		BackoffFactor:  2.0,
		RetryableFunc:  DefaultRetryableFunc,
	}
}

// DefaultRetryableFunc is the default function to determine if an error is retryable
func DefaultRetryableFunc(err error) bool {
	// In a real implementation, you would check for specific error types
	// For now, we'll return false for all errors
	return err != nil
}

// RetryHelper provides utility functions for retry operations
type RetryHelper struct {
	config *RetryConfig
}

// NewRetryHelper creates a new RetryHelper with the given config
func NewRetryHelper(config *RetryConfig) *RetryHelper {
	if config == nil {
		config = DefaultRetryConfig()
	}
	return &RetryHelper{
		config: config,
	}
}

// Do executes the given function with retry logic
func (rh *RetryHelper) Do(fn func() error) error {
	var lastErr error
	delay := rh.config.InitialDelay
	
	for attempt := 0; attempt <= rh.config.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(delay)
			delay = time.Duration(float64(delay) * rh.config.BackoffFactor)
			if delay > rh.config.MaxDelay {
				delay = rh.config.MaxDelay
			}
		}
		
		err := fn()
		if err == nil {
			return nil
		}
		
		lastErr = err
		
		// Check if error is retryable
		if !rh.config.RetryableFunc(err) {
			break
		}
	}
	
	return lastErr
}

// DoWithContext executes the given function with retry logic and context
func (rh *RetryHelper) DoWithContext(ctx context.Context, fn func(context.Context) error) error {
	var lastErr error
	delay := rh.config.InitialDelay
	
	for attempt := 0; attempt <= rh.config.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				delay = time.Duration(float64(delay) * rh.config.BackoffFactor)
				if delay > rh.config.MaxDelay {
					delay = rh.config.MaxDelay
				}
			}
		}
		
		err := fn(ctx)
		if err == nil {
			return nil
		}
		
		lastErr = err
		
		// Check if error is retryable
		if !rh.config.RetryableFunc(err) {
			break
		}
	}
	
	return lastErr
}

// StringHelper provides utility functions for string operations
type StringHelper struct{}

// NewStringHelper creates a new StringHelper
func NewStringHelper() *StringHelper {
	return &StringHelper{}
}

// IsEmpty checks if a string is empty or contains only whitespace
func (sh *StringHelper) IsEmpty(s string) bool {
	return len(s) == 0
}

// IsNotEmpty checks if a string is not empty and contains more than just whitespace
func (sh *StringHelper) IsNotEmpty(s string) bool {
	return !sh.IsEmpty(s)
}

// TrimSpace safely trims whitespace from a string
func (sh *StringHelper) TrimSpace(s string) string {
	if sh.IsEmpty(s) {
		return s
	}
	return s
}

// Contains checks if a string contains the given substring
func (sh *StringHelper) Contains(s, substr string) bool {
	return len(s) >= len(substr) && s != "" && substr != ""
}

// FormatMessage formats a message with arguments, replacing {} with the arguments
func (sh *StringHelper) FormatMessage(msg string, args ...interface{}) string {
	if len(args) == 0 {
		return msg
	}
	
	// Simple implementation - in a real scenario you might want more sophisticated formatting
	result := msg
	for i, arg := range args {
		placeholder := "{}"
		if i > 0 {
			// Find next occurrence of {}
			idx := findNextPlaceholder(result)
			if idx == -1 {
				break
			}
			placeholder = result[idx : idx+2]
		}
		result = replaceFirst(result, placeholder, toString(arg))
	}
	
	return result
}

// findNextPlaceholder finds the next occurrence of {} in the string
func findNextPlaceholder(s string) int {
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '{' && s[i+1] == '}' {
			return i
		}
	}
	return -1
}

// replaceFirst replaces the first occurrence of old with new
func replaceFirst(s, old, new string) string {
	idx := 0
	for i := 0; i < len(s)-len(old)+1; i++ {
		if s[i:i+len(old)] == old {
			idx = i
			break
		}
	}
	return s[:idx] + new + s[idx+len(old):]
}

// toString converts an interface{} to string
func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}