package middleware

import (
	"net/http"
	"strings"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// AccessControlConfig configures access control middleware
type AccessControlConfig struct {
	AllowDirectAccess    bool     `json:"allow_direct_access"`
	RequiredHeaders      []string `json:"required_headers"`
	BlockedUserAgents    []string `json:"blocked_user_agents"`
	AllowedSourceIPs     []string `json:"allowed_source_ips"`
	RequireServiceHeader bool     `json:"require_service_header"`
	ServiceHeaderName    string   `json:"service_header_name"`
	ServiceHeaderValue   string   `json:"service_header_value"`
	
	// Legacy fields for backward compatibility
	AllowedGatewayIPs []string `json:"allowed_gateway_ips"`
	AllowedNetworks   []string `json:"allowed_networks"`
	GatewayUserAgent  string   `json:"gateway_user_agent"`
	EnforceMode       bool     `json:"enforce_mode"`
}

// AccessControlMiddleware provides access control functionality
type AccessControlMiddleware struct {
	config AccessControlConfig
	logger logger.Logger
}

// NewAccessControlMiddleware creates a new access control middleware
func NewAccessControlMiddleware(config AccessControlConfig, log logger.Logger) (*AccessControlMiddleware, error) {
	// Set default service header name if not specified
	if config.ServiceHeaderName == "" && config.RequireServiceHeader {
		config.ServiceHeaderName = "X-Service-Name"
	}

	return &AccessControlMiddleware{
		config: config,
		logger: log,
	}, nil
}

// Middleware returns the access control middleware handler
func (a *AccessControlMiddleware) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If direct access is allowed, skip all checks
			if a.config.AllowDirectAccess {
				next.ServeHTTP(w, r)
				return
			}

			// Check required headers
			for _, header := range a.config.RequiredHeaders {
				if r.Header.Get(header) == "" {
					a.logger.Warn("Missing required header",
						logger.String("header", header),
						logger.String("remote_addr", r.RemoteAddr),
					)
					http.Error(w, "Access denied: missing required headers", http.StatusForbidden)
					return
				}
			}

			// Check service header if required
			if a.config.RequireServiceHeader {
				serviceHeader := r.Header.Get(a.config.ServiceHeaderName)
				if serviceHeader == "" {
					a.logger.Warn("Missing required service header",
						logger.String("header", a.config.ServiceHeaderName),
						logger.String("remote_addr", r.RemoteAddr),
					)
					http.Error(w, "Access denied: service identification required", http.StatusForbidden)
					return
				}

				if a.config.ServiceHeaderValue != "" && serviceHeader != a.config.ServiceHeaderValue {
					a.logger.Warn("Invalid service header value",
						logger.String("header", a.config.ServiceHeaderName),
						logger.String("expected", a.config.ServiceHeaderValue),
						logger.String("actual", serviceHeader),
						logger.String("remote_addr", r.RemoteAddr),
					)
					http.Error(w, "Access denied: invalid service identification", http.StatusForbidden)
					return
				}
			}

			// Check blocked user agents
			userAgent := r.Header.Get("User-Agent")
			for _, blockedAgent := range a.config.BlockedUserAgents {
				if strings.Contains(strings.ToLower(userAgent), strings.ToLower(blockedAgent)) {
					a.logger.Warn("Blocked user agent detected",
						logger.String("user_agent", userAgent),
						logger.String("blocked_pattern", blockedAgent),
						logger.String("remote_addr", r.RemoteAddr),
					)
					http.Error(w, "Access denied: user agent blocked", http.StatusForbidden)
					return
				}
			}

			// Check allowed source IPs (if specified)
			if len(a.config.AllowedSourceIPs) > 0 {
				remoteIP := a.getClientIP(r)
				allowed := false
				for _, allowedIP := range a.config.AllowedSourceIPs {
					if remoteIP == allowedIP || strings.HasPrefix(remoteIP, allowedIP) {
						allowed = true
						break
					}
				}
				if !allowed {
					a.logger.Warn("IP address not in allowed list",
						logger.String("remote_ip", remoteIP),
						logger.Any("allowed_ips", a.config.AllowedSourceIPs),
					)
					http.Error(w, "Access denied: IP not allowed", http.StatusForbidden)
					return
				}
			}

			// All checks passed, continue to next handler
			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP gets the real client IP address from request headers
func (a *AccessControlMiddleware) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the list
		if idx := strings.Index(xff, ","); idx >= 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx >= 0 {
		return r.RemoteAddr[:idx]
	}
	return r.RemoteAddr
}

// EnforceGatewayAccess returns a middleware that enforces gateway access
func (a *AccessControlMiddleware) EnforceGatewayAccess() func(http.Handler) http.Handler {
	return a.Middleware()
}