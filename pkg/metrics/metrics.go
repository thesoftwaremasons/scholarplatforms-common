package metrics

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/config"
)

// MetricsManager handles Prometheus metrics
type MetricsManager struct {
	registry       *prometheus.Registry
	httpRequests   *prometheus.CounterVec
	httpDuration   *prometheus.HistogramVec
	activeConns    prometheus.Gauge
	dbConnections  *prometheus.GaugeVec
	natsConnected  prometheus.Gauge
	jobRuns        *prometheus.CounterVec
	jobDuration    *prometheus.HistogramVec
	jobQueueDepth  *prometheus.GaugeVec
	domainOps      *prometheus.CounterVec
	domainDuration *prometheus.HistogramVec
}

// NewMetricsManager creates a new metrics manager
func NewMetricsManager(serviceName string) *MetricsManager {
	registry := prometheus.NewRegistry()

	// HTTP metrics
	httpRequests := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	httpDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "status"},
	)

	// Connection metrics
	activeConns := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "active_connections_total",
			Help: "Total number of active connections",
		},
	)

	// Database metrics
	dbConnections := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "database_connections",
			Help: "Number of database connections",
		},
		[]string{"state"}, // open, idle, in_use
	)

	natsConnected := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "nats_connected",
			Help: "Whether the service is currently connected to NATS (1 connected, 0 disconnected)",
		},
	)

	jobRuns := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "background_job_runs_total",
			Help: "Total number of background job runs",
		},
		[]string{"job", "outcome"},
	)
	jobDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "background_job_duration_seconds",
			Help:    "Background job duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"job", "outcome"},
	)
	jobQueueDepth := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "background_job_queue_depth",
			Help: "Current number of queued background jobs",
		},
		[]string{"queue"},
	)

	domainOps := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "domain_operations_total",
			Help: "Total number of domain operations",
		},
		[]string{"operation", "outcome"},
	)
	domainDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "domain_operation_duration_seconds",
			Help:    "Domain operation duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation", "outcome"},
	)

	// Register metrics
	registry.MustRegister(httpRequests)
	registry.MustRegister(httpDuration)
	registry.MustRegister(activeConns)
	registry.MustRegister(dbConnections)
	registry.MustRegister(natsConnected)
	registry.MustRegister(jobRuns)
	registry.MustRegister(jobDuration)
	registry.MustRegister(jobQueueDepth)
	registry.MustRegister(domainOps)
	registry.MustRegister(domainDuration)

	return &MetricsManager{
		registry:       registry,
		httpRequests:   httpRequests,
		httpDuration:   httpDuration,
		activeConns:    activeConns,
		dbConnections:  dbConnections,
		natsConnected:  natsConnected,
		jobRuns:        jobRuns,
		jobDuration:    jobDuration,
		jobQueueDepth:  jobQueueDepth,
		domainOps:      domainOps,
		domainDuration: domainDuration,
	}
}

// RecordHTTPRequest records an HTTP request metric
func (m *MetricsManager) RecordHTTPRequest(method, path string, status int, duration time.Duration) {
	statusStr := http.StatusText(status)
	if statusStr == "" {
		statusStr = "unknown"
	}

	m.httpRequests.WithLabelValues(method, path, statusStr).Inc()
	m.httpDuration.WithLabelValues(method, path, statusStr).Observe(duration.Seconds())
}

// SetActiveConnections sets the number of active connections
func (m *MetricsManager) SetActiveConnections(count int) {
	m.activeConns.Set(float64(count))
}

// IncActiveConnections increments the active connections counter
func (m *MetricsManager) IncActiveConnections() {
	m.activeConns.Inc()
}

// DecActiveConnections decrements the active connections counter
func (m *MetricsManager) DecActiveConnections() {
	m.activeConns.Dec()
}

// SetDatabaseConnections sets database connection metrics
func (m *MetricsManager) SetDatabaseConnections(open, idle, inUse int) {
	m.dbConnections.WithLabelValues("open").Set(float64(open))
	m.dbConnections.WithLabelValues("idle").Set(float64(idle))
	m.dbConnections.WithLabelValues("in_use").Set(float64(inUse))
}

// SetNATSConnected records whether the service has an active NATS connection.
func (m *MetricsManager) SetNATSConnected(connected bool) {
	if connected {
		m.natsConnected.Set(1)
		return
	}
	m.natsConnected.Set(0)
}

// RecordBackgroundJob records a completed background job execution.
func (m *MetricsManager) RecordBackgroundJob(job, outcome string, duration time.Duration) {
	m.jobRuns.WithLabelValues(job, outcome).Inc()
	m.jobDuration.WithLabelValues(job, outcome).Observe(duration.Seconds())
}

// SetJobQueueDepth records the current depth of a named in-process queue.
func (m *MetricsManager) SetJobQueueDepth(queue string, depth int) {
	m.jobQueueDepth.WithLabelValues(queue).Set(float64(depth))
}

// RecordDomainOperation records an application/domain operation.
func (m *MetricsManager) RecordDomainOperation(operation, outcome string, duration time.Duration) {
	m.domainOps.WithLabelValues(operation, outcome).Inc()
	m.domainDuration.WithLabelValues(operation, outcome).Observe(duration.Seconds())
}

// Handler returns the HTTP handler for metrics endpoint
func (m *MetricsManager) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// HTTPMetricsMiddleware creates middleware to record HTTP metrics
func (m *MetricsManager) HTTPMetricsMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Increment active connections
			m.IncActiveConnections()
			defer m.DecActiveConnections()

			// Wrap response writer to capture status code
			wrapped := &metricsResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Call next handler
			next.ServeHTTP(wrapped, r)

			// Record metrics
			duration := time.Since(start)
			m.RecordHTTPRequest(r.Method, r.URL.Path, wrapped.statusCode, duration)
		})
	}
}

// metricsResponseWriter wraps http.ResponseWriter to capture status code
type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (m *metricsResponseWriter) WriteHeader(code int) {
	m.statusCode = code
	m.ResponseWriter.WriteHeader(code)
}

// StartMetricsServer starts a metrics server on the specified config
func StartMetricsServer(cfg config.MetricsConfig, metricsManager *MetricsManager) *http.Server {
	if !cfg.Enabled {
		return nil
	}

	mux := http.NewServeMux()
	mux.Handle(cfg.Path, metricsManager.Handler())

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Handle error - in a real implementation, you'd want to log this properly
		}
	}()

	return server
}
