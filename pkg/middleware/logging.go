package middleware

import (
	"net/http"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// LoggingResponseWriter wraps http.ResponseWriter to capture status code and size
type LoggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	size       int
}

// NewLoggingResponseWriter creates a new logging response writer
func NewLoggingResponseWriter(w http.ResponseWriter) *LoggingResponseWriter {
	return &LoggingResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

// WriteHeader captures the status code
func (lrw *LoggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

// Write captures the response size
func (lrw *LoggingResponseWriter) Write(data []byte) (int, error) {
	size, err := lrw.ResponseWriter.Write(data)
	lrw.size += size
	return size, err
}

// LoggingMiddleware provides HTTP request logging
type LoggingMiddleware struct {
	logger logger.Logger
}

// NewLoggingMiddleware creates a new logging middleware
func NewLoggingMiddleware(log logger.Logger) *LoggingMiddleware {
	return &LoggingMiddleware{
		logger: log,
	}
}

// Handler returns logging middleware handler
func (lm *LoggingMiddleware) Handler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer
			lrw := NewLoggingResponseWriter(w)

			// Call next handler
			next.ServeHTTP(lrw, r)

			// Log request details
			duration := time.Since(start)

			lm.logger.Info("HTTP request",
				logger.String("method", r.Method),
				logger.String("path", r.URL.Path),
				logger.String("query", r.URL.RawQuery),
				logger.String("remote_addr", r.RemoteAddr),
				logger.String("user_agent", r.UserAgent()),
				logger.Int("status_code", lrw.statusCode),
				logger.Int("response_size", lrw.size),
				logger.Duration("duration", duration),
			)

			// Log errors for non-2xx status codes
			if lrw.statusCode >= 400 {
				lm.logger.Warn("HTTP error response",
					logger.String("method", r.Method),
					logger.String("path", r.URL.Path),
					logger.Int("status_code", lrw.statusCode),
					logger.Duration("duration", duration),
				)
			}
		})
	}
}

// Recovery middleware provides panic recovery
type RecoveryMiddleware struct {
	logger logger.Logger
}

// NewRecoveryMiddleware creates a new recovery middleware
func NewRecoveryMiddleware(log logger.Logger) *RecoveryMiddleware {
	return &RecoveryMiddleware{
		logger: log,
	}
}

// Handler returns recovery middleware handler
func (rm *RecoveryMiddleware) Handler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					rm.logger.Error("Panic recovered",
						logger.String("method", r.Method),
						logger.String("path", r.URL.Path),
						logger.Any("panic", err),
					)

					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// Recovery is a convenience function that creates a default recovery middleware
func Recovery(log logger.Logger) func(http.Handler) http.Handler {
	recovery := NewRecoveryMiddleware(log)
	return recovery.Handler()
}

// RequestLogger is a convenience function that creates a default logging middleware
func RequestLogger(log logger.Logger) func(http.Handler) http.Handler {
	logging := NewLoggingMiddleware(log)
	return logging.Handler()
}
