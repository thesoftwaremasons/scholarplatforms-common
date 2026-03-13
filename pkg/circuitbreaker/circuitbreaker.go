package circuitbreaker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sony/gobreaker"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/errors"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// CircuitBreaker wraps gobreaker with enhanced functionality
type CircuitBreaker struct {
	name    string
	breaker *gobreaker.CircuitBreaker
	logger  logger.Logger
	mu      sync.RWMutex
}

// Config holds circuit breaker configuration
type Config struct {
	Name          string
	MaxRequests   uint32        // Max requests allowed in half-open state
	Interval      time.Duration // Interval to clear internal counts
	Timeout       time.Duration // Time to wait in open state before half-open
	ReadyToTrip   func(counts gobreaker.Counts) bool
	OnStateChange func(name string, from gobreaker.State, to gobreaker.State)
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig(name string) Config {
	return Config{
		Name:        name,
		MaxRequests: 3,
		Interval:    10 * time.Second,
		Timeout:     60 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 3 && failureRatio >= 0.6
		},
	}
}

// New creates a new circuit breaker
func New(cfg Config, log logger.Logger) *CircuitBreaker {
	if cfg.ReadyToTrip == nil {
		cfg.ReadyToTrip = DefaultConfig(cfg.Name).ReadyToTrip
	}

	settings := gobreaker.Settings{
		Name:        cfg.Name,
		MaxRequests: cfg.MaxRequests,
		Interval:    cfg.Interval,
		Timeout:     cfg.Timeout,
		ReadyToTrip: cfg.ReadyToTrip,
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			log.Warn("Circuit breaker state changed",
				logger.String("circuit_breaker", name),
				logger.String("from_state", from.String()),
				logger.String("to_state", to.String()),
			)

			if cfg.OnStateChange != nil {
				cfg.OnStateChange(name, from, to)
			}
		},
	}

	return &CircuitBreaker{
		name:    cfg.Name,
		breaker: gobreaker.NewCircuitBreaker(settings),
		logger:  log,
	}
}

// Execute runs the given function within the circuit breaker
func (cb *CircuitBreaker) Execute(fn func() (interface{}, error)) (interface{}, error) {
	result, err := cb.breaker.Execute(fn)

	if err != nil {
		// Check if circuit is open
		if err == gobreaker.ErrOpenState {
			cb.logger.Warn("Circuit breaker is open",
				logger.String("circuit_breaker", cb.name),
			)
			return nil, errors.NewServiceUnavailableError(cb.name).
				WithDetail("circuit_state", "open").
				WithCause(err)
		}

		// Check if too many requests in half-open state
		if err == gobreaker.ErrTooManyRequests {
			cb.logger.Warn("Circuit breaker: too many requests",
				logger.String("circuit_breaker", cb.name),
			)
			return nil, errors.NewRateLimitExceededError(int(cb.breaker.Counts().Requests), "circuit_breaker").
				WithDetail("circuit_state", "half_open").
				WithCause(err)
		}
	}

	return result, err
}

// ExecuteContext runs the given function with context support
func (cb *CircuitBreaker) ExecuteContext(ctx context.Context, fn func(context.Context) (interface{}, error)) (interface{}, error) {
	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Wrap the function to handle context cancellation
	wrappedFn := func() (interface{}, error) {
		// Create a channel to receive the result
		type result struct {
			data interface{}
			err  error
		}
		resultCh := make(chan result, 1)

		// Execute function in goroutine
		go func() {
			data, err := fn(ctx)
			resultCh <- result{data: data, err: err}
		}()

		// Wait for either result or context cancellation
		select {
		case res := <-resultCh:
			return res.data, res.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return cb.Execute(wrappedFn)
}

// State returns the current state of the circuit breaker
func (cb *CircuitBreaker) State() gobreaker.State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.breaker.State()
}

// Counts returns the current counts
func (cb *CircuitBreaker) Counts() gobreaker.Counts {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.breaker.Counts()
}

// Name returns the circuit breaker name
func (cb *CircuitBreaker) Name() string {
	return cb.name
}

// CircuitBreakerManager manages multiple circuit breakers
type CircuitBreakerManager struct {
	breakers map[string]*CircuitBreaker
	logger   logger.Logger
	mu       sync.RWMutex
}

// NewManager creates a new circuit breaker manager
func NewManager(log logger.Logger) *CircuitBreakerManager {
	return &CircuitBreakerManager{
		breakers: make(map[string]*CircuitBreaker),
		logger:   log,
	}
}

// GetOrCreate gets an existing circuit breaker or creates a new one
func (m *CircuitBreakerManager) GetOrCreate(name string, cfg *Config) *CircuitBreaker {
	m.mu.RLock()
	cb, exists := m.breakers[name]
	m.mu.RUnlock()

	if exists {
		return cb
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check in case another goroutine created it
	if cb, exists := m.breakers[name]; exists {
		return cb
	}

	// Create config if not provided
	var config Config
	if cfg != nil {
		config = *cfg
	} else {
		config = DefaultConfig(name)
	}
	config.Name = name

	cb = New(config, m.logger)
	m.breakers[name] = cb

	m.logger.Info("Circuit breaker created",
		logger.String("name", name),
	)

	return cb
}

// Get retrieves a circuit breaker by name
func (m *CircuitBreakerManager) Get(name string) (*CircuitBreaker, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cb, exists := m.breakers[name]
	if !exists {
		return nil, fmt.Errorf("circuit breaker '%s' not found", name)
	}

	return cb, nil
}

// GetAll returns all circuit breakers
func (m *CircuitBreakerManager) GetAll() map[string]*CircuitBreaker {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*CircuitBreaker, len(m.breakers))
	for k, v := range m.breakers {
		result[k] = v
	}

	return result
}

// HealthStatus returns the health status of all circuit breakers
func (m *CircuitBreakerManager) HealthStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]interface{})
	for name, cb := range m.breakers {
		counts := cb.Counts()
		status[name] = map[string]interface{}{
			"state":           cb.State().String(),
			"requests":        counts.Requests,
			"total_successes": counts.TotalSuccesses,
			"total_failures":  counts.TotalFailures,
			"consecutive_successes": counts.ConsecutiveSuccesses,
			"consecutive_failures":  counts.ConsecutiveFailures,
		}
	}

	return status
}
