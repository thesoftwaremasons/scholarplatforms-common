// Package middleware provides enhanced rate limiting with per-service and per-endpoint controls
package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
	"golang.org/x/time/rate"
)

// EnhancedRateLimiter provides sophisticated rate limiting capabilities
type EnhancedRateLimiter struct {
	// Global limits
	globalLimiter *rate.Limiter

	// Service-specific limiters
	serviceLimiters map[string]*rate.Limiter
	serviceConfig   map[string]ServiceLimitConfig

	// Endpoint-specific limiters
	endpointLimiters map[string]*rate.Limiter
	endpointConfig   map[string]EndpointLimitConfig

	// Client IP limiters
	clientLimiters map[string]*rate.Limiter
	clientConfig   ClientLimitConfig

	// Cleanup tracking
	lastCleanup     time.Time
	cleanupInterval time.Duration

	// Synchronization
	mu sync.RWMutex

	logger logger.Logger
}

// ServiceLimitConfig defines rate limits for a specific service
type ServiceLimitConfig struct {
	RequestsPerSecond float64
	BurstSize         int
	Enabled           bool
}

// EndpointLimitConfig defines rate limits for specific endpoints
type EndpointLimitConfig struct {
	RequestsPerSecond float64
	BurstSize         int
	Methods           []string // HTTP methods this limit applies to
	Enabled           bool
}

// ClientLimitConfig defines rate limits per client IP
type ClientLimitConfig struct {
	RequestsPerSecond float64
	BurstSize         int
	Enabled           bool
	BlockDuration     time.Duration
}

// EnhancedRateLimiterConfig holds configuration for the enhanced rate limiter
type EnhancedRateLimiterConfig struct {
	// Global settings
	GlobalRequestsPerSecond float64
	GlobalBurstSize         int

	// Service-specific limits
	ServiceLimits map[string]ServiceLimitConfig

	// Endpoint-specific limits
	EndpointLimits map[string]EndpointLimitConfig

	// Client IP limits
	ClientLimits ClientLimitConfig

	// Cleanup settings
	CleanupInterval time.Duration

	// Headers to return in responses
	IncludeHeaders bool

	Logger logger.Logger
}

// NewEnhancedRateLimiter creates a new enhanced rate limiter
func NewEnhancedRateLimiter(config EnhancedRateLimiterConfig) *EnhancedRateLimiter {
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 5 * time.Minute
	}

	if config.GlobalRequestsPerSecond <= 0 {
		config.GlobalRequestsPerSecond = 1000 // Default: 1000 RPS
	}

	if config.GlobalBurstSize <= 0 {
		config.GlobalBurstSize = 100 // Default burst
	}

	rl := &EnhancedRateLimiter{
		globalLimiter:    rate.NewLimiter(rate.Limit(config.GlobalRequestsPerSecond), config.GlobalBurstSize),
		serviceLimiters:  make(map[string]*rate.Limiter),
		serviceConfig:    config.ServiceLimits,
		endpointLimiters: make(map[string]*rate.Limiter),
		endpointConfig:   config.EndpointLimits,
		clientLimiters:   make(map[string]*rate.Limiter),
		clientConfig:     config.ClientLimits,
		lastCleanup:      time.Now(),
		cleanupInterval:  config.CleanupInterval,
		logger:           config.Logger,
	}

	// Initialize service limiters
	for serviceName, serviceConfig := range config.ServiceLimits {
		if serviceConfig.Enabled {
			rl.serviceLimiters[serviceName] = rate.NewLimiter(
				rate.Limit(serviceConfig.RequestsPerSecond),
				serviceConfig.BurstSize,
			)
		}
	}

	// Initialize endpoint limiters
	for endpoint, endpointConfig := range config.EndpointLimits {
		if endpointConfig.Enabled {
			rl.endpointLimiters[endpoint] = rate.NewLimiter(
				rate.Limit(endpointConfig.RequestsPerSecond),
				endpointConfig.BurstSize,
			)
		}
	}

	return rl
}

// Middleware returns the rate limiting middleware function
func (rl *EnhancedRateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Perform periodic cleanup
			rl.periodicCleanup()

			// Extract identifiers
			serviceName := rl.extractServiceName(r)
			endpoint := rl.extractEndpoint(r)
			clientIP := rl.extractClientIP(r)

			// Check global limit
			if !rl.checkGlobalLimit() {
				rl.handleRateLimit(w, r, "global", 0)
				return
			}

			// Check service-specific limit
			if serviceName != "" {
				if allowed, retryAfter := rl.checkServiceLimit(serviceName); !allowed {
					rl.handleRateLimit(w, r, "service", retryAfter)
					return
				}
			}

			// Check endpoint-specific limit
			if endpoint != "" {
				if allowed, retryAfter := rl.checkEndpointLimit(endpoint, r.Method); !allowed {
					rl.handleRateLimit(w, r, "endpoint", retryAfter)
					return
				}
			}

			// Check client IP limit
			if rl.clientConfig.Enabled && clientIP != "" {
				if allowed, retryAfter := rl.checkClientLimit(clientIP); !allowed {
					rl.handleRateLimit(w, r, "client", retryAfter)
					return
				}
			}

			// Request is allowed, proceed
			next.ServeHTTP(w, r)
		})
	}
}

// checkGlobalLimit checks the global rate limit
func (rl *EnhancedRateLimiter) checkGlobalLimit() bool {
	return rl.globalLimiter.Allow()
}

// checkServiceLimit checks service-specific rate limit
func (rl *EnhancedRateLimiter) checkServiceLimit(serviceName string) (bool, time.Duration) {
	rl.mu.RLock()
	limiter, exists := rl.serviceLimiters[serviceName]
	rl.mu.RUnlock()

	if !exists {
		return true, 0 // No limit configured for this service
	}

	if limiter.Allow() {
		return true, 0
	}

	// Calculate retry after duration
	retryAfter := time.Duration(1.0/float64(limiter.Limit())) * time.Second
	return false, retryAfter
}

// checkEndpointLimit checks endpoint-specific rate limit
func (rl *EnhancedRateLimiter) checkEndpointLimit(endpoint, method string) (bool, time.Duration) {
	rl.mu.RLock()
	limiter, exists := rl.endpointLimiters[endpoint]
	config, configExists := rl.endpointConfig[endpoint]
	rl.mu.RUnlock()

	if !exists || !configExists {
		return true, 0 // No limit configured for this endpoint
	}

	// Check if method is allowed for this limit
	if len(config.Methods) > 0 {
		methodAllowed := false
		for _, allowedMethod := range config.Methods {
			if strings.EqualFold(allowedMethod, method) {
				methodAllowed = true
				break
			}
		}
		if !methodAllowed {
			return true, 0 // Method not subject to this limit
		}
	}

	if limiter.Allow() {
		return true, 0
	}

	retryAfter := time.Duration(1.0/float64(limiter.Limit())) * time.Second
	return false, retryAfter
}

// checkClientLimit checks client IP rate limit
func (rl *EnhancedRateLimiter) checkClientLimit(clientIP string) (bool, time.Duration) {
	rl.mu.Lock()
	limiter, exists := rl.clientLimiters[clientIP]
	if !exists {
		// Create new limiter for this client
		limiter = rate.NewLimiter(
			rate.Limit(rl.clientConfig.RequestsPerSecond),
			rl.clientConfig.BurstSize,
		)
		rl.clientLimiters[clientIP] = limiter
	}
	rl.mu.Unlock()

	if limiter.Allow() {
		return true, 0
	}

	retryAfter := time.Duration(1.0/float64(limiter.Limit())) * time.Second
	return false, retryAfter
}

// extractServiceName extracts service name from request
func (rl *EnhancedRateLimiter) extractServiceName(r *http.Request) string {
	// Check header first
	if serviceName := r.Header.Get("X-Service-Name"); serviceName != "" {
		return serviceName
	}

	// Extract from User-Agent
	userAgent := r.Header.Get("User-Agent")
	if strings.Contains(userAgent, "scholarplatforms-") {
		parts := strings.Split(userAgent, "-")
		if len(parts) >= 2 {
			return parts[1] // Return service name part
		}
	}

	// Extract from path (e.g., /api/notification/...)
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) >= 2 && pathParts[0] == "api" {
		return pathParts[1]
	}

	return ""
}

// extractEndpoint extracts endpoint identifier from request
func (rl *EnhancedRateLimiter) extractEndpoint(r *http.Request) string {
	// Normalize path by removing query parameters and trailing slashes
	endpoint := strings.TrimSuffix(r.URL.Path, "/")
	if endpoint == "" {
		endpoint = "/"
	}
	return endpoint
}

// extractClientIP extracts client IP from request
func (rl *EnhancedRateLimiter) extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the chain
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Use remote address
	clientIP := r.RemoteAddr
	// Remove port if present
	if colonIndex := strings.LastIndex(clientIP, ":"); colonIndex != -1 {
		clientIP = clientIP[:colonIndex]
	}

	return clientIP
}

// handleRateLimit handles rate limit exceeded scenarios
func (rl *EnhancedRateLimiter) handleRateLimit(w http.ResponseWriter, r *http.Request, limitType string, retryAfter time.Duration) {
	// Set rate limit headers
	w.Header().Set("X-RateLimit-Limit-Type", limitType)
	w.Header().Set("X-RateLimit-Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
	w.Header().Set("Content-Type", "application/json")

	// Log rate limit event
	rl.logger.Warn("Rate limit exceeded",
		logger.String("limit_type", limitType),
		logger.String("client_ip", rl.extractClientIP(r)),
		logger.String("service", rl.extractServiceName(r)),
		logger.String("endpoint", rl.extractEndpoint(r)),
		logger.String("method", r.Method),
		logger.Duration("retry_after", retryAfter))

	w.WriteHeader(http.StatusTooManyRequests)
	response := fmt.Sprintf(`{
		"error": "Rate limit exceeded",
		"limit_type": "%s",
		"retry_after": %d,
		"message": "Too many requests. Please wait before retrying."
	}`, limitType, int(retryAfter.Seconds()))

	w.Write([]byte(response))
}

// periodicCleanup removes old client limiters to prevent memory leaks
func (rl *EnhancedRateLimiter) periodicCleanup() {
	now := time.Now()
	if now.Sub(rl.lastCleanup) < rl.cleanupInterval {
		return
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Clean up client limiters that haven't been used recently
	for clientIP, limiter := range rl.clientLimiters {
		// If the limiter's token bucket is full and hasn't been used recently, remove it
		if limiter.Tokens() == float64(rl.clientConfig.BurstSize) {
			delete(rl.clientLimiters, clientIP)
		}
	}

	rl.lastCleanup = now
	rl.logger.Debug("Performed rate limiter cleanup",
		logger.Int("remaining_clients", len(rl.clientLimiters)))
}

// GetStats returns current rate limiter statistics
func (rl *EnhancedRateLimiter) GetStats() map[string]interface{} {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	return map[string]interface{}{
		"global_tokens":     rl.globalLimiter.Tokens(),
		"active_clients":    len(rl.clientLimiters),
		"service_limiters":  len(rl.serviceLimiters),
		"endpoint_limiters": len(rl.endpointLimiters),
		"last_cleanup":      rl.lastCleanup,
	}
}
