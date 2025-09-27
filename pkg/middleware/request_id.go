package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// RequestIDHeader is the header name for request ID
const RequestIDHeader = "X-Request-ID"

// requestIDKey is the context key for request ID
type requestIDKey struct{}

// RequestID middleware adds a unique request ID to each request
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(RequestIDHeader)

		if requestID == "" {
			// Generate a new request ID
			requestID = generateRequestID()
		}

		// Add request ID to response header
		w.Header().Set(RequestIDHeader, requestID)

		// Add request ID to context
		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID retrieves the request ID from context
func GetRequestID(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDKey{}).(string); ok {
		return requestID
	}
	return ""
}

// generateRequestID generates a unique request ID
func generateRequestID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID if random generation fails
		return hex.EncodeToString([]byte("fallback"))[:16]
	}
	return hex.EncodeToString(bytes)
}
