// Package testing provides comprehensive integration testing infrastructure
package testing

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/config"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// TestLogger is a simple logger implementation for testing
type TestLogger struct{}

func (l *TestLogger) Debug(msg string, fields ...logger.Field) {
	fmt.Printf("[DEBUG] %s\n", msg)
}

func (l *TestLogger) Info(msg string, fields ...logger.Field) {
	fmt.Printf("[INFO] %s\n", msg)
}

func (l *TestLogger) Warn(msg string, fields ...logger.Field) {
	fmt.Printf("[WARN] %s\n", msg)
}

func (l *TestLogger) Error(msg string, fields ...logger.Field) {
	fmt.Printf("[ERROR] %s\n", msg)
}

func (l *TestLogger) Fatal(msg string, fields ...logger.Field) {
	fmt.Printf("[FATAL] %s\n", msg)
}

func (l *TestLogger) With(fields ...logger.Field) logger.Logger {
	return l
}

func (l *TestLogger) WithContext(ctx context.Context) logger.Logger {
	return l
}

// IntegrationTestSuite provides a comprehensive testing environment
type IntegrationTestSuite struct {
	t             *testing.T
	ctx           context.Context
	logger        logger.Logger
	containers    map[string]testcontainers.Container
	services      map[string]*TestService
	httpClient    *http.Client
	dbConnections map[string]*sql.DB
	cleanupFuncs  []func() error
	mu            sync.RWMutex
}

// TestService represents a service instance for testing
type TestService struct {
	Name        string
	Port        int
	HealthURL   string
	Container   testcontainers.Container
	BaseURL     string
	Ready       bool
	mu          sync.RWMutex
}

// TestConfig holds configuration for integration tests
type TestConfig struct {
	// Database configuration
	DatabaseImage    string        `yaml:"database_image"`
	DatabasePort     int           `yaml:"database_port"`
	DatabaseUser     string        `yaml:"database_user"`
	DatabasePassword string        `yaml:"database_password"`
	DatabaseName     string        `yaml:"database_name"`
	
	// Redis configuration
	RedisImage       string        `yaml:"redis_image"`
	RedisPort        int           `yaml:"redis_port"`
	RedisPassword    string        `yaml:"redis_password"`
	
	// Test timeouts
	StartupTimeout   time.Duration `yaml:"startup_timeout"`
	RequestTimeout   time.Duration `yaml:"request_timeout"`
	ShutdownTimeout  time.Duration `yaml:"shutdown_timeout"`
	
	// Service configuration
	Services         []ServiceTestConfig `yaml:"services"`
	
	// Network configuration
	NetworkName      string        `yaml:"network_name"`
	
	// Test data paths
	TestDataDir      string        `yaml:"test_data_dir"`
	FixturesDir      string        `yaml:"fixtures_dir"`
	SchemasDir       string        `yaml:"schemas_dir"`
}

// ServiceTestConfig holds configuration for testing individual services
type ServiceTestConfig struct {
	Name           string   `yaml:"name"`
	Image          string   `yaml:"image"`
	Port           int      `yaml:"port"`
	HealthEndpoint string   `yaml:"health_endpoint"`
	Environment    []string `yaml:"environment"`
	WaitStrategies []string `yaml:"wait_strategies"`
	Dependencies   []string `yaml:"dependencies"`
}

// NewIntegrationTestSuite creates a new integration test suite
func NewIntegrationTestSuite(t *testing.T, configPath string) (*IntegrationTestSuite, error) {
	ctx := context.Background()
	
	// Load test configuration
	var testConfig TestConfig
	if err := config.LoadFromFile(configPath, &testConfig); err != nil {
		return nil, fmt.Errorf("failed to load test config: %w", err)
	}
	
	// Create logger for tests
	testLogger := &TestLogger{}
	
	suite := &IntegrationTestSuite{
		t:             t,
		ctx:           ctx,
		logger:        testLogger,
		containers:    make(map[string]testcontainers.Container),
		services:      make(map[string]*TestService),
		dbConnections: make(map[string]*sql.DB),
		cleanupFuncs:  make([]func() error, 0),
		httpClient: &http.Client{
			Timeout: testConfig.RequestTimeout,
		},
	}
	
	suite.logger.Info("Integration test suite initialized",
		logger.String("config_path", configPath))
	
	return suite, nil
}

// SetupInfrastructure sets up database, Redis, and other infrastructure containers
func (suite *IntegrationTestSuite) SetupInfrastructure(config TestConfig) error {
	suite.logger.Info("Setting up infrastructure containers...")
	
	// Create test network
	networkReq := testcontainers.GenericNetworkRequest{
		NetworkRequest: testcontainers.NetworkRequest{
			Name: config.NetworkName,
		},
	}
	
	network, err := testcontainers.GenericNetwork(suite.ctx, networkReq)
	if err != nil {
		return fmt.Errorf("failed to create test network: %w", err)
	}
	suite.cleanupFuncs = append(suite.cleanupFuncs, func() error {
		return network.Remove(suite.ctx)
	})
	
	// Setup CockroachDB
	if err := suite.setupCockroachDB(config, config.NetworkName); err != nil {
		return fmt.Errorf("failed to setup CockroachDB: %w", err)
	}
	
	// Setup Redis
	if err := suite.setupRedis(config, config.NetworkName); err != nil {
		return fmt.Errorf("failed to setup Redis: %w", err)
	}
	
	// Setup NATS
	if err := suite.setupNATS(config, config.NetworkName); err != nil {
		return fmt.Errorf("failed to setup NATS: %w", err)
	}
	
	suite.logger.Info("Infrastructure setup completed")
	return nil
}

// setupCockroachDB starts a CockroachDB container for testing
func (suite *IntegrationTestSuite) setupCockroachDB(config TestConfig, networkName string) error {
	req := testcontainers.ContainerRequest{
		Image:        config.DatabaseImage,
		ExposedPorts: []string{fmt.Sprintf("%d/tcp", config.DatabasePort)},
		Env: map[string]string{
			"COCKROACH_DATABASE": config.DatabaseName,
			"COCKROACH_USER":     config.DatabaseUser,
			"COCKROACH_PASSWORD": config.DatabasePassword,
		},
		Cmd: []string{
			"start-single-node",
			"--insecure",
			"--store=type=mem,size=512MiB",
			"--advertise-addr=localhost",
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("CockroachDB node starting"),
			wait.ForListeningPort("26257/tcp"),
		).WithStartupTimeout(config.StartupTimeout),
		Networks: []string{networkName},
	}
	
	container, err := testcontainers.GenericContainer(suite.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return err
	}
	
	suite.containers["cockroachdb"] = container
	suite.cleanupFuncs = append(suite.cleanupFuncs, func() error {
		return container.Terminate(suite.ctx)
	})
	
	// Get connection details
	host, err := container.Host(suite.ctx)
	if err != nil {
		return err
	}
	
	port, err := container.MappedPort(suite.ctx, "26257")
	if err != nil {
		return err
	}
	
	// Create database connection
	dsn := fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=disable",
		config.DatabaseUser, config.DatabasePassword, host, port.Port(), config.DatabaseName)
	
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	
	suite.dbConnections["cockroachdb"] = db
	suite.cleanupFuncs = append(suite.cleanupFuncs, func() error {
		return db.Close()
	})
	
	suite.logger.Info("CockroachDB container started",
		logger.String("host", host),
		logger.String("port", port.Port()))
	
	return nil
}

// setupRedis starts a Redis container for testing
func (suite *IntegrationTestSuite) setupRedis(config TestConfig, networkName string) error {
	req := testcontainers.ContainerRequest{
		Image:        config.RedisImage,
		ExposedPorts: []string{"6379/tcp"},
		Env: map[string]string{
			"VALKEY_PASSWORD": config.RedisPassword,
		},
		Cmd: []string{
			"redis-server",
			"--requirepass", config.RedisPassword,
		},
		WaitingFor: wait.ForLog("Ready to accept connections").
			WithStartupTimeout(config.StartupTimeout),
		Networks: []string{networkName},
	}
	
	container, err := testcontainers.GenericContainer(suite.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return err
	}
	
	suite.containers["redis"] = container
	suite.cleanupFuncs = append(suite.cleanupFuncs, func() error {
		return container.Terminate(suite.ctx)
	})
	
	host, err := container.Host(suite.ctx)
	if err != nil {
		return err
	}
	
	port, err := container.MappedPort(suite.ctx, "6379")
	if err != nil {
		return err
	}
	
	suite.logger.Info("Redis container started",
		logger.String("host", host),
		logger.String("port", port.Port()))
	
	return nil
}

// setupNATS starts a NATS container for testing
func (suite *IntegrationTestSuite) setupNATS(config TestConfig, networkName string) error {
	req := testcontainers.ContainerRequest{
		Image:        "nats:latest",
		ExposedPorts: []string{"4222/tcp", "8222/tcp"},
		Cmd:          []string{"-js"},
		WaitingFor: wait.ForLog("Server is ready").
			WithStartupTimeout(config.StartupTimeout),
		Networks: []string{networkName},
	}
	
	container, err := testcontainers.GenericContainer(suite.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return err
	}
	
	suite.containers["nats"] = container
	suite.cleanupFuncs = append(suite.cleanupFuncs, func() error {
		return container.Terminate(suite.ctx)
	})
	
	suite.logger.Info("NATS container started")
	return nil
}

// StartService starts a service container for testing
func (suite *IntegrationTestSuite) StartService(serviceConfig ServiceTestConfig, networkName string) error {
	suite.mu.Lock()
	defer suite.mu.Unlock()
	
	suite.logger.Info("Starting service", logger.String("service", serviceConfig.Name))
	
	// Wait for dependencies
	for _, dep := range serviceConfig.Dependencies {
		if err := suite.waitForService(dep, 30*time.Second); err != nil {
			return fmt.Errorf("dependency %s not ready: %w", dep, err)
		}
	}
	
	req := testcontainers.ContainerRequest{
		Image:        serviceConfig.Image,
		ExposedPorts: []string{fmt.Sprintf("%d/tcp", serviceConfig.Port)},
		Env:          convertSliceToMap(serviceConfig.Environment),
		WaitingFor: wait.ForHTTP(serviceConfig.HealthEndpoint).
			WithPort(nat.Port(fmt.Sprintf("%d/tcp", serviceConfig.Port))).
			WithStartupTimeout(30 * time.Second),
		Networks: []string{networkName},
	}
	
	container, err := testcontainers.GenericContainer(suite.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return fmt.Errorf("failed to start service %s: %w", serviceConfig.Name, err)
	}
	
	host, err := container.Host(suite.ctx)
	if err != nil {
		return err
	}
	
	port, err := container.MappedPort(suite.ctx, nat.Port(fmt.Sprintf("%d/tcp", serviceConfig.Port)))
	if err != nil {
		return err
	}
	
	testService := &TestService{
		Name:      serviceConfig.Name,
		Port:      port.Int(),
		HealthURL: fmt.Sprintf("http://%s:%d%s", host, port.Int(), serviceConfig.HealthEndpoint),
		Container: container,
		BaseURL:   fmt.Sprintf("http://%s:%d", host, port.Int()),
		Ready:     true,
	}
	
	suite.services[serviceConfig.Name] = testService
	suite.containers[serviceConfig.Name] = container
	suite.cleanupFuncs = append(suite.cleanupFuncs, func() error {
		return container.Terminate(suite.ctx)
	})
	
	suite.logger.Info("Service started successfully",
		logger.String("service", serviceConfig.Name),
		logger.String("base_url", testService.BaseURL))
	
	return nil
}

// GetService returns a test service by name
func (suite *IntegrationTestSuite) GetService(name string) (*TestService, error) {
	suite.mu.RLock()
	defer suite.mu.RUnlock()
	
	service, exists := suite.services[name]
	if !exists {
		return nil, fmt.Errorf("service %s not found", name)
	}
	
	return service, nil
}

// GetDatabase returns a database connection by name
func (suite *IntegrationTestSuite) GetDatabase(name string) (*sql.DB, error) {
	suite.mu.RLock()
	defer suite.mu.RUnlock()
	
	db, exists := suite.dbConnections[name]
	if !exists {
		return nil, fmt.Errorf("database connection %s not found", name)
	}
	
	return db, nil
}

// WaitForServices waits for all services to be ready
func (suite *IntegrationTestSuite) WaitForServices(timeout time.Duration) error {
	suite.logger.Info("Waiting for all services to be ready...")
	
	ctx, cancel := context.WithTimeout(suite.ctx, timeout)
	defer cancel()
	
	for name := range suite.services {
		if err := suite.waitForServiceCtx(ctx, name); err != nil {
			return fmt.Errorf("service %s not ready: %w", name, err)
		}
	}
	
	suite.logger.Info("All services are ready")
	return nil
}

// waitForService waits for a specific service to be ready
func (suite *IntegrationTestSuite) waitForService(name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(suite.ctx, timeout)
	defer cancel()
	return suite.waitForServiceCtx(ctx, name)
}

// waitForServiceCtx waits for a service with context
func (suite *IntegrationTestSuite) waitForServiceCtx(ctx context.Context, name string) error {
	service, exists := suite.services[name]
	if !exists {
		return fmt.Errorf("service %s not found", name)
	}
	
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			resp, err := suite.httpClient.Get(service.HealthURL)
			if err == nil && resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				return nil
			}
			if resp != nil {
				resp.Body.Close()
			}
			time.Sleep(1 * time.Second)
		}
	}
}

// Cleanup shuts down all containers and cleans up resources
func (suite *IntegrationTestSuite) Cleanup() error {
	suite.logger.Info("Cleaning up test suite...")
	
	var errors []error
	
	// Run all cleanup functions in reverse order
	for i := len(suite.cleanupFuncs) - 1; i >= 0; i-- {
		if err := suite.cleanupFuncs[i](); err != nil {
			errors = append(errors, err)
			suite.logger.Error("Cleanup error", logger.ErrorField(err))
		}
	}
	
	if len(errors) > 0 {
		return fmt.Errorf("cleanup completed with %d errors", len(errors))
	}
	
	suite.logger.Info("Test suite cleanup completed")
	return nil
}

// HTTP helper methods for the test service
func (service *TestService) GET(endpoint string) (*http.Response, error) {
	return http.Get(service.BaseURL + endpoint)
}

func (service *TestService) POST(endpoint string, body interface{}) (*http.Response, error) {
	// Implementation would include JSON marshaling and POST request
	return nil, fmt.Errorf("POST method not implemented yet")
}

// Helper functions

// convertSliceToMap converts environment slice to map
func convertSliceToMap(env []string) map[string]string {
	result := make(map[string]string)
	for _, item := range env {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}