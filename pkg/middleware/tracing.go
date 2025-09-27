package middleware

import (
	"net/http"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// TracingMiddleware provides distributed tracing for HTTP requests
type TracingMiddleware struct {
	tracingManager *tracing.TracingManager
}

// NewTracingMiddleware creates a new tracing middleware
func NewTracingMiddleware(tm *tracing.TracingManager) *TracingMiddleware {
	return &TracingMiddleware{
		tracingManager: tm,
	}
}

// Handler returns tracing middleware handler
func (tm *TracingMiddleware) Handler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tm.tracingManager == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Create span name from HTTP method and path
			spanName := r.Method + " " + r.URL.Path

			// Start span
			ctx, span := tm.tracingManager.StartSpan(r.Context(), spanName)
			defer span.End()

			// Add HTTP attributes
			span.SetAttributes(
				semconv.HTTPRequestMethodKey.String(r.Method),
				semconv.URLFullKey.String(r.URL.String()),
				semconv.UserAgentOriginalKey.String(r.UserAgent()),
				semconv.ClientAddressKey.String(r.RemoteAddr),
				semconv.URLSchemeKey.String(r.URL.Scheme),
				attribute.String("http.path", r.URL.Path),
			)

			// Add request ID if available
			if requestID := GetRequestID(ctx); requestID != "" {
				span.SetAttributes(attribute.String("request.id", requestID))
			}

			// Wrap response writer to capture status code
			lrw := NewLoggingResponseWriter(w)

			// Call next handler with traced context
			next.ServeHTTP(lrw, r.WithContext(ctx))

			// Add response attributes
			span.SetAttributes(
				semconv.HTTPResponseStatusCodeKey.Int(lrw.statusCode),
				semconv.HTTPResponseBodySizeKey.Int(lrw.size),
			)

			// Set span status based on HTTP status code
			if lrw.statusCode >= 400 {
				span.SetStatus(codes.Error, http.StatusText(lrw.statusCode))
			} else {
				span.SetStatus(codes.Ok, "")
			}
		})
	}
}

// Tracing is a convenience function that creates a tracing middleware with a default tracing manager
// This is used when you don't have a specific tracing manager instance
func Tracing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If no tracing manager is configured, just pass through
		next.ServeHTTP(w, r)
	})
}
