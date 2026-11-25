package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/config"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	tokens     int
	maxTokens  int
	refillRate time.Duration
	lastRefill time.Time
	mutex      sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(maxTokens int, refillRate time.Duration) *RateLimiter {
	return &RateLimiter{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

// Allow checks if a request is allowed
func (rl *RateLimiter) Allow() bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastRefill)

	// Refill tokens based on elapsed time
	tokensToAdd := int(elapsed / rl.refillRate)
	if tokensToAdd > 0 {
		rl.tokens = min(rl.maxTokens, rl.tokens+tokensToAdd)
		rl.lastRefill = now
	}

	// Check if we have tokens available
	if rl.tokens > 0 {
		rl.tokens--
		return true
	}

	return false
}

// RateLimitMiddleware provides rate limiting functionality
type RateLimitMiddleware struct {
	config   config.RateLimitConfig
	limiters map[string]*RateLimiter
	mutex    sync.RWMutex
	logger   logger.Logger
}

// NewRateLimitMiddleware creates a new rate limiting middleware
func NewRateLimitMiddleware(cfg config.RateLimitConfig, log logger.Logger) *RateLimitMiddleware {
	return &RateLimitMiddleware{
		config:   cfg,
		limiters: make(map[string]*RateLimiter),
		logger:   log,
	}
}

// Handler returns rate limiting middleware handler
func (rlm *RateLimitMiddleware) Handler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rlm.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			clientIP := getClientIPFromRequest(r)
			limiter := rlm.getLimiter(clientIP)

			if !limiter.Allow() {
				if rlm.logger != nil {
					rlm.logger.Warn("Rate limit exceeded",
						logger.String("client_ip", clientIP),
						logger.String("path", r.URL.Path),
					)
				}

				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", rlm.config.RequestsPerMinute))
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Minute).Unix()))

				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			// Add rate limit headers
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", rlm.config.RequestsPerMinute))

			next.ServeHTTP(w, r)
		})
	}
}

// getLimiter gets or creates a rate limiter for the given key
func (rlm *RateLimitMiddleware) getLimiter(key string) *RateLimiter {
	rlm.mutex.RLock()
	limiter, exists := rlm.limiters[key]
	rlm.mutex.RUnlock()

	if exists {
		return limiter
	}

	rlm.mutex.Lock()
	defer rlm.mutex.Unlock()

	// Check again in case it was created while waiting for lock
	if limiter, exists := rlm.limiters[key]; exists {
		return limiter
	}

	// Create new limiter
	refillRate := time.Minute / time.Duration(rlm.config.RequestsPerMinute)
	limiter = NewRateLimiter(rlm.config.Burst, refillRate)
	rlm.limiters[key] = limiter

	return limiter
}

// getClientIP extracts the client IP from the request
func (rlm *RateLimitMiddleware) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// Take the first IP in the list
		ips := strings.Split(forwarded, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return realIP
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return ip
}

// Cleanup removes old limiters (should be called periodically)
func (rlm *RateLimitMiddleware) Cleanup() {
	rlm.mutex.Lock()
	defer rlm.mutex.Unlock()

	// Simple cleanup - remove limiters that haven't been used recently
	// In a production system, you might want more sophisticated cleanup
	for key, limiter := range rlm.limiters {
		if time.Since(limiter.lastRefill) > 10*time.Minute {
			delete(rlm.limiters, key)
		}
	}
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RateLimit is a convenience function that creates a default rate limiter middleware
func RateLimit(requestsPerMinute int, window time.Duration) func(http.Handler) http.Handler {
	// Convert to config format
	cfg := config.RateLimitConfig{
		Enabled:           true,
		RequestsPerMinute: requestsPerMinute,
		Burst:             requestsPerMinute / 2, // Set burst to half of requests per minute
	}

	// For now, we'll create a simple logger instance (this might need to be injected)
	limiter := NewRateLimitMiddleware(cfg, nil)
	return limiter.Handler()
}

// Enhanced Rate Limiting Types for notification service compatibility

// RateLimitConfig provides enhanced configuration for rate limiting
type RateLimitConfig struct {
	Enabled       bool                    `yaml:"enabled" json:"enabled"`
	GlobalLimits  GlobalLimits            `yaml:"global_limits" json:"global_limits"`
	ServiceLimits map[string]ServiceLimit `yaml:"service_limits" json:"service_limits"`
	IPWhitelist   []string                `yaml:"ip_whitelist" json:"ip_whitelist"`

	// Legacy fields for backward compatibility
	GlobalRequestsPerMinute int           `yaml:"global_requests_per_minute" json:"global_requests_per_minute"`
	GlobalBurstSize         int           `yaml:"global_burst_size" json:"global_burst_size"`
	UserRequestsPerMinute   int           `yaml:"user_requests_per_minute" json:"user_requests_per_minute"`
	UserBurstSize           int           `yaml:"user_burst_size" json:"user_burst_size"`
	CleanupInterval         time.Duration `yaml:"cleanup_interval" json:"cleanup_interval"`
	BlockDuration           time.Duration `yaml:"block_duration" json:"block_duration"`
	EnableMetrics           bool          `yaml:"enable_metrics" json:"enable_metrics"`
}

// GlobalLimits defines global rate limits
type GlobalLimits struct {
	RequestsPerSecond int `yaml:"requests_per_second" json:"requests_per_second"`
	BurstSize         int `yaml:"burst_size" json:"burst_size"`
}

// ServiceLimit defines rate limits per service
type ServiceLimit struct {
	RequestsPerSecond int `yaml:"requests_per_second" json:"requests_per_second"`
	BurstSize         int `yaml:"burst_size" json:"burst_size"`
	MaxConcurrent     int `yaml:"max_concurrent" json:"max_concurrent"`
}

// RateLimitManager provides enhanced rate limiting functionality
type RateLimitManager struct {
	config          RateLimitConfig
	globalLimiters  map[string]*RateLimiter
	serviceLimiters map[string]map[string]*RateLimiter // service -> client -> limiter
	mutex           sync.RWMutex
	logger          logger.Logger
}

// NewRateLimitManager creates a new enhanced rate limit manager
func NewRateLimitManager(config RateLimitConfig, log logger.Logger) *RateLimitManager {
	return &RateLimitManager{
		config:          config,
		globalLimiters:  make(map[string]*RateLimiter),
		serviceLimiters: make(map[string]map[string]*RateLimiter),
		logger:          log,
	}
}

// ServiceRateLimit returns a rate limiting middleware for a specific service
func (rlm *RateLimitManager) ServiceRateLimit(serviceName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rlm.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			clientIP := getClientIPFromRequest(r)

			// Check if IP is whitelisted
			if rlm.isWhitelisted(clientIP) {
				next.ServeHTTP(w, r)
				return
			}

			// Get service-specific limiter
			limiter := rlm.getServiceLimiter(serviceName, clientIP)

			if !limiter.Allow() {
				rlm.logger.Warn("Service rate limit exceeded",
					logger.String("service", serviceName),
					logger.String("client_ip", clientIP),
					logger.String("path", r.URL.Path),
				)

				serviceLimit := rlm.config.ServiceLimits[serviceName]
				w.Header().Set("X-RateLimit-Service", serviceName)
				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", serviceLimit.RequestsPerSecond))
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Second).Unix()))

				http.Error(w, "Service rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getServiceLimiter gets or creates a rate limiter for a service and client
func (rlm *RateLimitManager) getServiceLimiter(serviceName, clientKey string) *RateLimiter {
	rlm.mutex.RLock()
	serviceMap, exists := rlm.serviceLimiters[serviceName]
	if exists {
		if limiter, exists := serviceMap[clientKey]; exists {
			rlm.mutex.RUnlock()
			return limiter
		}
	}
	rlm.mutex.RUnlock()

	rlm.mutex.Lock()
	defer rlm.mutex.Unlock()

	// Ensure service map exists
	if rlm.serviceLimiters[serviceName] == nil {
		rlm.serviceLimiters[serviceName] = make(map[string]*RateLimiter)
	}

	// Check again in case it was created while waiting for lock
	if limiter, exists := rlm.serviceLimiters[serviceName][clientKey]; exists {
		return limiter
	}

	// Get service-specific config or use defaults
	serviceLimit, exists := rlm.config.ServiceLimits[serviceName]
	if !exists {
		serviceLimit = ServiceLimit{
			RequestsPerSecond: rlm.config.GlobalLimits.RequestsPerSecond,
			BurstSize:         rlm.config.GlobalLimits.BurstSize,
			MaxConcurrent:     100,
		}
	}

	// Create new limiter
	refillRate := time.Second / time.Duration(serviceLimit.RequestsPerSecond)
	limiter := NewRateLimiter(serviceLimit.BurstSize, refillRate)
	rlm.serviceLimiters[serviceName][clientKey] = limiter

	return limiter
}

// isWhitelisted checks if an IP is whitelisted
func (rlm *RateLimitManager) isWhitelisted(ip string) bool {
	for _, whitelistedIP := range rlm.config.IPWhitelist {
		if ip == whitelistedIP || strings.HasPrefix(ip, whitelistedIP) {
			return true
		}
	}
	return false
}

// RateLimiterFunc convenience function for backward compatibility
func RateLimiterFunc(requestsPerMin int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		limiters := make(map[string]*RateLimiter)
		var mutex sync.RWMutex

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP := getClientIPFromRequest(r)

			mutex.RLock()
			limiter, exists := limiters[clientIP]
			mutex.RUnlock()

			if !exists {
				mutex.Lock()
				if limiter, exists = limiters[clientIP]; !exists {
					refillRate := window / time.Duration(requestsPerMin)
					limiter = NewRateLimiter(requestsPerMin, refillRate)
					limiters[clientIP] = limiter
				}
				mutex.Unlock()
			}

			if !limiter.Allow() {
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getClientIPFromRequest extracts client IP from request (helper function)
func getClientIPFromRequest(r *http.Request) string {
	// Check X-Forwarded-For header
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		ips := strings.Split(forwarded, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return realIP
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return ip
}

// RateLimiterMiddleware is an alias for RateLimiterFunc for backward compatibility
func RateLimiterMiddleware(requestsPerMin int, window time.Duration) func(http.Handler) http.Handler {
	return RateLimiterFunc(requestsPerMin, window)
}

// RateLimitMiddleware returns a rate limiting middleware for the manager
func (rlm *RateLimitManager) RateLimitMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rlm.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			clientIP := getClientIPFromRequest(r)

			// Check if IP is whitelisted
			if rlm.isWhitelisted(clientIP) {
				next.ServeHTTP(w, r)
				return
			}

			// Get global limiter
			limiter := rlm.getGlobalLimiter(clientIP)

			if !limiter.Allow() {
				rlm.logger.Warn("Global rate limit exceeded",
					logger.String("client_ip", clientIP),
					logger.String("path", r.URL.Path),
				)

				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", rlm.config.GlobalRequestsPerMinute))
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Minute).Unix()))

				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getGlobalLimiter gets or creates a global rate limiter for a client
func (rlm *RateLimitManager) getGlobalLimiter(clientKey string) *RateLimiter {
	rlm.mutex.RLock()
	limiter, exists := rlm.globalLimiters[clientKey]
	rlm.mutex.RUnlock()

	if exists {
		return limiter
	}

	rlm.mutex.Lock()
	defer rlm.mutex.Unlock()

	// Check again in case it was created while waiting for lock
	if limiter, exists := rlm.globalLimiters[clientKey]; exists {
		return limiter
	}

	// Use global config or defaults
	requestsPerMin := rlm.config.GlobalRequestsPerMinute
	if requestsPerMin == 0 {
		requestsPerMin = rlm.config.GlobalLimits.RequestsPerSecond * 60
	}
	if requestsPerMin == 0 {
		requestsPerMin = 100 // default
	}

	burstSize := rlm.config.GlobalBurstSize
	if burstSize == 0 {
		burstSize = rlm.config.GlobalLimits.BurstSize
	}
	if burstSize == 0 {
		burstSize = requestsPerMin / 2 // default to half of requests per minute
	}

	// Create new limiter
	refillRate := time.Minute / time.Duration(requestsPerMin)
	limiter = NewRateLimiter(burstSize, refillRate)
	rlm.globalLimiters[clientKey] = limiter

	return limiter
}
