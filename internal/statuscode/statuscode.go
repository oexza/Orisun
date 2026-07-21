// Package statuscode defines transport-neutral error codes shared by storage
// backends and embedded clients. The server-only adapter maps these errors to
// gRPC statuses without making gRPC a dependency of embedded binaries.
package statuscode

import (
	"context"
	"errors"
	"fmt"
)

type Code uint8

const (
	Unknown Code = iota
	Canceled
	InvalidArgument
	DeadlineExceeded
	NotFound
	AlreadyExists
	PermissionDenied
	Unauthenticated
	Unavailable
	Unimplemented
	Internal
)

type Error struct {
	code    Code
	message string
}

func New(code Code, message string) error {
	return &Error{code: code, message: message}
}

func Errorf(code Code, format string, args ...any) error {
	return New(code, fmt.Sprintf(format, args...))
}

func (e *Error) Error() string {
	return e.message
}

func (e *Error) Code() Code {
	return e.code
}

func FromContextError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return New(Canceled, context.Canceled.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return New(DeadlineExceeded, context.DeadlineExceeded.Error())
	default:
		return New(Unknown, err.Error())
	}
}

func FromError(err error) (Code, string, bool) {
	var coded *Error
	if !errors.As(err, &coded) {
		return Unknown, err.Error(), false
	}
	return coded.code, coded.message, true
}
