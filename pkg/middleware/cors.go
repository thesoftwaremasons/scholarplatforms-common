package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/config"
)

// CORSMiddleware provides CORS functionality
type CORSMiddleware struct {
	config config.CORSConfig
}

// NewCORSMiddleware creates a new CORS middleware
func NewCORSMiddleware(cfg config.CORSConfig) *CORSMiddleware {
	// Set defaults if not provided
	if len(cfg.AllowedOrigins) == 0 {
		cfg.AllowedOrigins = []string{"*"}
	}
	if len(cfg.AllowedMethods) == 0 {
		cfg.AllowedMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	}
	if len(cfg.AllowedHeaders) == 0 {
		cfg.AllowedHeaders = []string{"Content-Type", "Authorization"}
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = 86400 // 24 hours
	}

	return &CORSMiddleware{
		config: cfg,
	}
}

// Handler returns CORS middleware handler
func (c *CORSMiddleware) Handler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is allowed and set appropriate header
			if origin != "" && c.isOriginAllowed(origin) {
				// If credentials are allowed, we must set specific origin (not *)
				if c.config.AllowCredentials {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				} else {
					// Check if wildcard is in allowed origins
					hasWildcard := false
					for _, allowedOrigin := range c.config.AllowedOrigins {
						if allowedOrigin == "*" {
							hasWildcard = true
							break
						}
					}
					if hasWildcard {
						w.Header().Set("Access-Control-Allow-Origin", "*")
					} else {
						w.Header().Set("Access-Control-Allow-Origin", origin)
					}
				}
			}

			// Set allowed methods
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(c.config.AllowedMethods, ", "))

			// Set allowed headers
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(c.config.AllowedHeaders, ", "))

			// Set exposed headers
			if len(c.config.ExposedHeaders) > 0 {
				w.Header().Set("Access-Control-Expose-Headers", strings.Join(c.config.ExposedHeaders, ", "))
			}

			// Set credentials (only if we set a specific origin)
			if c.config.AllowCredentials && origin != "" {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			// Set max age for preflight requests
			w.Header().Set("Access-Control-Max-Age", strconv.Itoa(c.config.MaxAge))

			// Handle preflight requests
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isOriginAllowed checks if the origin is in the allowed list
func (c *CORSMiddleware) isOriginAllowed(origin string) bool {
	if origin == "" {
		return false
	}

	for _, allowedOrigin := range c.config.AllowedOrigins {
		if allowedOrigin == "*" || allowedOrigin == origin {
			return true
		}

		// Support wildcard subdomains
		if strings.Contains(allowedOrigin, "*") {
			if c.matchWildcard(allowedOrigin, origin) {
				return true
			}
		}
	}

	return false
}

// matchWildcard checks if origin matches wildcard pattern
func (c *CORSMiddleware) matchWildcard(pattern, origin string) bool {
	// Simple wildcard matching - replace * with regex equivalent
	pattern = strings.ReplaceAll(pattern, "*", ".*")

	// This is a simplified implementation
	// For production, consider using regexp or more sophisticated matching
	if strings.HasPrefix(pattern, ".*") {
		suffix := pattern[2:] // Remove .*
		return strings.HasSuffix(origin, suffix)
	}

	if strings.HasSuffix(pattern, ".*") {
		prefix := pattern[:len(pattern)-2] // Remove .*
		return strings.HasPrefix(origin, prefix)
	}

	return false
}

// CORS is a convenience function that creates a default CORS middleware
func CORS(next http.Handler) http.Handler {
	// Default CORS configuration
	corsMiddleware := NewCORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{
			"http://localhost:3005",
			"http://localhost:3006",
			"http://localhost:3000",
			"http://127.0.0.1:3005",
			"http://127.0.0.1:3006",
			"http://127.0.0.1:3000",
			// Support subdomain-based multi-tenancy
			"http://*.localtest.me:3005",
			"http://*.localtest.me:3006",
			"http://*.localtest.me:3000",
		},
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{
			"Content-Type",
			"Authorization",
			"X-Request-ID",
			"X-User-ID",
			"X-User-Email",
			"X-User-Name",
			"X-User-Role",
			"X-Organization-ID",
			"X-School-ID",
			"X-Session-Token",
			"X-Session-Data",
			"X-Service-Token",
		},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           86400,
	})

	return corsMiddleware.Handler()(next)
}
