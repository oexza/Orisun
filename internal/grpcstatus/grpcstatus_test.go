package grpcstatus

import (
	"testing"

	"github.com/OrisunLabs/Orisun/internal/statuscode"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFromError(t *testing.T) {
	err := FromError(statuscode.New(statuscode.AlreadyExists, "position conflict"))
	if got := status.Code(err); got != codes.AlreadyExists {
		t.Fatalf("status.Code() = %v, want %v", got, codes.AlreadyExists)
	}
	if got := status.Convert(err).Message(); got != "position conflict" {
		t.Fatalf("status message = %q", got)
	}
}
