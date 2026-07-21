package statuscode

import (
	"context"
	"fmt"
	"testing"
)

func TestFromErrorFindsWrappedCode(t *testing.T) {
	err := fmt.Errorf("save failed: %w", New(AlreadyExists, "position conflict"))
	code, message, ok := FromError(err)
	if !ok || code != AlreadyExists || message != "position conflict" {
		t.Fatalf("FromError() = (%v, %q, %v)", code, message, ok)
	}
}

func TestFromContextError(t *testing.T) {
	err := FromContextError(context.DeadlineExceeded)
	code, _, ok := FromError(err)
	if !ok || code != DeadlineExceeded {
		t.Fatalf("FromContextError() code = %v, want %v", code, DeadlineExceeded)
	}
}

func TestCodeOfNil(t *testing.T) {
	if got := CodeOf(nil); got != OK {
		t.Fatalf("CodeOf(nil) = %v, want %v", got, OK)
	}
}
