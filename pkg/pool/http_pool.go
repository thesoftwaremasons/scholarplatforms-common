// Package pool provides HTTP client connection pooling
package pool

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// HTTPPool manages HTTP client connection pooling with advanced features
type HTTPPool struct {
	clients      map[string]*http.Client
	config       HTTPPoolConfig
	logger       logger.Logger
	stats        *HTTPPoolStats
	mu           sync.RWMutex
	healthChecker *HTTPHealthChecker
	closed       bool
}

// HTTPPoolConfig holds configuration for HTTP connection pooling
type HTTPPoolConfig struct {
	// Connection limits
	MaxIdleConns        int           `env:"HTTP_MAX_IDLE_CONNS" yaml:"max_idle_conns"`
	MaxIdleConnsPerHost int           `env:"HTTP_MAX_IDLE_CONNS_PER_HOST" yaml:"max_idle_conns_per_host"`
	MaxConnsPerHost     int           `env:"HTTP_MAX_CONNS_PER_HOST" yaml:"max_conns_per_host"`
	
	// Timeouts
	DialTimeout           time.Duration `env:"HTTP_DIAL_TIMEOUT" yaml:"dial_timeout"`
	TLSHandshakeTimeout   time.Duration `env:"HTTP_TLS_HANDSHAKE_TIMEOUT" yaml:"tls_handshake_timeout"`
	ResponseHeaderTimeout time.Duration `env:"HTTP_RESPONSE_HEADER_TIMEOUT" yaml:"response_header_timeout"`
	IdleConnTimeout       time.Duration `env:"HTTP_IDLE_CONN_TIMEOUT" yaml:"idle_conn_timeout"`
	RequestTimeout        time.Duration `env:"HTTP_REQUEST_TIMEOUT" yaml:"request_timeout"`
	
	// Keep-alive settings
	KeepAlive        time.Duration `env:"HTTP_KEEP_ALIVE" yaml:"keep_alive"`
	DisableKeepAlive bool          `env:"HTTP_DISABLE_KEEP_ALIVE" yaml:"disable_keep_alive"`
	
	// TLS configuration
	InsecureSkipVerify bool   `env:"HTTP_INSECURE_SKIP_VERIFY" yaml:"insecure_skip_verify"`
	TLSCertFile        string `env:"HTTP_TLS_CERT_FILE" yaml:"tls_cert_file"`
	TLSKeyFile         string `env:"HTTP_TLS_KEY_FILE" yaml:"tls_key_file"`
	CACertFile         string `env:"HTTP_CA_CERT_FILE" yaml:"ca_cert_file"`
	
	// Retry configuration
	RetryAttempts    int           `env:"HTTP_RETRY_ATTEMPTS" yaml:"retry_attempts"`
	RetryDelay       time.Duration `env:"HTTP_RETRY_DELAY" yaml:"retry_delay"`
	RetryBackoffMax  time.Duration `env:"HTTP_RETRY_BACKOFF_MAX" yaml:"retry_backoff_max"`
	
	// Health check configuration
	HealthCheckInterval time.Duration `env:"HTTP_HEALTH_CHECK_INTERVAL" yaml:"health_check_interval"`
	HealthCheckTimeout  time.Duration `env:"HTTP_HEALTH_CHECK_TIMEOUT" yaml:"health_check_timeout"`
	HealthCheckPath     string        `env:"HTTP_HEALTH_CHECK_PATH" yaml:"health_check_path"`
	
	// Monitoring
	EnableMetrics         bool          `env:"HTTP_ENABLE_METRICS" yaml:"enable_metrics"`
	LogSlowRequests       bool          `env:"HTTP_LOG_SLOW_REQUESTS" yaml:"log_slow_requests"`
	SlowRequestThreshold  time.Duration `env:"HTTP_SLOW_REQUEST_THRESHOLD" yaml:"slow_request_threshold"`
}

// HTTPPoolStats tracks HTTP pool statistics
type HTTPPoolStats struct {
	mu sync.RWMutex
	
	// Request statistics
	RequestsTotal      int64         `json:"requests_total"`
	RequestsSuccessful int64         `json:"requests_successful"`
	RequestsTimeout    int64         `json:"requests_timeout"`
	RequestsError      int64         `json:"requests_error"`
	
	// Response statistics
	AverageResponseTime time.Duration `json:"average_response_time"`
	SlowRequests       int64         `json:"slow_requests"`
	
	// Connection statistics by host
	ConnectionStats    map[string]*HostConnectionStats `json:"connection_stats"`
	
	// Health check statistics
	HealthCheckSuccesses int64       `json:"health_check_successes"`
	HealthCheckFailures  int64       `json:"health_check_failures"`
	LastHealthCheck     time.Time    `json:"last_health_check"`
	HealthStatus        string       `json:"health_status"`
}

// HostConnectionStats tracks statistics per host
type HostConnectionStats struct {
	ActiveConnections int64         `json:"active_connections"`
	IdleConnections   int64         `json:"idle_connections"`
	RequestsTotal     int64         `json:"requests_total"`
	RequestsError     int64         `json:"requests_error"`
	AverageLatency    time.Duration `json:"average_latency"`
}

// HTTPHealthChecker manages HTTP client health monitoring
type HTTPHealthChecker struct {
	pool      *HTTPPool
	stopCh    chan struct{}
	doneCh    chan struct{}
	targets   []string
	interval  time.Duration
	timeout   time.Duration
	healthPath string
}

// NewHTTPPool creates a new HTTP connection pool
func NewHTTPPool(config HTTPPoolConfig, log logger.Logger) (*HTTPPool, error) {
	// Set defaults
	config.SetDefaults()
	
	pool := &HTTPPool{
		clients: make(map[string]*http.Client),
		config:  config,
		logger:  log,
		stats: &HTTPPoolStats{
			ConnectionStats: make(map[string]*HostConnectionStats),
			HealthStatus:    "initializing",
		},
	}
	
	// Initialize health checker
	pool.healthChecker = &HTTPHealthChecker{
		pool:       pool,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
		interval:   config.HealthCheckInterval,
		timeout:    config.HealthCheckTimeout,
		healthPath: config.HealthCheckPath,
		targets:    make([]string, 0),
	}
	
	// Start background processes
	go pool.healthChecker.start()
	go pool.startMetricsCollector()
	
	log.Info("HTTP pool initialized",
		logger.Int("max_idle_conns", config.MaxIdleConns),
		logger.Int("max_idle_conns_per_host", config.MaxIdleConnsPerHost),
		logger.Duration("request_timeout", config.RequestTimeout),
		logger.Duration("dial_timeout", config.DialTimeout))
	
	return pool, nil
}

// GetClient returns an HTTP client for the specified service
func (p *HTTPPool) GetClient(serviceName string) *http.Client {
	p.mu.RLock()
	client, exists := p.clients[serviceName]
	p.mu.RUnlock()
	
	if exists {
		return client
	}
	
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// Double-check after acquiring write lock
	if client, exists := p.clients[serviceName]; exists {
		return client
	}
	
	// Create new client
	client = p.createHTTPClient(serviceName)
	p.clients[serviceName] = client
	
	// Initialize stats for this service
	if p.stats.ConnectionStats[serviceName] == nil {
		p.stats.ConnectionStats[serviceName] = &HostConnectionStats{}
	}
	
	// Add to health check targets
	p.healthChecker.targets = append(p.healthChecker.targets, serviceName)
	
	p.logger.Debug("Created HTTP client",
		logger.String("service", serviceName))
	
	return client
}

// createHTTPClient creates a new HTTP client with optimized settings
func (p *HTTPPool) createHTTPClient(serviceName string) *http.Client {
	// Create custom transport
	transport := &http.Transport{
		// Connection pooling
		MaxIdleConns:        p.config.MaxIdleConns,
		MaxIdleConnsPerHost: p.config.MaxIdleConnsPerHost,
		MaxConnsPerHost:     p.config.MaxConnsPerHost,
		IdleConnTimeout:     p.config.IdleConnTimeout,
		
		// Timeouts
		ResponseHeaderTimeout: p.config.ResponseHeaderTimeout,
		TLSHandshakeTimeout:   p.config.TLSHandshakeTimeout,
		
		// Dialer configuration
		DialContext: (&net.Dialer{
			Timeout:   p.config.DialTimeout,
			KeepAlive: p.config.KeepAlive,
		}).DialContext,
		
		// Keep-alive settings
		DisableKeepAlives: p.config.DisableKeepAlive,
		
		// TLS configuration
		TLSClientConfig: p.createTLSConfig(),
		
		// Enable HTTP/2
		ForceAttemptHTTP2: true,
	}
	
	// Create client with transport and timeout
	client := &http.Client{
		Transport: transport,
		Timeout:   p.config.RequestTimeout,
	}
	
	return client
}

// createTLSConfig creates TLS configuration based on pool settings
func (p *HTTPPool) createTLSConfig() *tls.Config {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: p.config.InsecureSkipVerify,
	}
	
	// Load client certificates if configured
	if p.config.TLSCertFile != "" && p.config.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(p.config.TLSCertFile, p.config.TLSKeyFile)
		if err != nil {
			p.logger.Error("Failed to load TLS certificate",
				logger.String("cert_file", p.config.TLSCertFile),
				logger.String("key_file", p.config.TLSKeyFile),
				logger.ErrorField(err))
		} else {
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
	}
	
	return tlsConfig
}

// DoRequest performs an HTTP request with monitoring and retry logic
func (p *HTTPPool) DoRequest(ctx context.Context, serviceName string, req *http.Request) (*http.Response, error) {
	client := p.GetClient(serviceName)
	
	var resp *http.Response
	var err error
	
	start := time.Now()
	
	// Apply request timeout from context or config
	if p.config.RequestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.config.RequestTimeout)
		defer cancel()
	}
	
	req = req.WithContext(ctx)
	
	// Retry logic
	for attempt := 0; attempt <= p.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			// Calculate backoff delay
			delay := p.config.RetryDelay * time.Duration(attempt)
			if delay > p.config.RetryBackoffMax {
				delay = p.config.RetryBackoffMax
			}
			
			p.logger.Debug("Retrying HTTP request",
				logger.String("service", serviceName),
				logger.Int("attempt", attempt),
				logger.Duration("delay", delay))
			
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		
		resp, err = client.Do(req)
		if err == nil && resp.StatusCode < 500 {
			break // Success or client error (don't retry)
		}
		
		if resp != nil {
			resp.Body.Close()
		}
		
		if attempt == p.config.RetryAttempts {
			break // Last attempt
		}
	}
	
	duration := time.Since(start)
	
	// Update statistics
	p.updateRequestStats(serviceName, duration, err, resp)
	
	// Log slow requests
	if p.config.LogSlowRequests && duration > p.config.SlowRequestThreshold {
		p.logger.Warn("Slow HTTP request detected",
			logger.String("service", serviceName),
			logger.String("method", req.Method),
			logger.String("url", req.URL.String()),
			logger.Duration("duration", duration))
	}
	
	return resp, err
}

// updateRequestStats updates request statistics
func (p *HTTPPool) updateRequestStats(serviceName string, duration time.Duration, err error, resp *http.Response) {
	p.stats.mu.Lock()
	defer p.stats.mu.Unlock()
	
	// Update global stats
	p.stats.RequestsTotal++
	
	if err != nil {
		p.stats.RequestsError++
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			p.stats.RequestsTimeout++
		}
	} else if resp != nil && resp.StatusCode < 400 {
		p.stats.RequestsSuccessful++
	} else if resp != nil && resp.StatusCode >= 400 {
		p.stats.RequestsError++
	}
	
	// Update average response time
	if p.stats.RequestsTotal == 1 {
		p.stats.AverageResponseTime = duration
	} else {
		p.stats.AverageResponseTime = time.Duration(
			(int64(p.stats.AverageResponseTime) + int64(duration)) / 2,
		)
	}
	
	// Update slow request count
	if duration > p.config.SlowRequestThreshold {
		p.stats.SlowRequests++
	}
	
	// Update per-host stats
	hostStats := p.stats.ConnectionStats[serviceName]
	if hostStats == nil {
		hostStats = &HostConnectionStats{}
		p.stats.ConnectionStats[serviceName] = hostStats
	}
	
	hostStats.RequestsTotal++
	if err != nil {
		hostStats.RequestsError++
	}
	
	// Update average latency for host
	if hostStats.RequestsTotal == 1 {
		hostStats.AverageLatency = duration
	} else {
		hostStats.AverageLatency = time.Duration(
			(int64(hostStats.AverageLatency) + int64(duration)) / 2,
		)
	}
}

// startMetricsCollector starts background metrics collection
func (p *HTTPPool) startMetricsCollector() {
	if !p.config.EnableMetrics {
		return
	}
	
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			p.logMetrics()
		case <-p.healthChecker.stopCh:
			return
		}
	}
}

// logMetrics logs current pool metrics
func (p *HTTPPool) logMetrics() {
	stats := p.GetStats()
	
	p.logger.Debug("HTTP pool metrics",
		logger.Int64("requests_total", stats.RequestsTotal),
		logger.Int64("requests_successful", stats.RequestsSuccessful),
		logger.Int64("requests_error", stats.RequestsError),
		logger.Int64("requests_timeout", stats.RequestsTimeout),
		logger.Duration("avg_response_time", stats.AverageResponseTime),
		logger.Int64("slow_requests", stats.SlowRequests))
}

// GetStats returns current HTTP pool statistics
func (p *HTTPPool) GetStats() *HTTPPoolStats {
	p.stats.mu.RLock()
	defer p.stats.mu.RUnlock()
	
	// Create a safe copy without the mutex
	statsCopy := &HTTPPoolStats{
		RequestsTotal:       p.stats.RequestsTotal,
		RequestsSuccessful:  p.stats.RequestsSuccessful,
		RequestsError:       p.stats.RequestsError,
		RequestsTimeout:     p.stats.RequestsTimeout,
		AverageResponseTime: p.stats.AverageResponseTime,
		SlowRequests:        p.stats.SlowRequests,
		ConnectionStats:     make(map[string]*HostConnectionStats),
	}
	
	for host, hostStats := range p.stats.ConnectionStats {
		hostStatsCopy := *hostStats
		statsCopy.ConnectionStats[host] = &hostStatsCopy
	}
	
	return statsCopy
}

// Health returns the health status of the HTTP pool
func (p *HTTPPool) Health() map[string]interface{} {
	stats := p.GetStats()
	
	health := map[string]interface{}{
		"status":                  stats.HealthStatus,
		"last_health_check":       stats.LastHealthCheck,
		"requests_total":          stats.RequestsTotal,
		"requests_successful":     stats.RequestsSuccessful,
		"requests_error":          stats.RequestsError,
		"requests_timeout":        stats.RequestsTimeout,
		"average_response_time":   stats.AverageResponseTime.String(),
		"slow_requests":           stats.SlowRequests,
		"health_check_successes":  stats.HealthCheckSuccesses,
		"health_check_failures":   stats.HealthCheckFailures,
		"connection_stats":        stats.ConnectionStats,
	}
	
	return health
}

// AddHealthCheckTarget adds a service to health check monitoring
func (p *HTTPPool) AddHealthCheckTarget(serviceName, baseURL string) {
	p.healthChecker.targets = append(p.healthChecker.targets, serviceName)
	p.logger.Debug("Added health check target",
		logger.String("service", serviceName),
		logger.String("url", baseURL))
}

// Close closes the HTTP pool and stops background processes
func (p *HTTPPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if p.closed {
		return nil
	}
	
	p.closed = true
	
	// Stop health checker
	close(p.healthChecker.stopCh)
	<-p.healthChecker.doneCh
	
	// Close idle connections for all clients
	for serviceName, client := range p.clients {
		if transport, ok := client.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
		p.logger.Debug("Closed HTTP client", logger.String("service", serviceName))
	}
	
	p.logger.Info("HTTP pool closed")
	return nil
}

// Health checker methods
func (hc *HTTPHealthChecker) start() {
	defer close(hc.doneCh)
	
	if hc.interval == 0 {
		return // Health checking disabled
	}
	
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()
	
	// Perform initial health check
	hc.performHealthChecks()
	
	for {
		select {
		case <-ticker.C:
			hc.performHealthChecks()
		case <-hc.stopCh:
			return
		}
	}
}

func (hc *HTTPHealthChecker) performHealthChecks() {
	if len(hc.targets) == 0 {
		return
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), hc.timeout)
	defer cancel()
	
	var successCount, failureCount int64
	
	for _, serviceName := range hc.targets {
		client := hc.pool.GetClient(serviceName)
		
		// Construct health check URL
		// This would typically use service discovery to get the actual URL
		healthURL := fmt.Sprintf("http://%s%s", serviceName, hc.healthPath)
		
		req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
		if err != nil {
			failureCount++
			continue
		}
		
		resp, err := client.Do(req)
		if err != nil {
			failureCount++
			hc.pool.logger.Debug("Health check failed",
				logger.String("service", serviceName),
				logger.ErrorField(err))
			continue
		}
		
		resp.Body.Close()
		
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			successCount++
		} else {
			failureCount++
			hc.pool.logger.Debug("Health check returned non-2xx status",
				logger.String("service", serviceName),
				logger.Int("status_code", resp.StatusCode))
		}
	}
	
	// Update health statistics
	hc.pool.stats.mu.Lock()
	hc.pool.stats.LastHealthCheck = time.Now()
	hc.pool.stats.HealthCheckSuccesses += successCount
	hc.pool.stats.HealthCheckFailures += failureCount
	
	if failureCount == 0 {
		hc.pool.stats.HealthStatus = "healthy"
	} else if successCount > failureCount {
		hc.pool.stats.HealthStatus = "degraded"
	} else {
		hc.pool.stats.HealthStatus = "unhealthy"
	}
	hc.pool.stats.mu.Unlock()
}

// SetDefaults sets default values for HTTP pool configuration
func (config *HTTPPoolConfig) SetDefaults() {
	if config.MaxIdleConns == 0 {
		config.MaxIdleConns = 100
	}
	if config.MaxIdleConnsPerHost == 0 {
		config.MaxIdleConnsPerHost = 10
	}
	if config.MaxConnsPerHost == 0 {
		config.MaxConnsPerHost = 20
	}
	if config.DialTimeout == 0 {
		config.DialTimeout = 10 * time.Second
	}
	if config.TLSHandshakeTimeout == 0 {
		config.TLSHandshakeTimeout = 10 * time.Second
	}
	if config.ResponseHeaderTimeout == 0 {
		config.ResponseHeaderTimeout = 10 * time.Second
	}
	if config.IdleConnTimeout == 0 {
		config.IdleConnTimeout = 90 * time.Second
	}
	if config.RequestTimeout == 0 {
		config.RequestTimeout = 30 * time.Second
	}
	if config.KeepAlive == 0 {
		config.KeepAlive = 30 * time.Second
	}
	if config.RetryAttempts == 0 {
		config.RetryAttempts = 3
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 1 * time.Second
	}
	if config.RetryBackoffMax == 0 {
		config.RetryBackoffMax = 10 * time.Second
	}
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 30 * time.Second
	}
	if config.HealthCheckTimeout == 0 {
		config.HealthCheckTimeout = 5 * time.Second
	}
	if config.HealthCheckPath == "" {
		config.HealthCheckPath = "/health"
	}
	if config.SlowRequestThreshold == 0 {
		config.SlowRequestThreshold = 2 * time.Second
	}
}