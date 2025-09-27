package middleware

import (
	"context"
	"net/http"
	"time"
)

// TimeoutMiddleware provides request timeout functionality
type TimeoutMiddleware struct {
	timeout time.Duration
}

// NewTimeoutMiddleware creates a new timeout middleware
func NewTimeoutMiddleware(timeout time.Duration) *TimeoutMiddleware {
	return &TimeoutMiddleware{
		timeout: timeout,
	}
}

// Handler returns timeout middleware handler
func (tm *TimeoutMiddleware) Handler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), tm.timeout)
			defer cancel()

			r = r.WithContext(ctx)
			
			// Create a channel to signal completion
			done := make(chan struct{})
			
			go func() {
				defer close(done)
				next.ServeHTTP(w, r)
			}()
			
			select {
			case <-done:
				// Request completed successfully
			case <-ctx.Done():
				// Request timed out
				if ctx.Err() == context.DeadlineExceeded {
					http.Error(w, "Request timeout", http.StatusRequestTimeout)
				}
			}
		})
	}
}

// Timeout is a convenience function that creates a timeout middleware
func Timeout(timeout time.Duration) func(http.Handler) http.Handler {
	middleware := NewTimeoutMiddleware(timeout)
	return middleware.Handler()
}