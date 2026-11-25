// Package pool provides Redis connection pooling utilities
package pool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// RedisPool manages Redis connection pooling with advanced features
type RedisPool struct {
	client        redis.UniversalClient
	config        RedisPoolConfig
	logger        logger.Logger
	stats         *RedisPoolStats
	healthChecker *RedisHealthChecker
	mu            sync.RWMutex
	closed        bool
}

// RedisPoolConfig holds configuration for Redis connection pooling
type RedisPoolConfig struct {
	// Connection settings
	Addresses  []string `env:"VALKEY_ADDRESSES" yaml:"addresses"`
	Password   string   `env:"VALKEY_PASSWORD" yaml:"password"`
	Database   int      `env:"VALKEY_DATABASE" yaml:"database"`
	Username   string   `env:"VALKEY_USERNAME" yaml:"username"`
	
	// Pool configuration
	PoolSize        int           `env:"VALKEY_POOL_SIZE" yaml:"pool_size"`
	MinIdleConns    int           `env:"VALKEY_MIN_IDLE_CONNS" yaml:"min_idle_conns"`
	MaxConnAge      time.Duration `env:"VALKEY_MAX_CONN_AGE" yaml:"max_conn_age"`
	PoolTimeout     time.Duration `env:"VALKEY_POOL_TIMEOUT" yaml:"pool_timeout"`
	IdleTimeout     time.Duration `env:"VALKEY_IDLE_TIMEOUT" yaml:"idle_timeout"`
	IdleCheckFreq   time.Duration `env:"VALKEY_IDLE_CHECK_FREQ" yaml:"idle_check_freq"`
	
	// Timeouts
	DialTimeout  time.Duration `env:"VALKEY_DIAL_TIMEOUT" yaml:"dial_timeout"`
	ReadTimeout  time.Duration `env:"VALKEY_READ_TIMEOUT" yaml:"read_timeout"`
	WriteTimeout time.Duration `env:"VALKEY_WRITE_TIMEOUT" yaml:"write_timeout"`
	
	// Cluster configuration (for Redis Cluster)
	ClusterEnabled      bool          `env:"VALKEY_CLUSTER_ENABLED" yaml:"cluster_enabled"`
	MaxRedirects        int           `env:"VALKEY_MAX_REDIRECTS" yaml:"max_redirects"`
	ReadOnly            bool          `env:"VALKEY_READ_ONLY" yaml:"read_only"`
	RouteByLatency      bool          `env:"VALKEY_ROUTE_BY_LATENCY" yaml:"route_by_latency"`
	RouteRandomly       bool          `env:"VALKEY_ROUTE_RANDOMLY" yaml:"route_randomly"`
	
	// Sentinel configuration (for Redis Sentinel)
	SentinelEnabled     bool     `env:"VALKEY_SENTINEL_ENABLED" yaml:"sentinel_enabled"`
	MasterName          string   `env:"VALKEY_MASTER_NAME" yaml:"master_name"`
	SentinelAddresses   []string `env:"VALKEY_SENTINEL_ADDRESSES" yaml:"sentinel_addresses"`
	SentinelPassword    string   `env:"VALKEY_SENTINEL_PASSWORD" yaml:"sentinel_password"`
	
	// Health check configuration
	HealthCheckInterval time.Duration `env:"VALKEY_HEALTH_CHECK_INTERVAL" yaml:"health_check_interval"`
	HealthCheckTimeout  time.Duration `env:"VALKEY_HEALTH_CHECK_TIMEOUT" yaml:"health_check_timeout"`
	
	// Retry configuration
	MaxRetries      int           `env:"VALKEY_MAX_RETRIES" yaml:"max_retries"`
	MinRetryBackoff time.Duration `env:"VALKEY_MIN_RETRY_BACKOFF" yaml:"min_retry_backoff"`
	MaxRetryBackoff time.Duration `env:"VALKEY_MAX_RETRY_BACKOFF" yaml:"max_retry_backoff"`
	
	// Monitoring
	EnableMetrics         bool          `env:"VALKEY_ENABLE_METRICS" yaml:"enable_metrics"`
	LogSlowCommands       bool          `env:"VALKEY_LOG_SLOW_COMMANDS" yaml:"log_slow_commands"`
	SlowCommandThreshold  time.Duration `env:"VALKEY_SLOW_COMMAND_THRESHOLD" yaml:"slow_command_threshold"`
}

// RedisPoolStats tracks Redis pool statistics
type RedisPoolStats struct {
	mu sync.RWMutex
	
	// Connection statistics
	TotalConns   uint32        `json:"total_conns"`
	IdleConns    uint32        `json:"idle_conns"`
	StaleConns   uint32        `json:"stale_conns"`
	Hits         uint32        `json:"hits"`
	Misses       uint32        `json:"misses"`
	Timeouts     uint32        `json:"timeouts"`
	
	// Command statistics
	CommandsExecuted     int64         `json:"commands_executed"`
	CommandsSuccessful   int64         `json:"commands_successful"`
	CommandsError        int64         `json:"commands_error"`
	SlowCommands         int64         `json:"slow_commands"`
	AverageCommandTime   time.Duration `json:"average_command_time"`
	
	// Health check statistics
	HealthCheckSuccesses int64       `json:"health_check_successes"`
	HealthCheckFailures  int64       `json:"health_check_failures"`
	LastHealthCheck     time.Time    `json:"last_health_check"`
	HealthStatus        string       `json:"health_status"`
	
	// Memory statistics (from Redis INFO)
	UsedMemory     int64 `json:"used_memory"`
	UsedMemoryPeak int64 `json:"used_memory_peak"`
	KeyspaceHits   int64 `json:"keyspace_hits"`
	KeyspaceMisses int64 `json:"keyspace_misses"`
}

// RedisHealthChecker manages Redis health monitoring
type RedisHealthChecker struct {
	pool     *RedisPool
	stopCh   chan struct{}
	doneCh   chan struct{}
	interval time.Duration
	timeout  time.Duration
}

// NewRedisPool creates a new Redis connection pool
func NewRedisPool(config RedisPoolConfig, log logger.Logger) (*RedisPool, error) {
	// Set defaults
	config.SetDefaults()
	
	var client redis.UniversalClient
	var err error
	
	// Create appropriate client based on configuration
	if config.SentinelEnabled {
		client, err = createSentinelClient(config)
	} else if config.ClusterEnabled {
		client, err = createClusterClient(config)
	} else {
		client, err = createStandaloneClient(config)
	}
	
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis client: %w", err)
	}
	
	// Initialize pool stats
	stats := &RedisPoolStats{
		HealthStatus: "initializing",
	}
	
	pool := &RedisPool{
		client: client,
		config: config,
		logger: log,
		stats:  stats,
	}
	
	// Initialize health checker
	pool.healthChecker = &RedisHealthChecker{
		pool:     pool,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		interval: config.HealthCheckInterval,
		timeout:  config.HealthCheckTimeout,
	}
	
	// Test initial connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to ping Redis: %w", err)
	}
	
	// Start health checker
	go pool.healthChecker.start()
	
	// Start stats collector
	go pool.startStatsCollector()
	
	log.Info("Redis pool initialized",
		logger.Any("addresses", config.Addresses),
		logger.Int("database", config.Database),
		logger.Int("pool_size", config.PoolSize),
		logger.Bool("cluster_enabled", config.ClusterEnabled),
		logger.Bool("sentinel_enabled", config.SentinelEnabled))
	
	return pool, nil
}

// createStandaloneClient creates a standalone Redis client
func createStandaloneClient(config RedisPoolConfig) (redis.UniversalClient, error) {
	if len(config.Addresses) == 0 {
		return nil, fmt.Errorf("no Redis addresses configured")
	}
	
	options := &redis.Options{
		Addr:            config.Addresses[0],
		Password:        config.Password,
		DB:              config.Database,
		Username:        config.Username,
		PoolSize:        config.PoolSize,
		MinIdleConns:    config.MinIdleConns,
		MaxConnAge:      config.MaxConnAge,
		PoolTimeout:     config.PoolTimeout,
		IdleTimeout:     config.IdleTimeout,
		DialTimeout:     config.DialTimeout,
		ReadTimeout:     config.ReadTimeout,
		WriteTimeout:    config.WriteTimeout,
		MaxRetries:      config.MaxRetries,
		MinRetryBackoff: config.MinRetryBackoff,
		MaxRetryBackoff: config.MaxRetryBackoff,
	}
	
	return redis.NewClient(options), nil
}

// createClusterClient creates a Redis cluster client
func createClusterClient(config RedisPoolConfig) (redis.UniversalClient, error) {
	if len(config.Addresses) == 0 {
		return nil, fmt.Errorf("no Redis cluster addresses configured")
	}
	
	options := &redis.ClusterOptions{
		Addrs:           config.Addresses,
		Password:        config.Password,
		Username:        config.Username,
		PoolSize:        config.PoolSize,
		MinIdleConns:    config.MinIdleConns,
		MaxConnAge:      config.MaxConnAge,
		PoolTimeout:     config.PoolTimeout,
		IdleTimeout:     config.IdleTimeout,
		DialTimeout:     config.DialTimeout,
		ReadTimeout:     config.ReadTimeout,
		WriteTimeout:    config.WriteTimeout,
		MaxRetries:      config.MaxRetries,
		MinRetryBackoff: config.MinRetryBackoff,
		MaxRetryBackoff: config.MaxRetryBackoff,
		MaxRedirects:    config.MaxRedirects,
		ReadOnly:        config.ReadOnly,
		RouteByLatency:  config.RouteByLatency,
		RouteRandomly:   config.RouteRandomly,
	}
	
	return redis.NewClusterClient(options), nil
}

// createSentinelClient creates a Redis Sentinel client
func createSentinelClient(config RedisPoolConfig) (redis.UniversalClient, error) {
	if len(config.SentinelAddresses) == 0 {
		return nil, fmt.Errorf("no Redis Sentinel addresses configured")
	}
	
	if config.MasterName == "" {
		return nil, fmt.Errorf("Redis Sentinel master name not configured")
	}
	
	options := &redis.FailoverOptions{
		MasterName:       config.MasterName,
		SentinelAddrs:    config.SentinelAddresses,
		SentinelPassword: config.SentinelPassword,
		Password:         config.Password,
		DB:               config.Database,
		Username:         config.Username,
		PoolSize:         config.PoolSize,
		MinIdleConns:     config.MinIdleConns,
		MaxConnAge:       config.MaxConnAge,
		PoolTimeout:      config.PoolTimeout,
		IdleTimeout:      config.IdleTimeout,
		DialTimeout:      config.DialTimeout,
		ReadTimeout:      config.ReadTimeout,
		WriteTimeout:     config.WriteTimeout,
		MaxRetries:       config.MaxRetries,
		MinRetryBackoff:  config.MinRetryBackoff,
		MaxRetryBackoff:  config.MaxRetryBackoff,
	}
	
	return redis.NewFailoverClient(options), nil
}

// GetClient returns the Redis client
func (p *RedisPool) GetClient() redis.UniversalClient {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.client
}

// Do executes a Redis command with monitoring
func (p *RedisPool) Do(ctx context.Context, args ...interface{}) *redis.Cmd {
	start := time.Now()
	
	cmd := p.client.Do(ctx, args...)
	
	duration := time.Since(start)
	
	// Update statistics
	p.updateCommandStats(duration, cmd.Err())
	
	// Log slow commands
	if p.config.LogSlowCommands && duration > p.config.SlowCommandThreshold {
		p.logger.Warn("Slow Redis command detected",
			logger.Any("args", args),
			logger.Duration("duration", duration))
		
		p.stats.mu.Lock()
		p.stats.SlowCommands++
		p.stats.mu.Unlock()
	}
	
	return cmd
}

// Get executes a GET command with monitoring
func (p *RedisPool) Get(ctx context.Context, key string) *redis.StringCmd {
	start := time.Now()
	
	cmd := p.client.Get(ctx, key)
	
	duration := time.Since(start)
	p.updateCommandStats(duration, cmd.Err())
	
	if p.config.LogSlowCommands && duration > p.config.SlowCommandThreshold {
		p.logger.Warn("Slow Redis GET",
			logger.String("key", key),
			logger.Duration("duration", duration))
		
		p.stats.mu.Lock()
		p.stats.SlowCommands++
		p.stats.mu.Unlock()
	}
	
	return cmd
}

// Set executes a SET command with monitoring
func (p *RedisPool) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	start := time.Now()
	
	cmd := p.client.Set(ctx, key, value, expiration)
	
	duration := time.Since(start)
	p.updateCommandStats(duration, cmd.Err())
	
	if p.config.LogSlowCommands && duration > p.config.SlowCommandThreshold {
		p.logger.Warn("Slow Redis SET",
			logger.String("key", key),
			logger.Duration("duration", duration))
		
		p.stats.mu.Lock()
		p.stats.SlowCommands++
		p.stats.mu.Unlock()
	}
	
	return cmd
}

// Del executes a DEL command with monitoring
func (p *RedisPool) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	start := time.Now()
	
	cmd := p.client.Del(ctx, keys...)
	
	duration := time.Since(start)
	p.updateCommandStats(duration, cmd.Err())
	
	if p.config.LogSlowCommands && duration > p.config.SlowCommandThreshold {
		p.logger.Warn("Slow Redis DEL",
			logger.Any("keys", keys),
			logger.Duration("duration", duration))
		
		p.stats.mu.Lock()
		p.stats.SlowCommands++
		p.stats.mu.Unlock()
	}
	
	return cmd
}

// Pipeline creates a Redis pipeline for batch operations
func (p *RedisPool) Pipeline() redis.Pipeliner {
	return p.client.Pipeline()
}

// TxPipeline creates a Redis transaction pipeline
func (p *RedisPool) TxPipeline() redis.Pipeliner {
	return p.client.TxPipeline()
}

// updateCommandStats updates command execution statistics
func (p *RedisPool) updateCommandStats(duration time.Duration, err error) {
	p.stats.mu.Lock()
	defer p.stats.mu.Unlock()
	
	p.stats.CommandsExecuted++
	
	if err != nil {
		p.stats.CommandsError++
	} else {
		p.stats.CommandsSuccessful++
	}
	
	// Update average command time
	if p.stats.CommandsExecuted == 1 {
		p.stats.AverageCommandTime = duration
	} else {
		p.stats.AverageCommandTime = time.Duration(
			(int64(p.stats.AverageCommandTime) + int64(duration)) / 2,
		)
	}
}

// startStatsCollector starts background statistics collection
func (p *RedisPool) startStatsCollector() {
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
func (p *RedisPool) updatePoolStats() {
	// Get pool statistics
	poolStats := p.client.PoolStats()
	
	p.stats.mu.Lock()
	p.stats.TotalConns = poolStats.TotalConns
	p.stats.IdleConns = poolStats.IdleConns
	p.stats.StaleConns = poolStats.StaleConns
	p.stats.Hits = poolStats.Hits
	p.stats.Misses = poolStats.Misses
	p.stats.Timeouts = poolStats.Timeouts
	p.stats.mu.Unlock()
	
	// Get Redis server info
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	info := p.client.Info(ctx, "memory", "stats")
	if info.Err() == nil {
		p.parseRedisInfo(info.Val())
	}
	
	// Log pool statistics periodically
	if p.config.EnableMetrics {
		p.logger.Debug("Redis pool stats",
			logger.Any("total_conns", poolStats.TotalConns),
			logger.Any("idle_conns", poolStats.IdleConns),
			logger.Any("stale_conns", poolStats.StaleConns),
			logger.Any("hits", poolStats.Hits),
			logger.Any("misses", poolStats.Misses),
			logger.Any("timeouts", poolStats.Timeouts))
	}
}

// parseRedisInfo parses Redis INFO command output
func (p *RedisPool) parseRedisInfo(info string) {
	// Simple parsing of Redis INFO output
	// In a real implementation, you would parse this more thoroughly
	p.stats.mu.Lock()
	defer p.stats.mu.Unlock()
	
	// This is a simplified parsing - in reality you'd parse the full INFO output
	// used_memory:1234567
	// keyspace_hits:5678
	// keyspace_misses:90
	
	// For now, we'll set placeholder values
	// In a real implementation, you would parse the actual INFO response
	p.stats.UsedMemory = 0
	p.stats.UsedMemoryPeak = 0
	p.stats.KeyspaceHits = 0
	p.stats.KeyspaceMisses = 0
}

// GetStats returns current Redis pool statistics
func (p *RedisPool) GetStats() *RedisPoolStats {
	p.stats.mu.RLock()
	defer p.stats.mu.RUnlock()
	
	// Create a safe copy without the mutex
	return &RedisPoolStats{
		TotalConns:           p.stats.TotalConns,
		IdleConns:            p.stats.IdleConns,
		StaleConns:           p.stats.StaleConns,
		Hits:                 p.stats.Hits,
		Misses:               p.stats.Misses,
		Timeouts:             p.stats.Timeouts,
		CommandsExecuted:     p.stats.CommandsExecuted,
		CommandsSuccessful:   p.stats.CommandsSuccessful,
		CommandsError:        p.stats.CommandsError,
		SlowCommands:         p.stats.SlowCommands,
		AverageCommandTime:   p.stats.AverageCommandTime,
		HealthCheckSuccesses: p.stats.HealthCheckSuccesses,
		HealthCheckFailures:  p.stats.HealthCheckFailures,
		LastHealthCheck:     p.stats.LastHealthCheck,
		HealthStatus:        p.stats.HealthStatus,
		UsedMemory:          p.stats.UsedMemory,
		UsedMemoryPeak:      p.stats.UsedMemoryPeak,
		KeyspaceHits:        p.stats.KeyspaceHits,
		KeyspaceMisses:      p.stats.KeyspaceMisses,
	}
}

// Health returns the health status of the Redis pool
func (p *RedisPool) Health() map[string]interface{} {
	stats := p.GetStats()
	
	return map[string]interface{}{
		"status":                   stats.HealthStatus,
		"last_health_check":        stats.LastHealthCheck,
		"total_connections":        stats.TotalConns,
		"idle_connections":         stats.IdleConns,
		"stale_connections":        stats.StaleConns,
		"connection_hits":          stats.Hits,
		"connection_misses":        stats.Misses,
		"connection_timeouts":      stats.Timeouts,
		"commands_executed":        stats.CommandsExecuted,
		"commands_successful":      stats.CommandsSuccessful,
		"commands_error":           stats.CommandsError,
		"slow_commands":            stats.SlowCommands,
		"average_command_time":     stats.AverageCommandTime.String(),
		"health_check_successes":   stats.HealthCheckSuccesses,
		"health_check_failures":    stats.HealthCheckFailures,
		"used_memory":              stats.UsedMemory,
		"keyspace_hits":            stats.KeyspaceHits,
		"keyspace_misses":          stats.KeyspaceMisses,
	}
}

// Close closes the Redis pool and stops background processes
func (p *RedisPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if p.closed {
		return nil
	}
	
	p.closed = true
	
	// Stop health checker
	close(p.healthChecker.stopCh)
	<-p.healthChecker.doneCh
	
	// Close Redis client
	err := p.client.Close()
	if err != nil {
		p.logger.Error("Error closing Redis pool", logger.ErrorField(err))
		return err
	}
	
	p.logger.Info("Redis pool closed")
	return nil
}

// Health checker methods
func (hc *RedisHealthChecker) start() {
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

func (hc *RedisHealthChecker) performHealthCheck() {
	ctx, cancel := context.WithTimeout(context.Background(), hc.timeout)
	defer cancel()
	
	err := hc.pool.client.Ping(ctx).Err()
	
	hc.pool.stats.mu.Lock()
	hc.pool.stats.LastHealthCheck = time.Now()
	
	if err == nil {
		hc.pool.stats.HealthCheckSuccesses++
		hc.pool.stats.HealthStatus = "healthy"
	} else {
		hc.pool.stats.HealthCheckFailures++
		hc.pool.stats.HealthStatus = "unhealthy"
		hc.pool.logger.Error("Redis health check failed", logger.ErrorField(err))
	}
	
	hc.pool.stats.mu.Unlock()
}

// SetDefaults sets default values for Redis pool configuration
func (config *RedisPoolConfig) SetDefaults() {
	if len(config.Addresses) == 0 {
		config.Addresses = []string{"localhost:6379"}
	}
	if config.PoolSize == 0 {
		config.PoolSize = 10
	}
	if config.MinIdleConns == 0 {
		config.MinIdleConns = 2
	}
	if config.MaxConnAge == 0 {
		config.MaxConnAge = 30 * time.Minute
	}
	if config.PoolTimeout == 0 {
		config.PoolTimeout = 4 * time.Second
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = 5 * time.Minute
	}
	if config.IdleCheckFreq == 0 {
		config.IdleCheckFreq = 1 * time.Minute
	}
	if config.DialTimeout == 0 {
		config.DialTimeout = 5 * time.Second
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 3 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 3 * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.MinRetryBackoff == 0 {
		config.MinRetryBackoff = 8 * time.Millisecond
	}
	if config.MaxRetryBackoff == 0 {
		config.MaxRetryBackoff = 512 * time.Millisecond
	}
	if config.MaxRedirects == 0 {
		config.MaxRedirects = 8
	}
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 30 * time.Second
	}
	if config.HealthCheckTimeout == 0 {
		config.HealthCheckTimeout = 5 * time.Second
	}
	if config.SlowCommandThreshold == 0 {
		config.SlowCommandThreshold = 100 * time.Millisecond
	}
}