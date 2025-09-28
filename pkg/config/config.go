package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ServerConfig represents common server configuration
type ServerConfig struct {
	Host           string        `yaml:"host" env:"SERVER_HOST" validate:"required"`
	Port           int           `yaml:"port" env:"SERVER_PORT" validate:"required,min=1,max=65535"`
	Environment    string        `yaml:"environment" env:"NODE_ENV" validate:"required,oneof=development staging production"`
	ReadTimeout    time.Duration `yaml:"read_timeout" env:"SERVER_READ_TIMEOUT"`
	WriteTimeout   time.Duration `yaml:"write_timeout" env:"SERVER_WRITE_TIMEOUT"`
	IdleTimeout    time.Duration `yaml:"idle_timeout" env:"SERVER_IDLE_TIMEOUT"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" env:"SERVER_SHUTDOWN_TIMEOUT"`
	TLSEnabled     bool          `yaml:"tls_enabled" env:"TLS_ENABLED"`
	TLSCertPath    string        `yaml:"tls_cert_path" env:"TLS_CERT_PATH"`
	TLSKeyPath     string        `yaml:"tls_key_path" env:"TLS_KEY_PATH"`
}

// DatabaseConfig represents common database configuration
type DatabaseConfig struct {
	Driver       string `yaml:"driver" env:"DB_DRIVER" validate:"required,oneof=postgres cockroachdb mysql"`
	Host         string `yaml:"host" env:"DB_HOST" validate:"required"`
	Port         int    `yaml:"port" env:"DB_PORT" validate:"required,min=1,max=65535"`
	Database     string `yaml:"database" env:"DB_NAME" validate:"required"`
	User         string `yaml:"user" env:"DB_USER" validate:"required"`
	Password     string `yaml:"password" env:"DB_PASSWORD" validate:"required"`
	SSLMode      string `yaml:"ssl_mode" env:"DB_SSL_MODE" validate:"oneof=disable require prefer allow"`
	MaxOpenConns int    `yaml:"max_open_conns" env:"DB_MAX_OPEN_CONNS" validate:"min=1,max=1000"`
	MaxIdleConns int    `yaml:"max_idle_conns" env:"DB_MAX_IDLE_CONNS" validate:"min=1,max=100"`
	MaxLifetime  string `yaml:"max_lifetime" env:"DB_MAX_LIFETIME"`
}

// RedisConfig represents Redis configuration
type RedisConfig struct {
	Address  string `yaml:"address" env:"REDIS_ADDR" validate:"required"`
	Password string `yaml:"password" env:"REDIS_PASSWORD"`
	DB       int    `yaml:"db" env:"REDIS_DB" validate:"min=0,max=15"`
	PoolSize int    `yaml:"pool_size" env:"REDIS_POOL_SIZE" validate:"min=1,max=100"`
	Timeout  string `yaml:"timeout" env:"REDIS_TIMEOUT"`
}

// LoggerConfig represents logging configuration
type LoggerConfig struct {
	Level      string `yaml:"level"`
	Format     string `yaml:"format"`
	Output     string `yaml:"output"`
	TimeFormat string `yaml:"time_format"`
}

// MetricsConfig represents metrics configuration
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Path    string `yaml:"path"`
}

// TracingConfig represents tracing configuration
type TracingConfig struct {
	Enabled      bool    `yaml:"enabled"`
	ServiceName  string  `yaml:"service_name"`
	TempoEndpoint string  `yaml:"tempo_endpoint"`
	SampleRate   float64 `yaml:"sample_rate"`
}

// ZitadelConfig represents Zitadel configuration
type ZitadelConfig struct {
	Enabled           bool   `yaml:"enabled"`
	Endpoint          string `yaml:"endpoint"`
	ProjectID         string `yaml:"project_id"`
	ClientID          string `yaml:"client_id"`
	ClientPrivateKey  string `yaml:"client_private_key"`
	RedirectURL       string `yaml:"redirect_url"`
	Issuer            string `yaml:"issuer"`
	JWKSURL           string `yaml:"jwks_url"`
	Scopes            []string `yaml:"scopes"`
}

// AuthConfig represents authentication configuration
type AuthConfig struct {
	JWTSecret             string        `yaml:"jwt_secret"`
	TokenExpiry           time.Duration `yaml:"token_expiry"`
	RefreshTokenExpiry    time.Duration `yaml:"refresh_token_expiry"`
	RequireAuthentication bool          `yaml:"require_authentication"`
}

// RateLimitConfig represents rate limiting configuration
type RateLimitConfig struct {
	Enabled           bool `yaml:"enabled"`
	RequestsPerMinute int  `yaml:"requests_per_minute"`
	Burst             int  `yaml:"burst"`
}

// CORSConfig represents CORS configuration
type CORSConfig struct {
	AllowedOrigins   []string `yaml:"allowed_origins"`
	AllowedMethods   []string `yaml:"allowed_methods"`
	AllowedHeaders   []string `yaml:"allowed_headers"`
	ExposedHeaders   []string `yaml:"exposed_headers"`
	AllowCredentials bool     `yaml:"allow_credentials"`
	MaxAge           int      `yaml:"max_age"`
}

// NATSConfig represents NATS configuration
type NATSConfig struct {
	URL               string        `yaml:"url"`
	Subject           string        `yaml:"subject"`
	MaxReconnects     int           `yaml:"max_reconnects"`
	ReconnectWait     time.Duration `yaml:"reconnect_wait"`
	ConnectionTimeout time.Duration `yaml:"connection_timeout"`
}

// LoadFromFile loads configuration from a YAML file
func LoadFromFile(path string, config interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return yaml.Unmarshal(data, config)
}

// SaveToFile saves configuration to a YAML file
func SaveToFile(path string, config interface{}) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// SetDefaults sets default values for server configuration
func (s *ServerConfig) SetDefaults() {
	if s.Host == "" {
		s.Host = "0.0.0.0"
	}
	if s.Port == 0 {
		s.Port = 8080
	}
	if s.Environment == "" {
		s.Environment = "development"
	}
	if s.ReadTimeout == 0 {
		s.ReadTimeout = 30 * time.Second
	}
	if s.WriteTimeout == 0 {
		s.WriteTimeout = 30 * time.Second
	}
	if s.IdleTimeout == 0 {
		s.IdleTimeout = 120 * time.Second
	}
	if s.ShutdownTimeout == 0 {
		s.ShutdownTimeout = 15 * time.Second
	}
}

// SetDefaults sets default values for database configuration
func (d *DatabaseConfig) SetDefaults() {
	if d.Driver == "" {
		d.Driver = "postgres"
	}
	if d.Host == "" {
		d.Host = "localhost"
	}
	if d.Port == 0 {
		d.Port = 5432
	}
	if d.SSLMode == "" {
		d.SSLMode = "disable"
	}
	if d.MaxOpenConns == 0 {
		d.MaxOpenConns = 25
	}
	if d.MaxIdleConns == 0 {
		d.MaxIdleConns = 5
	}
	if d.MaxLifetime == "" {
		d.MaxLifetime = "5m"
	}
}

// SetDefaults sets default values for Redis configuration
func (r *RedisConfig) SetDefaults() {
	if r.Address == "" {
		r.Address = "localhost:6379"
	}
	if r.PoolSize == 0 {
		r.PoolSize = 10
	}
	if r.Timeout == "" {
		r.Timeout = "5s"
	}
}

// SetDefaults sets default values for logger configuration
func (l *LoggerConfig) SetDefaults() {
	if l.Level == "" {
		l.Level = "info"
	}
	if l.Format == "" {
		l.Format = "json"
	}
	if l.Output == "" {
		l.Output = "stdout"
	}
	if l.TimeFormat == "" {
		l.TimeFormat = time.RFC3339
	}
}

// SetDefaults sets default values for auth configuration
func (a *AuthConfig) SetDefaults() {
	if a.TokenExpiry == 0 {
		a.TokenExpiry = 24 * time.Hour
	}
	if a.RefreshTokenExpiry == 0 {
		a.RefreshTokenExpiry = 7 * 24 * time.Hour
	}
}