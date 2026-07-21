//go:build !orisun_embedded

package statuscode

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGRPCStatusAdapter(t *testing.T) {
	err := New(AlreadyExists, "position conflict")
	if got := status.Code(err); got != codes.AlreadyExists {
		t.Fatalf("status.Code() = %v, want %v", got, codes.AlreadyExists)
	}
	if got := status.Convert(err).Message(); got != "position conflict" {
		t.Fatalf("status message = %q", got)
	}
}
