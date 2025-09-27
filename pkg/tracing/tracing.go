package tracing

import (
	"context"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// TracingManager handles OpenTelemetry tracing setup
type TracingManager struct {
	tracer oteltrace.Tracer
	tp     *trace.TracerProvider
}

// NewTracingManager creates a new tracing manager
func NewTracingManager(cfg config.TracingConfig) (*TracingManager, error) {
	if !cfg.Enabled {
		// Return no-op tracer if tracing is disabled
		return &TracingManager{
			tracer: otel.Tracer(cfg.ServiceName),
		}, nil
	}

	// Create Jaeger exporter
	exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(cfg.JaegerEndpoint)))
	if err != nil {
		return nil, err
	}

	// Create resource
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, err
	}

	// Create trace provider
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exp),
		trace.WithResource(res),
		trace.WithSampler(trace.TraceIDRatioBased(cfg.SampleRate)),
	)

	// Set global trace provider
	otel.SetTracerProvider(tp)

	// Get tracer
	tracer := tp.Tracer(cfg.ServiceName)

	return &TracingManager{
		tracer: tracer,
		tp:     tp,
	}, nil
}

// StartSpan starts a new span
func (tm *TracingManager) StartSpan(ctx context.Context, spanName string, opts ...oteltrace.SpanStartOption) (context.Context, oteltrace.Span) {
	return tm.tracer.Start(ctx, spanName, opts...)
}

// GetTracer returns the tracer
func (tm *TracingManager) GetTracer() oteltrace.Tracer {
	return tm.tracer
}

// Shutdown cleanly shuts down the tracing manager
func (tm *TracingManager) Shutdown(ctx context.Context) error {
	if tm.tp != nil {
		return tm.tp.Shutdown(ctx)
	}
	return nil
}

// TraceHTTPHandler is a helper to trace HTTP handlers
func (tm *TracingManager) TraceHTTPHandler(operationName string, handler func(ctx context.Context) error) error {
	ctx, span := tm.StartSpan(context.Background(), operationName)
	defer span.End()

	start := time.Now()
	err := handler(ctx)
	duration := time.Since(start)

	// Add attributes to span
	span.SetAttributes(
		semconv.HTTPRequestMethodKey.String("HTTP"),
		attribute.Int("http.request.duration_ms", int(duration.Milliseconds())),
	)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}

	return err
}

// TraceDatabaseOperation traces database operations
// func (tm *TracingManager) TraceDatabaseOperation(ctx context.Context, operation, table string, handler func(ctx context.Context) error) error {
// 	spanName := "db." + operation
// 	if table != "" {
// 		spanName += "." + table
// 	}

// 	ctx, span := tm.StartSpan(ctx, spanName)
// 	defer span.End()

// 	start := time.Now()
// 	err := handler(ctx)
// 	duration := time.Since(start)

// 	// Add attributes
// 	span.SetAttributes(
// 		semconv.DBOperationKey.String(operation),
// 		semconv.DBNameKey.String(table),
// 		attribute.Int("db.duration_ms", int(duration.Milliseconds())),
// 	)

// 	if err != nil {
// 		span.RecordError(err)
// 		span.SetStatus(codes.Error, err.Error())
// 	} else {
// 		span.SetStatus(codes.Ok, "")
// 	}

// 	return err
// }

// TraceServiceCall traces service-to-service calls
func (tm *TracingManager) TraceServiceCall(ctx context.Context, serviceName, method string, handler func(ctx context.Context) error) error {
	spanName := serviceName + "." + method

	ctx, span := tm.StartSpan(ctx, spanName)
	defer span.End()

	start := time.Now()
	err := handler(ctx)
	duration := time.Since(start)

	// Add attributes
	span.SetAttributes(
		semconv.RPCServiceKey.String(serviceName),
		semconv.RPCMethodKey.String(method),
		attribute.Int("rpc.duration_ms", int(duration.Milliseconds())),
	)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}

	return err
}
