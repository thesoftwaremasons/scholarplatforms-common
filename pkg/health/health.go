package health

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Status represents the health status
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusUnhealthy Status = "unhealthy"
	StatusDegraded  Status = "degraded"
)

// CheckResult represents the result of a health check
type CheckResult struct {
	Name        string            `json:"name"`
	Status      Status            `json:"status"`
	Message     string            `json:"message,omitempty"`
	Error       string            `json:"error,omitempty"`
	Duration    time.Duration     `json:"duration"`
	Timestamp   time.Time         `json:"timestamp"`
	Details     map[string]interface{} `json:"details,omitempty"`
}

// HealthReport represents the overall health status
type HealthReport struct {
	Status       Status         `json:"status"`
	Timestamp    time.Time      `json:"timestamp"`
	Uptime       time.Duration  `json:"uptime"`
	Version      string         `json:"version,omitempty"`
	ServiceName  string         `json:"service_name,omitempty"`
	Checks       []CheckResult  `json:"checks"`
	Metrics      SystemMetrics  `json:"metrics"`
}

// SystemMetrics contains system performance metrics
type SystemMetrics struct {
	MemoryUsage      uint64        `json:"memory_usage_bytes"`
	MemoryAllocated  uint64        `json:"memory_allocated_bytes"`
	CPUUsage         float64       `json:"cpu_usage_percent"`
	GoroutineCount   int           `json:"goroutine_count"`
	GCCount          uint32        `json:"gc_count"`
	RequestsTotal    int64         `json:"requests_total"`
	RequestsPerSecond float64      `json:"requests_per_second"`
	AvgResponseTime  time.Duration `json:"avg_response_time"`
	DatabaseConnections int        `json:"database_connections"`
}

// CheckFunc represents a health check function
type CheckFunc func(ctx context.Context) CheckResult

// Checker represents a health check
type Checker struct {
	Name     string
	CheckFunc CheckFunc
	Timeout  time.Duration
	Critical bool // If true, failure of this check marks the service as unhealthy
}

// HealthService manages health checks
type HealthService struct {
	serviceName string
	version     string
	startTime   time.Time
	checkers    []Checker
	metrics     *MetricsCollector
	mutex       sync.RWMutex
}

// MetricsCollector collects system metrics
type MetricsCollector struct {
	requestsTotal      prometheus.Counter
	requestDuration    prometheus.Histogram
	activeConnections  prometheus.Gauge
	healthCheckResults *prometheus.GaugeVec
}

// NewHealthService creates a new health service
func NewHealthService(serviceName, version string) *HealthService {
	return &HealthService{
		serviceName: serviceName,
		version:     version,
		startTime:   time.Now(),
		checkers:    make([]Checker, 0),
		metrics:     newMetricsCollector(serviceName),
	}
}

func newMetricsCollector(serviceName string) *MetricsCollector {
	return &MetricsCollector{
		requestsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: "scholarplatforms",
			Subsystem: serviceName,
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests",
		}),
		requestDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace: "scholarplatforms",
			Subsystem: serviceName,
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request duration in seconds",
			Buckets:   prometheus.DefBuckets,
		}),
		activeConnections: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "scholarplatforms",
			Subsystem: serviceName,
			Name:      "active_connections",
			Help:      "Number of active connections",
		}),
		healthCheckResults: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "scholarplatforms",
				Subsystem: serviceName,
				Name:      "health_check_status",
				Help:      "Health check status (1 = healthy, 0 = unhealthy)",
			},
			[]string{"check_name"},
		),
	}
}

// AddChecker adds a health checker
func (hs *HealthService) AddChecker(checker Checker) {
	hs.mutex.Lock()
	defer hs.mutex.Unlock()
	hs.checkers = append(hs.checkers, checker)
}

// AddDatabaseCheck adds a database health check
func (hs *HealthService) AddDatabaseCheck(name string, db *sql.DB, timeout time.Duration, critical bool) {
	checker := Checker{
		Name:     name,
		Timeout:  timeout,
		Critical: critical,
		CheckFunc: func(ctx context.Context) CheckResult {
			return checkDatabase(ctx, db)
		},
	}
	hs.AddChecker(checker)
}

// AddHTTPCheck adds an HTTP endpoint health check
func (hs *HealthService) AddHTTPCheck(name, url string, timeout time.Duration, critical bool) {
	checker := Checker{
		Name:     name,
		Timeout:  timeout,
		Critical: critical,
		CheckFunc: func(ctx context.Context) CheckResult {
			return checkHTTP(ctx, url)
		},
	}
	hs.AddChecker(checker)
}

// AddTCPCheck adds a TCP connection health check
func (hs *HealthService) AddTCPCheck(name, address string, timeout time.Duration, critical bool) {
	checker := Checker{
		Name:     name,
		Timeout:  timeout,
		Critical: critical,
		CheckFunc: func(ctx context.Context) CheckResult {
			return checkTCP(ctx, address)
		},
	}
	hs.AddChecker(checker)
}

// Check performs all health checks and returns a health report
func (hs *HealthService) Check(ctx context.Context) HealthReport {
	hs.mutex.RLock()
	checkers := make([]Checker, len(hs.checkers))
	copy(checkers, hs.checkers)
	hs.mutex.RUnlock()

	var results []CheckResult
	overallStatus := StatusHealthy
	
	// Run all health checks
	for _, checker := range checkers {
		checkCtx := ctx
		if checker.Timeout > 0 {
			var cancel context.CancelFunc
			checkCtx, cancel = context.WithTimeout(ctx, checker.Timeout)
			defer cancel()
		}
		
		start := time.Now()
		result := checker.CheckFunc(checkCtx)
		result.Name = checker.Name
		result.Duration = time.Since(start)
		result.Timestamp = time.Now()
		
		results = append(results, result)
		
		// Update metrics
		statusValue := 0.0
		if result.Status == StatusHealthy {
			statusValue = 1.0
		}
		hs.metrics.healthCheckResults.WithLabelValues(checker.Name).Set(statusValue)
		
		// Determine overall status
		if checker.Critical && result.Status != StatusHealthy {
			overallStatus = StatusUnhealthy
		} else if result.Status == StatusDegraded && overallStatus == StatusHealthy {
			overallStatus = StatusDegraded
		}
	}
	
	return HealthReport{
		Status:      overallStatus,
		Timestamp:   time.Now(),
		Uptime:      time.Since(hs.startTime),
		Version:     hs.version,
		ServiceName: hs.serviceName,
		Checks:      results,
		Metrics:     hs.collectSystemMetrics(),
	}
}

// collectSystemMetrics collects current system metrics
func (hs *HealthService) collectSystemMetrics() SystemMetrics {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	
	return SystemMetrics{
		MemoryUsage:      memStats.Sys,
		MemoryAllocated:  memStats.Alloc,
		CPUUsage:         getCPUUsage(), // Simplified implementation
		GoroutineCount:   runtime.NumGoroutine(),
		GCCount:          memStats.NumGC,
		RequestsTotal:    int64(hs.getRequestCount()),
		RequestsPerSecond: hs.getRequestsPerSecond(),
		AvgResponseTime:  hs.getAvgResponseTime(),
		DatabaseConnections: hs.getDatabaseConnections(),
	}
}

// RecordRequest records an HTTP request for metrics
func (hs *HealthService) RecordRequest(duration time.Duration) {
	hs.metrics.requestsTotal.Inc()
	hs.metrics.requestDuration.Observe(duration.Seconds())
}

// SetActiveConnections sets the number of active connections
func (hs *HealthService) SetActiveConnections(count float64) {
	hs.metrics.activeConnections.Set(count)
}

// HTTPHandler returns an HTTP handler for health checks
func (hs *HealthService) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			hs.RecordRequest(time.Since(start))
		}()

		ctx := r.Context()
		report := hs.Check(ctx)
		
		w.Header().Set("Content-Type", "application/json")
		
		statusCode := http.StatusOK
		if report.Status == StatusUnhealthy {
			statusCode = http.StatusServiceUnavailable
		} else if report.Status == StatusDegraded {
			statusCode = http.StatusOK // Or 207 Multi-Status if preferred
		}
		
		w.WriteHeader(statusCode)
		
		// Simple JSON encoding (could use encoding/json for more complex cases)
		fmt.Fprintf(w, `{
			"status": "%s",
			"timestamp": "%s",
			"uptime": "%s",
			"version": "%s",
			"service_name": "%s",
			"checks": [`,
			report.Status,
			report.Timestamp.Format(time.RFC3339),
			report.Uptime.String(),
			report.Version,
			report.ServiceName,
		)
		
		for i, check := range report.Checks {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{
				"name": "%s",
				"status": "%s",
				"message": "%s",
				"error": "%s",
				"duration": "%s",
				"timestamp": "%s"
			}`,
				check.Name,
				check.Status,
				check.Message,
				check.Error,
				check.Duration.String(),
				check.Timestamp.Format(time.RFC3339),
			)
		}
		
		fmt.Fprintf(w, `],
			"metrics": {
				"memory_usage_bytes": %d,
				"memory_allocated_bytes": %d,
				"cpu_usage_percent": %.2f,
				"goroutine_count": %d,
				"gc_count": %d,
				"requests_total": %d,
				"requests_per_second": %.2f,
				"avg_response_time": "%s",
				"database_connections": %d
			}
		}`,
			report.Metrics.MemoryUsage,
			report.Metrics.MemoryAllocated,
			report.Metrics.CPUUsage,
			report.Metrics.GoroutineCount,
			report.Metrics.GCCount,
			report.Metrics.RequestsTotal,
			report.Metrics.RequestsPerSecond,
			report.Metrics.AvgResponseTime.String(),
			report.Metrics.DatabaseConnections,
		)
	}
}

// Built-in check functions

func checkDatabase(ctx context.Context, db *sql.DB) CheckResult {
	if db == nil {
		return CheckResult{
			Status: StatusUnhealthy,
			Error:  "Database connection is nil",
		}
	}
	
	err := db.PingContext(ctx)
	if err != nil {
		return CheckResult{
			Status: StatusUnhealthy,
			Error:  fmt.Sprintf("Database ping failed: %v", err),
		}
	}
	
	stats := db.Stats()
	details := map[string]interface{}{
		"open_connections": stats.OpenConnections,
		"idle_connections": stats.Idle,
		"in_use_connections": stats.InUse,
	}
	
	status := StatusHealthy
	if stats.OpenConnections > stats.MaxOpenConnections*80/100 {
		status = StatusDegraded
	}
	
	return CheckResult{
		Status:  status,
		Message: "Database connection is healthy",
		Details: details,
	}
}

func checkHTTP(ctx context.Context, url string) CheckResult {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return CheckResult{
			Status: StatusUnhealthy,
			Error:  fmt.Sprintf("Failed to create request: %v", err),
		}
	}
	
	resp, err := client.Do(req)
	if err != nil {
		return CheckResult{
			Status: StatusUnhealthy,
			Error:  fmt.Sprintf("HTTP request failed: %v", err),
		}
	}
	defer resp.Body.Close()
	
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return CheckResult{
			Status:  StatusHealthy,
			Message: fmt.Sprintf("HTTP endpoint returned %d", resp.StatusCode),
		}
	}
	
	return CheckResult{
		Status: StatusUnhealthy,
		Error:  fmt.Sprintf("HTTP endpoint returned %d", resp.StatusCode),
	}
}

func checkTCP(ctx context.Context, address string) CheckResult {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
	}
	
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return CheckResult{
			Status: StatusUnhealthy,
			Error:  fmt.Sprintf("TCP connection failed: %v", err),
		}
	}
	defer conn.Close()
	
	return CheckResult{
		Status:  StatusHealthy,
		Message: "TCP connection successful",
	}
}

// Helper methods (simplified implementations)

func (hs *HealthService) getRequestCount() float64 {
	// This would normally come from a metric collector
	return 0
}

func (hs *HealthService) getRequestsPerSecond() float64 {
	// This would normally be calculated from recent request metrics
	return 0
}

func (hs *HealthService) getAvgResponseTime() time.Duration {
	// This would normally be calculated from recent response times
	return 0
}

func (hs *HealthService) getDatabaseConnections() int {
	// This would normally come from database stats
	return 0
}

func getCPUUsage() float64 {
	// Simplified CPU usage calculation
	// In production, you'd want a more sophisticated implementation
	return 0
}