// Package grpcstatus translates transport-neutral statuscode errors at the
// gRPC boundary. Embedded packages must not import this package.
package grpcstatus

import (
	"github.com/OrisunLabs/Orisun/internal/statuscode"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func FromError(err error) error {
	if err == nil {
		return nil
	}
	code, message, ok := statuscode.FromError(err)
	if !ok {
		return err
	}
	return status.Error(grpcCode(code), message)
}

func grpcCode(code statuscode.Code) codes.Code {
	switch code {
	case statuscode.Canceled:
		return codes.Canceled
	case statuscode.InvalidArgument:
		return codes.InvalidArgument
	case statuscode.DeadlineExceeded:
		return codes.DeadlineExceeded
	case statuscode.NotFound:
		return codes.NotFound
	case statuscode.AlreadyExists:
		return codes.AlreadyExists
	case statuscode.PermissionDenied:
		return codes.PermissionDenied
	case statuscode.Unauthenticated:
		return codes.Unauthenticated
	case statuscode.Unavailable:
		return codes.Unavailable
	case statuscode.Unimplemented:
		return codes.Unimplemented
	case statuscode.FailedPrecondition:
		return codes.FailedPrecondition
	case statuscode.Internal:
		return codes.Internal
	default:
		return codes.Unknown
	}
}
