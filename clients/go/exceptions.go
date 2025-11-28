package orisun

import (
	"fmt"
	"strings"
)

// OrisunException represents an error that occurred in the Orisun client
type OrisunException struct {
	message string
	cause   error
	context map[string]interface{}
}

// NewOrisunException creates a new OrisunException with the given message
func NewOrisunException(message string) *OrisunException {
	return &OrisunException{
		message: message,
		context: make(map[string]interface{}),
	}
}

// NewOrisunExceptionWithCause creates a new OrisunException with the given message and cause
func NewOrisunExceptionWithCause(message string, cause error) *OrisunException {
	return &OrisunException{
		message: message,
		cause:   cause,
		context: make(map[string]interface{}),
	}
}

// NewOrisunExceptionWithCauseAndContext creates a new OrisunException with the given message, cause, and context
func NewOrisunExceptionWithCauseAndContext(message string, cause error, context map[string]interface{}) *OrisunException {
	return &OrisunException{
		message: message,
		cause:   cause,
		context: context,
	}
}

// Error implements the error interface
func (e *OrisunException) Error() string {
	var sb strings.Builder
	sb.WriteString(e.message)
	
	if len(e.context) > 0 {
		sb.WriteString(" [Context: ")
		first := true
		for key, value := range e.context {
			if !first {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s=%v", key, value))
			first = false
		}
		sb.WriteString("]")
	}
	
	return sb.String()
}

// Unwrap returns the underlying cause
func (e *OrisunException) Unwrap() error {
	return e.cause
}

// AddContext adds context information to the exception
func (e *OrisunException) AddContext(key string, value interface{}) *OrisunException {
	if e.context == nil {
		e.context = make(map[string]interface{})
	}
	e.context[key] = value
	return e
}

// GetContext gets context information by key
func (e *OrisunException) GetContext(key string) (interface{}, bool) {
	if e.context == nil {
		return nil, false
	}
	value, exists := e.context[key]
	return value, exists
}

// GetAllContext returns a copy of all context information
func (e *OrisunException) GetAllContext() map[string]interface{} {
	if e.context == nil {
		return make(map[string]interface{})
	}
	
	result := make(map[string]interface{}, len(e.context))
	for k, v := range e.context {
		result[k] = v
	}
	return result
}

// HasContext checks if context contains a specific key
func (e *OrisunException) HasContext(key string) bool {
	if e.context == nil {
		return false
	}
	_, exists := e.context[key]
	return exists
}

// GetMessage returns the base message without context
func (e *OrisunException) GetMessage() string {
	return e.message
}

// GetCause returns the underlying cause
func (e *OrisunException) GetCause() error {
	return e.cause
}

// OptimisticConcurrencyException represents an error that occurs when there's a version conflict
type OptimisticConcurrencyException struct {
	*OrisunException
	expectedVersion int64
	actualVersion   int64
}

// NewOptimisticConcurrencyException creates a new OptimisticConcurrencyException
func NewOptimisticConcurrencyException(message string, expectedVersion, actualVersion int64) *OptimisticConcurrencyException {
	return &OptimisticConcurrencyException{
		OrisunException: NewOrisunException(message),
		expectedVersion: expectedVersion,
		actualVersion:   actualVersion,
	}
}

// NewOptimisticConcurrencyExceptionWithCause creates a new OptimisticConcurrencyException with a cause
func NewOptimisticConcurrencyExceptionWithCause(message string, expectedVersion, actualVersion int64, cause error) *OptimisticConcurrencyException {
	return &OptimisticConcurrencyException{
		OrisunException: NewOrisunExceptionWithCause(message, cause),
		expectedVersion: expectedVersion,
		actualVersion:   actualVersion,
	}
}

// Error implements the error interface
func (e *OptimisticConcurrencyException) Error() string {
	return fmt.Sprintf("%s (Expected version: %d, Actual version: %d)", 
		e.OrisunException.Error(), e.expectedVersion, e.actualVersion)
}

// GetExpectedVersion returns the expected version
func (e *OptimisticConcurrencyException) GetExpectedVersion() int64 {
	return e.expectedVersion
}

// GetActualVersion returns the actual version
func (e *OptimisticConcurrencyException) GetActualVersion() int64 {
	return e.actualVersion
}