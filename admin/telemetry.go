package admin

import (
	"context"
	"fmt"

	l "github.com/oexza/Orisun/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

var (
	tracer trace.Tracer
)

// InitTracer initializes the OpenTelemetry tracer
func InitTracer(serviceName, otelEndpoint string, logger l.Logger) (func(context.Context) error, error) {
	if otelEndpoint == "" {
		logger.Info("OpenTelemetry endpoint not configured, tracing disabled")
		return nil, nil
	}

	ctx := context.Background()

	// Create OTLP gRPC exporter
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(otelEndpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	// Create resource with service name
	res, err := resource.New(
		ctx,
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("service.version", "1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Get tracer
	tracer = tp.Tracer(serviceName)

	logger.Infof("OpenTelemetry tracer initialized for service: %s, endpoint: %s", serviceName, otelEndpoint)

	// Return shutdown function
	return tp.Shutdown, nil
}

// GetTracer returns the configured tracer
func GetTracer() trace.Tracer {
	if tracer == nil {
		return otel.Tracer("")
	}
	return tracer
}

// UnaryTracingInterceptor returns a gRPC unary interceptor that adds OpenTelemetry tracing
func UnaryTracingInterceptor(logger l.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if tracer == nil {
			// Tracing not enabled, just call handler
			return handler(ctx, req)
		}

		// Start span
		ctx, span := tracer.Start(
			ctx,
			info.FullMethod,
			trace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.service", info.FullMethod),
				attribute.String("rpc.method", info.FullMethod),
			),
		)
		defer span.End()

		// Call handler
		resp, err := handler(ctx, req)

		// Set span status based on error
		if err != nil {
			s, _ := status.FromError(err)
			span.SetAttributes(
				attribute.String("rpc.grpc.status_code", s.Code().String()),
			)
			span.SetStatus(codes.Error, s.Message())
		} else {
			span.SetStatus(codes.Ok, "")
		}

		return resp, err
	}
}

// StreamTracingInterceptor returns a gRPC streaming interceptor that adds OpenTelemetry tracing
func StreamTracingInterceptor(logger l.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if tracer == nil {
			// Tracing not enabled, just call handler
			return handler(srv, ss)
		}

		// Wrap server stream to capture context
		wrapped := &tracedServerStream{
			ServerStream: ss,
			ctx:          ss.Context(),
		}

		// Start span
		ctx, span := tracer.Start(
			wrapped.ctx,
			info.FullMethod,
			trace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.service", info.FullMethod),
				attribute.String("rpc.method", info.FullMethod),
			),
		)
		defer span.End()

		wrapped.ctx = ctx

		// Call handler
		err := handler(srv, wrapped)

		// Set span status based on error
		if err != nil {
			s, _ := status.FromError(err)
			span.SetAttributes(
				attribute.String("rpc.grpc.status_code", s.Code().String()),
			)
			span.SetStatus(codes.Error, s.Message())
		} else {
			span.SetStatus(codes.Ok, "")
		}

		return err
	}
}

// tracedServerStream wraps grpc.ServerStream to use the traced context
type tracedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context returns the traced context
func (t *tracedServerStream) Context() context.Context {
	return t.ctx
}
