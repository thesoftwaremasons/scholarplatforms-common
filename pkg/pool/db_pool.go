// Package pool provides connection pooling utilities for all services
package pool

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// DatabasePool manages database connection pooling with advanced features
type DatabasePool struct {
	db           *sql.DB
	logger       logger.Logger
	config       DatabasePoolConfig
	stats        *PoolStats
	healthChecker *HealthChecker
	mu           sync.RWMutex
	closed       bool
}

// DatabasePoolConfig holds configuration for database connection pooling
type DatabasePoolConfig struct {
	// Connection settings
	Driver           string        `env:"DB_DRIVER" yaml:"driver"`
	Host             string        `env:"DB_HOST" yaml:"host"`
	Port             int           `env:"DB_PORT" yaml:"port"`
	Database         string        `env:"DB_DATABASE" yaml:"database"`
	Username         string        `env:"DB_USERNAME" yaml:"username"`
	Password         string        `env:"DB_PASSWORD" yaml:"password"`
	SSLMode          string        `env:"DB_SSL_MODE" yaml:"ssl_mode"`
	
	// Pool configuration
	MaxOpenConns     int           `env:"DB_MAX_OPEN_CONNS" yaml:"max_open_conns"`
	MaxIdleConns     int           `env:"DB_MAX_IDLE_CONNS" yaml:"max_idle_conns"`
	ConnMaxLifetime  time.Duration `env:"DB_CONN_MAX_LIFETIME" yaml:"conn_max_lifetime"`
	ConnMaxIdleTime  time.Duration `env:"DB_CONN_MAX_IDLE_TIME" yaml:"conn_max_idle_time"`
	
	// Health check configuration
	HealthCheckInterval time.Duration `env:"DB_HEALTH_CHECK_INTERVAL" yaml:"health_check_interval"`
	HealthCheckTimeout  time.Duration `env:"DB_HEALTH_CHECK_TIMEOUT" yaml:"health_check_timeout"`
	HealthCheckRetries  int           `env:"DB_HEALTH_CHECK_RETRIES" yaml:"health_check_retries"`
	
	// Performance tuning
	PreparedStatementCache int `env:"DB_PREPARED_STMT_CACHE" yaml:"prepared_statement_cache"`
	QueryTimeout          time.Duration `env:"DB_QUERY_TIMEOUT" yaml:"query_timeout"`
	TxTimeout             time.Duration `env:"DB_TX_TIMEOUT" yaml:"tx_timeout"`
	
	// Monitoring
	EnableMetrics     bool `env:"DB_ENABLE_METRICS" yaml:"enable_metrics"`
	LogSlowQueries    bool `env:"DB_LOG_SLOW_QUERIES" yaml:"log_slow_queries"`
	SlowQueryThreshold time.Duration `env:"DB_SLOW_QUERY_THRESHOLD" yaml:"slow_query_threshold"`
}

// PoolStats tracks database pool statistics
type PoolStats struct {
	mu sync.RWMutex
	
	// Connection statistics
	OpenConnections     int           `json:"open_connections"`
	InUseConnections    int           `json:"in_use_connections"`
	IdleConnections     int           `json:"idle_connections"`
	WaitCount          int64         `json:"wait_count"`
	WaitDuration       time.Duration `json:"wait_duration"`
	MaxIdleClosed      int64         `json:"max_idle_closed"`
	MaxLifetimeClosed  int64         `json:"max_lifetime_closed"`
	
	// Query statistics
	QueriesExecuted    int64         `json:"queries_executed"`
	SlowQueries        int64         `json:"slow_queries"`
	QueryErrors        int64         `json:"query_errors"`
	AverageQueryTime   time.Duration `json:"average_query_time"`
	
	// Health check statistics
	HealthCheckSuccesses int64       `json:"health_check_successes"`
	HealthCheckFailures  int64       `json:"health_check_failures"`
	LastHealthCheck     time.Time    `json:"last_health_check"`
	HealthStatus        string       `json:"health_status"`
}

// HealthChecker manages database health monitoring
type HealthChecker struct {
	pool     *DatabasePool
	stopCh   chan struct{}
	doneCh   chan struct{}
	interval time.Duration
	timeout  time.Duration
	retries  int
}

// NewDatabasePool creates a new database connection pool
func NewDatabasePool(config DatabasePoolConfig, log logger.Logger) (*DatabasePool, error) {
	// Build connection string based on driver
	dsn, err := buildDSN(config)
	if err != nil {
		return nil, fmt.Errorf("failed to build DSN: %w", err)
	}
	
	// Open database connection
	db, err := sql.Open(config.Driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	
	// Configure connection pool
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)
	db.SetConnMaxIdleTime(config.ConnMaxIdleTime)
	
	// Initialize pool stats
	stats := &PoolStats{
		HealthStatus: "initializing",
	}
	
	pool := &DatabasePool{
		db:     db,
		logger: log,
		config: config,
		stats:  stats,
	}
	
	// Initialize health checker
	pool.healthChecker = &HealthChecker{
		pool:     pool,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		interval: config.HealthCheckInterval,
		timeout:  config.HealthCheckTimeout,
		retries:  config.HealthCheckRetries,
	}
	
	// Test initial connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	
	// Start health checker
	go pool.healthChecker.start()
	
	// Start stats collector
	go pool.startStatsCollector()
	
	log.Info("Database pool initialized",
		logger.String("driver", config.Driver),
		logger.String("host", config.Host),
		logger.Int("port", config.Port),
		logger.String("database", config.Database),
		logger.Int("max_open_conns", config.MaxOpenConns),
		logger.Int("max_idle_conns", config.MaxIdleConns))
	
	return pool, nil
}

// buildDSN builds a database connection string
func buildDSN(config DatabasePoolConfig) (string, error) {
	switch config.Driver {
	case "postgres":
		return fmt.Sprintf("postgresql://%s:%s@%s:%d/%s?sslmode=%s",
			config.Username, config.Password, config.Host, config.Port,
			config.Database, config.SSLMode), nil
	case "mysql":
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&loc=Local",
			config.Username, config.Password, config.Host, config.Port,
			config.Database), nil
	default:
		return "", fmt.Errorf("unsupported database driver: %s", config.Driver)
	}
}

// GetDB returns the underlying database connection
func (p *DatabasePool) GetDB() *sql.DB {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.db
}

// QueryContext executes a query with context and monitoring
func (p *DatabasePool) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	start := time.Now()
	
	// Apply query timeout if configured
	if p.config.QueryTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.config.QueryTimeout)
		defer cancel()
	}
	
	rows, err := p.db.QueryContext(ctx, query, args...)
	duration := time.Since(start)
	
	// Update statistics
	p.updateQueryStats(duration, err)
	
	// Log slow queries
	if p.config.LogSlowQueries && duration > p.config.SlowQueryThreshold {
		p.logger.Warn("Slow query detected",
			logger.String("query", query),
			logger.Duration("duration", duration),
			logger.Any("args", args))
		
		p.stats.mu.Lock()
		p.stats.SlowQueries++
		p.stats.mu.Unlock()
	}
	
	return rows, err
}

// ExecContext executes a statement with context and monitoring
func (p *DatabasePool) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	start := time.Now()
	
	if p.config.QueryTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.config.QueryTimeout)
		defer cancel()
	}
	
	result, err := p.db.ExecContext(ctx, query, args...)
	duration := time.Since(start)
	
	p.updateQueryStats(duration, err)
	
	if p.config.LogSlowQueries && duration > p.config.SlowQueryThreshold {
		p.logger.Warn("Slow exec detected",
			logger.String("query", query),
			logger.Duration("duration", duration),
			logger.Any("args", args))
		
		p.stats.mu.Lock()
		p.stats.SlowQueries++
		p.stats.mu.Unlock()
	}
	
	return result, err
}

// BeginTxContext begins a transaction with context and timeout
func (p *DatabasePool) BeginTxContext(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	if p.config.TxTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.config.TxTimeout)
		defer cancel()
	}
	
	return p.db.BeginTx(ctx, opts)
}

// updateQueryStats updates query execution statistics
func (p *DatabasePool) updateQueryStats(duration time.Duration, err error) {
	p.stats.mu.Lock()
	defer p.stats.mu.Unlock()
	
	p.stats.QueriesExecuted++
	
	if err != nil {
		p.stats.QueryErrors++
	}
	
	// Update average query time (simple moving average)
	if p.stats.QueriesExecuted == 1 {
		p.stats.AverageQueryTime = duration
	} else {
		p.stats.AverageQueryTime = time.Duration(
			(int64(p.stats.AverageQueryTime) + int64(duration)) / 2,
		)
	}
}

// startStatsCollector starts a goroutine to collect pool statistics
func (p *DatabasePool) startStatsCollector() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			p.updatePoolStats()
		case <-p.healthChecker.stopCh:
			return
		}
	}
}

// updatePoolStats updates connection pool statistics
func (p *DatabasePool) updatePoolStats() {
	dbStats := p.db.Stats()
	
	p.stats.mu.Lock()
	p.stats.OpenConnections = dbStats.OpenConnections
	p.stats.InUseConnections = dbStats.InUse
	p.stats.IdleConnections = dbStats.Idle
	p.stats.WaitCount = dbStats.WaitCount
	p.stats.WaitDuration = dbStats.WaitDuration
	p.stats.MaxIdleClosed = dbStats.MaxIdleClosed
	p.stats.MaxLifetimeClosed = dbStats.MaxLifetimeClosed
	p.stats.mu.Unlock()
	
	// Log pool statistics periodically
	if p.config.EnableMetrics {
		p.logger.Debug("Database pool stats",
			logger.Int("open_conns", dbStats.OpenConnections),
			logger.Int("in_use_conns", dbStats.InUse),
			logger.Int("idle_conns", dbStats.Idle),
			logger.Int64("wait_count", dbStats.WaitCount),
			logger.Duration("wait_duration", dbStats.WaitDuration))
	}
}

// GetStats returns current pool statistics
func (p *DatabasePool) GetStats() *PoolStats {
	p.stats.mu.RLock()
	defer p.stats.mu.RUnlock()
	
	// Create a safe copy without the mutex
	return &PoolStats{
		OpenConnections:       p.stats.OpenConnections,
		InUseConnections:      p.stats.InUseConnections,
		IdleConnections:       p.stats.IdleConnections,
		WaitCount:            p.stats.WaitCount,
		WaitDuration:         p.stats.WaitDuration,
		MaxIdleClosed:        p.stats.MaxIdleClosed,
		MaxLifetimeClosed:    p.stats.MaxLifetimeClosed,
		QueriesExecuted:      p.stats.QueriesExecuted,
		SlowQueries:          p.stats.SlowQueries,
		QueryErrors:          p.stats.QueryErrors,
		AverageQueryTime:     p.stats.AverageQueryTime,
		HealthCheckSuccesses: p.stats.HealthCheckSuccesses,
		HealthCheckFailures:  p.stats.HealthCheckFailures,
		LastHealthCheck:      p.stats.LastHealthCheck,
		HealthStatus:         p.stats.HealthStatus,
	}
}

// Health returns the health status of the database pool
func (p *DatabasePool) Health() map[string]interface{} {
	stats := p.GetStats()
	
	return map[string]interface{}{
		"status":               stats.HealthStatus,
		"last_health_check":    stats.LastHealthCheck,
		"open_connections":     stats.OpenConnections,
		"in_use_connections":   stats.InUseConnections,
		"idle_connections":     stats.IdleConnections,
		"queries_executed":     stats.QueriesExecuted,
		"query_errors":         stats.QueryErrors,
		"slow_queries":         stats.SlowQueries,
		"average_query_time":   stats.AverageQueryTime.String(),
		"health_check_successes": stats.HealthCheckSuccesses,
		"health_check_failures":  stats.HealthCheckFailures,
	}
}

// Close closes the database pool and stops background processes
func (p *DatabasePool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if p.closed {
		return nil
	}
	
	p.closed = true
	
	// Stop health checker
	close(p.healthChecker.stopCh)
	<-p.healthChecker.doneCh
	
	// Close database connection
	err := p.db.Close()
	if err != nil {
		p.logger.Error("Error closing database pool", logger.ErrorField(err))
		return err
	}
	
	p.logger.Info("Database pool closed")
	return nil
}

// Health checker methods
func (hc *HealthChecker) start() {
	defer close(hc.doneCh)
	
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()
	
	// Perform initial health check
	hc.performHealthCheck()
	
	for {
		select {
		case <-ticker.C:
			hc.performHealthCheck()
		case <-hc.stopCh:
			return
		}
	}
}

func (hc *HealthChecker) performHealthCheck() {
	ctx, cancel := context.WithTimeout(context.Background(), hc.timeout)
	defer cancel()
	
	var err error
	for attempt := 0; attempt < hc.retries; attempt++ {
		err = hc.pool.db.PingContext(ctx)
		if err == nil {
			break
		}
		
		if attempt < hc.retries-1 {
			time.Sleep(1 * time.Second)
		}
	}
	
	hc.pool.stats.mu.Lock()
	hc.pool.stats.LastHealthCheck = time.Now()
	
	if err == nil {
		hc.pool.stats.HealthCheckSuccesses++
		hc.pool.stats.HealthStatus = "healthy"
	} else {
		hc.pool.stats.HealthCheckFailures++
		hc.pool.stats.HealthStatus = "unhealthy"
		hc.pool.logger.Error("Database health check failed", logger.ErrorField(err))
	}
	
	hc.pool.stats.mu.Unlock()
}

// SetDefaults sets default values for database pool configuration
func (config *DatabasePoolConfig) SetDefaults() {
	if config.Driver == "" {
		config.Driver = "postgres"
	}
	if config.Host == "" {
		config.Host = "localhost"
	}
	if config.Port == 0 {
		config.Port = 5432
		if config.Driver == "postgres" {
			config.Port = 5432
		} else if config.Driver == "mysql" {
			config.Port = 3306
		}
	}
	if config.SSLMode == "" {
		config.SSLMode = "disable"
	}
	if config.MaxOpenConns == 0 {
		config.MaxOpenConns = 25
	}
	if config.MaxIdleConns == 0 {
		config.MaxIdleConns = 5
	}
	if config.ConnMaxLifetime == 0 {
		config.ConnMaxLifetime = 5 * time.Minute
	}
	if config.ConnMaxIdleTime == 0 {
		config.ConnMaxIdleTime = 5 * time.Minute
	}
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 30 * time.Second
	}
	if config.HealthCheckTimeout == 0 {
		config.HealthCheckTimeout = 5 * time.Second
	}
	if config.HealthCheckRetries == 0 {
		config.HealthCheckRetries = 3
	}
	if config.QueryTimeout == 0 {
		config.QueryTimeout = 30 * time.Second
	}
	if config.TxTimeout == 0 {
		config.TxTimeout = 1 * time.Minute
	}
	if config.SlowQueryThreshold == 0 {
		config.SlowQueryThreshold = 1 * time.Second
	}
}