package recovery

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// RecoveryMiddleware provides panic recovery with logging
func RecoveryMiddleware(log logger.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					// Log the panic with stack trace
					log.Error("panic recovered",
						logger.String("error", fmt.Sprintf("%v", err)),
						logger.String("stack", string(debug.Stack())),
						logger.String("method", r.Method),
						logger.String("path", r.URL.Path),
						logger.String("remote_addr", r.RemoteAddr),
					)

					// Return 500 Internal Server Error
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}