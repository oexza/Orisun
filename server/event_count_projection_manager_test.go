package server

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestEventCountProjectionManagerRegistersDynamicBoundaryOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	manager := newEventCountProjectionManager(
		ctx, nil, nil, nil, nil, nil, nil, serverTestLogger{},
	)

	manager.StartBoundary("sales")
	manager.StartBoundary("sales")
	manager.StartBoundary("orders")

	manager.mu.Lock()
	defer manager.mu.Unlock()
	if len(manager.running) != 2 {
		t.Fatalf("registered projectors = %#v", manager.running)
	}
	if _, ok := manager.running["sales"]; !ok {
		t.Fatal("dynamic sales projector was not registered")
	}
}

func TestStreamErrorInterceptorPreservesCodedStatus(t *testing.T) {
	interceptor := streamErrorInterceptor(serverTestLogger{})
	want := status.Error(codes.FailedPrecondition, "boundary is not active")
	err := interceptor(
		nil,
		nil,
		&grpc.StreamServerInfo{FullMethod: "/orisun.EventStore/CatchUpSubscribeToEvents"},
		func(any, grpc.ServerStream) error { return want },
	)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stream status code = %s, want %s", status.Code(err), codes.FailedPrecondition)
	}

	err = interceptor(
		nil,
		nil,
		&grpc.StreamServerInfo{FullMethod: "/orisun.EventStore/CatchUpSubscribeToEvents"},
		func(any, grpc.ServerStream) error { return errors.New("uncoded failure") },
	)
	if status.Code(err) != codes.Internal {
		t.Fatalf("uncoded stream status = %s, want %s", status.Code(err), codes.Internal)
	}
}

type serverTestLogger struct{}

func (serverTestLogger) IsDebugEnabled() bool  { return false }
func (serverTestLogger) Debug(...any)          {}
func (serverTestLogger) Debugf(string, ...any) {}
func (serverTestLogger) Info(...any)           {}
func (serverTestLogger) Infof(string, ...any)  {}
func (serverTestLogger) Warn(...any)           {}
func (serverTestLogger) Warnf(string, ...any)  {}
func (serverTestLogger) Error(...any)          {}
func (serverTestLogger) Errorf(string, ...any) {}
func (serverTestLogger) Fatal(...any)          {}
func (serverTestLogger) Fatalf(string, ...any) {}
