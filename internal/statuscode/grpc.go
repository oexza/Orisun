//go:build !orisun_embedded

package statuscode

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCStatus lets the gRPC server preserve backend error semantics while the
// transport-neutral error itself remains usable by embedded builds.
func (e *Error) GRPCStatus() *status.Status {
	return status.New(grpcCode(e.code), e.message)
}

func grpcCode(code Code) codes.Code {
	switch code {
	case Canceled:
		return codes.Canceled
	case InvalidArgument:
		return codes.InvalidArgument
	case DeadlineExceeded:
		return codes.DeadlineExceeded
	case NotFound:
		return codes.NotFound
	case AlreadyExists:
		return codes.AlreadyExists
	case PermissionDenied:
		return codes.PermissionDenied
	case Unauthenticated:
		return codes.Unauthenticated
	case Unavailable:
		return codes.Unavailable
	case Unimplemented:
		return codes.Unimplemented
	case Internal:
		return codes.Internal
	default:
		return codes.Unknown
	}
}
