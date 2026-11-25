// Package config provides standardized service configuration templates
package config

import (
	"os"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// BaseServiceConfig provides common configuration for all services
type BaseServiceConfig struct {
	// Service metadata
	ServiceName string `env:"SERVICE_NAME" yaml:"service_name"`
	Version     string `env:"VERSION" yaml:"version"`
	Environment string `env:"ENVIRONMENT" yaml:"environment"`
	
	// Server configuration
	Server EnhancedServerConfig `yaml:"server"`
	
	// Database configuration
	Database DatabaseConfig `yaml:"database"`
	
	// Redis configuration
	Valkey ValkeyConfig `yaml:"valkey"`
	
	// Logging configuration
	Logging LoggingConfig `yaml:"logging"`
	
	// Tracing configuration
	Tracing EnhancedTracingConfig `yaml:"tracing"`
	
	// Metrics configuration
	Metrics EnhancedMetricsConfig `yaml:"metrics"`
	
	// Security configuration
	Security SecurityConfig `yaml:"security"`
	
	// Service discovery configuration
	ServiceDiscovery ServiceDiscoveryConfig `yaml:"service_discovery"`
}

// EnhancedServerConfig extends the existing ServerConfig with environment variable support
type EnhancedServerConfig struct {
	ServerConfig
	Environment     string        `env:"ENVIRONMENT" yaml:"environment"`
	ShutdownTimeout time.Duration `env:"SERVER_SHUTDOWN_TIMEOUT" yaml:"shutdown_timeout"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level       string      `env:"LOG_LEVEL" yaml:"level"`
	Format      string      `env:"LOG_FORMAT" yaml:"format"`
	Output      string      `env:"LOG_OUTPUT" yaml:"output"`
	Development bool        `env:"LOG_DEVELOPMENT" yaml:"development"`
	LokiConfig  LokiConfig  `yaml:"loki"`
}

// LokiConfig holds Loki logging configuration
type LokiConfig struct {
	Enabled   bool              `env:"LOKI_ENABLED" yaml:"enabled"`
	URL       string            `env:"LOKI_URL" yaml:"url"`
	TenantID  string            `env:"LOKI_TENANT_ID" yaml:"tenant_id"`
	BatchSize int               `env:"LOKI_BATCH_SIZE" yaml:"batch_size"`
	BatchWait time.Duration     `env:"LOKI_BATCH_WAIT" yaml:"batch_wait"`
	Timeout   time.Duration     `env:"LOKI_TIMEOUT" yaml:"timeout"`
	Labels    map[string]string `yaml:"labels"`
}

// EnhancedTracingConfig extends the existing TracingConfig with environment variable support
type EnhancedTracingConfig struct {
	TracingConfig
	ServiceName  string  `env:"TRACING_SERVICE_NAME" yaml:"service_name"`
	JaegerURL    string  `env:"JAEGER_URL" yaml:"jaeger_url"`
	SamplingRate float64 `env:"TRACING_SAMPLING_RATE" yaml:"sampling_rate"`
}

// EnhancedMetricsConfig extends the existing MetricsConfig with environment variable support
type EnhancedMetricsConfig struct {
	MetricsConfig
	Namespace string `env:"METRICS_NAMESPACE" yaml:"namespace"`
}

// SecurityConfig holds security-related configuration
type SecurityConfig struct {
	MTLSEnabled        bool   `env:"SECURITY_MTLS_ENABLED" yaml:"mtls_enabled"`
	CACertFile         string `env:"SECURITY_CA_CERT_FILE" yaml:"ca_cert_file"`
	ServiceCertFile    string `env:"SECURITY_SERVICE_CERT_FILE" yaml:"service_cert_file"`
	ServiceKeyFile     string `env:"SECURITY_SERVICE_KEY_FILE" yaml:"service_key_file"`
	InsecureSkipVerify bool   `env:"SECURITY_INSECURE_SKIP_VERIFY" yaml:"insecure_skip_verify"`
	JWTSecret          string `env:"JWT_SECRET" yaml:"jwt_secret"` // Should come from secrets
	APIKeys            []string `env:"API_KEYS" yaml:"api_keys"`
}

// ServiceDiscoveryConfig holds service discovery configuration  
type ServiceDiscoveryConfig struct {
	Enabled     bool                       `env:"SERVICE_DISCOVERY_ENABLED" yaml:"enabled"`
	Services    map[string]ServiceEndpoint `yaml:"services"`
	HealthCheck HealthCheckConfig          `yaml:"health_check"`
}

// ServiceEndpoint represents a service endpoint
type ServiceEndpoint struct {
	LocalURL    string        `yaml:"local_url"`
	DockerURL   string        `yaml:"docker_url"`
	HealthPath  string        `yaml:"health_path"`
	Timeout     time.Duration `yaml:"timeout"`
}

// HealthCheckConfig holds health check configuration
type HealthCheckConfig struct {
	Enabled         bool          `env:"HEALTH_CHECK_ENABLED" yaml:"enabled"`
	Interval        time.Duration `env:"HEALTH_CHECK_INTERVAL" yaml:"interval"`
	Timeout         time.Duration `env:"HEALTH_CHECK_TIMEOUT" yaml:"timeout"`
	FailureThreshold int           `env:"HEALTH_CHECK_FAILURE_THRESHOLD" yaml:"failure_threshold"`
}

// LoadServiceConfig loads configuration for a service using both environment variables and YAML
func LoadServiceConfig[T any](serviceName string, configPtr *T, log logger.Logger) error {
	// Create environment config manager
	envManager := NewEnvConfigManager(serviceName, log)
	
	// Register common configuration options
	envManager.RegisterConfig([]ConfigOption{
		{Key: "SERVICE_NAME", DefaultValue: serviceName, Required: true, Description: "Service name"},
		{Key: "ENVIRONMENT", DefaultValue: "development", Required: false, Description: "Environment (development, staging, production)"},
		{Key: "SERVER_HOST", DefaultValue: "0.0.0.0", Required: false, Description: "Server bind host"},
		{Key: "SERVER_PORT", DefaultValue: 8080, Required: false, Validator: ValidatePort, Description: "Server port"},
		{Key: "LOG_LEVEL", DefaultValue: "info", Required: false, Description: "Log level"},
		{Key: "LOG_FORMAT", DefaultValue: "json", Required: false, Description: "Log format"},
		{Key: "DB_HOST", DefaultValue: "localhost", Required: false, Description: "Database host"},
		{Key: "DB_PORT", DefaultValue: 26257, Required: false, Validator: ValidatePort, Description: "Database port"},
		{Key: "VALKEY_HOST", DefaultValue: "localhost", Required: false, Description: "Redis host"},
		{Key: "VALKEY_PORT", DefaultValue: 6379, Required: false, Validator: ValidatePort, Description: "Redis port"},
		{Key: "METRICS_ENABLED", DefaultValue: true, Required: false, Description: "Enable metrics collection"},
		{Key: "TRACING_ENABLED", DefaultValue: false, Required: false, Description: "Enable distributed tracing"},
		
		// Sensitive configuration
		{Key: "DB_PASSWORD", Required: false, Sensitive: true, Description: "Database password"},
		{Key: "VALKEY_PASSWORD", Required: false, Sensitive: true, Description: "Redis password"},
		{Key: "JWT_SECRET", Required: false, Sensitive: true, Description: "JWT signing secret"},
	})
	
	// Load configuration into struct
	if err := envManager.LoadConfig(configPtr); err != nil {
		return err
	}
	
	// Validate configuration
	if err := envManager.ValidateConfig(); err != nil {
		return err
	}
	
	log.Info("Configuration loaded successfully",
		logger.String("service", serviceName))
	
	return nil
}

// SetupSecretsForService sets up secrets management for a service
func SetupSecretsForService(serviceName string, log logger.Logger) (*SecretsManager, error) {
	secretConfig := SecretConfig{
		SecretDir:     "./secrets",
		MasterKey:     GetEnvOrDefault("MASTER_KEY", "default-master-key-change-in-production"),
		KeyDerivation: true,
	}
	
	secretsManager, err := NewSecretsManager(secretConfig, log)
	if err != nil {
		return nil, err
	}
	
	// Generate default secrets for development if they don't exist
	if GetEnvOrDefault("ENVIRONMENT", "development") == "development" {
		if err := secretsManager.GenerateSecrets(); err != nil {
			log.Warn("Failed to generate development secrets", logger.ErrorField(err))
		}
	}
	
	return secretsManager, nil
}

// GetEnvOrDefault gets environment variable with fallback to default
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// ConfigDefaults provides sensible defaults for common configuration values
var ConfigDefaults = map[string]interface{}{
	"SERVER_READ_TIMEOUT":      "30s",
	"SERVER_WRITE_TIMEOUT":     "30s", 
	"SERVER_IDLE_TIMEOUT":      "120s",
	"SERVER_SHUTDOWN_TIMEOUT":  "30s",
	"DB_MAX_OPEN_CONNS":        25,
	"DB_MAX_IDLE_CONNS":        5,
	"DB_CONN_MAX_LIFETIME":     "5m",
	"DB_CONN_MAX_IDLE_TIME":    "5m",
	"VALKEY_POOL_SIZE":          10,
	"VALKEY_MIN_IDLE_CONNS":     2,
	"VALKEY_POOL_TIMEOUT":       "4s",
	"VALKEY_IDLE_TIMEOUT":       "5m",
	"METRICS_PORT":             9090,
	"METRICS_PATH":             "/metrics",
	"TRACING_SAMPLING_RATE":    0.1,
	"HEALTH_CHECK_INTERVAL":    "30s",
	"HEALTH_CHECK_TIMEOUT":     "5s",
	"HEALTH_CHECK_FAILURE_THRESHOLD": 3,
}