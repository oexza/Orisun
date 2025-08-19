package admin

import (
	"context"
	"encoding/base64"
	"log"
	"strings"
	"time"

	globalCommon "orisun/common"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

//middleware to measure performance of grpc requests
func UnaryPerformanceInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		elapsed := time.Since(start)
		log.Printf("Unary request %s took %v", info.FullMethod, elapsed)
		return resp, err
	}
}

func UnaryAuthInterceptor(auth *Authenticator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		user, err := authenticate(ctx, auth)
		if err != nil {
			return nil, err
		}

		newCtx := context.WithValue(ctx, globalCommon.UserContextKey, user)
		return handler(newCtx, req)
	}
}

func StreamAuthInterceptor(auth *Authenticator) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		user, err := authenticate(ss.Context(), auth)
		if err != nil {
			return err
		}

		newCtx := context.WithValue(ss.Context(), globalCommon.UserContextKey, user)
		wrapped := newWrappedServerStream(ss, newCtx)
		return handler(srv, wrapped)
	}
}

func authenticate(ctx context.Context, auth *Authenticator) (globalCommon.User, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return globalCommon.User{}, status.Error(codes.Unauthenticated, "missing metadata")
	}

	values := md.Get("authorization")
	if len(values) == 0 {
		return globalCommon.User{}, status.Error(codes.Unauthenticated, "missing authorization header")
	}

	authHeader := values[0]
	if !strings.HasPrefix(authHeader, "Basic ") {
		return globalCommon.User{}, status.Error(codes.Unauthenticated, "invalid authorization header format")
	}

	encoded := strings.TrimPrefix(authHeader, "Basic ")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return globalCommon.User{}, status.Error(codes.Unauthenticated, "invalid authorization header encoding")
	}

	credentials := strings.SplitN(string(decoded), ":", 2)
	if len(credentials) != 2 {
		return globalCommon.User{}, status.Error(codes.Unauthenticated, "invalid authorization header format")
	}

	user, err := auth.ValidateCredentials(credentials[0], credentials[1])
	if err != nil {
		return globalCommon.User{}, status.Error(codes.Unauthenticated, "invalid credentials")
	}

	return user, nil
}

type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func newWrappedServerStream(ss grpc.ServerStream, ctx context.Context) grpc.ServerStream {
	return &wrappedServerStream{
		ServerStream: ss,
		ctx:          ctx,
	}
}

func (w *wrappedServerStream) Context() context.Context {
	return w.ctx
}
