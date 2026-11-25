package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/secrets"
)

// InfisicalConfigLoader loads configuration from Infisical
type InfisicalConfigLoader struct {
	client *secrets.InfisicalClient
	logger logger.Logger
}

// NewInfisicalConfigLoader creates a new Infisical config loader
func NewInfisicalConfigLoader(log logger.Logger) (*InfisicalConfigLoader, error) {
	client, err := secrets.NewInfisicalClientFromEnv(log)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Infisical client: %w", err)
	}

	return &InfisicalConfigLoader{
		client: client,
		logger: log,
	}, nil
}

// LoadDatabaseConfig loads database configuration from Infisical
func (icl *InfisicalConfigLoader) LoadDatabaseConfig() (*DatabaseConfig, error) {
	cfg := &DatabaseConfig{}

	var err error
	cfg.Driver = icl.client.GetSecretWithDefault("DB_DRIVER", "postgres")
	cfg.Host = icl.client.GetSecretWithDefault("DB_HOST", "localhost")

	portStr := icl.client.GetSecretWithDefault("DB_PORT", "5432")
	cfg.Port, err = strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid DB_PORT: %w", err)
	}

	cfg.Database, err = icl.client.GetSecret("DB_NAME")
	if err != nil {
		return nil, fmt.Errorf("DB_NAME is required: %w", err)
	}

	cfg.User, err = icl.client.GetSecret("DB_USER")
	if err != nil {
		return nil, fmt.Errorf("DB_USER is required: %w", err)
	}

	cfg.Password, err = icl.client.GetSecret("DB_PASSWORD")
	if err != nil {
		return nil, fmt.Errorf("DB_PASSWORD is required: %w", err)
	}

	cfg.SSLMode = icl.client.GetSecretWithDefault("DB_SSL_MODE", "disable")

	maxOpenConnsStr := icl.client.GetSecretWithDefault("DB_MAX_OPEN_CONNS", "25")
	cfg.MaxOpenConns, _ = strconv.Atoi(maxOpenConnsStr)

	maxIdleConnsStr := icl.client.GetSecretWithDefault("DB_MAX_IDLE_CONNS", "5")
	cfg.MaxIdleConns, _ = strconv.Atoi(maxIdleConnsStr)

	cfg.MaxLifetime = icl.client.GetSecretWithDefault("DB_MAX_LIFETIME", "5m")

	cfg.SetDefaults()
	return cfg, nil
}

// LoadValkeyConfig loads Redis configuration from Infisical
func (icl *InfisicalConfigLoader) LoadValkeyConfig() (*ValkeyConfig, error) {
	cfg := &ValkeyConfig{}

	cfg.Address = icl.client.GetSecretWithDefault("VALKEY_ADDR", "localhost:6379")

	password, err := icl.client.GetSecret("VALKEY_PASSWORD")
	if err != nil {
		icl.logger.Warn("VALKEY_PASSWORD not found in Infisical, using empty password")
		cfg.Password = ""
	} else {
		cfg.Password = password
	}

	dbStr := icl.client.GetSecretWithDefault("VALKEY_DB", "0")
	cfg.DB, _ = strconv.Atoi(dbStr)

	poolSizeStr := icl.client.GetSecretWithDefault("VALKEY_POOL_SIZE", "10")
	cfg.PoolSize, _ = strconv.Atoi(poolSizeStr)

	cfg.Timeout = icl.client.GetSecretWithDefault("VALKEY_TIMEOUT", "5s")

	cfg.SetDefaults()
	return cfg, nil
}

// LoadZitadelConfig loads Zitadel configuration from Infisical
func (icl *InfisicalConfigLoader) LoadZitadelConfig() (*ZitadelConfig, error) {
	cfg := &ZitadelConfig{}

	enabledStr := icl.client.GetSecretWithDefault("ZITADEL_ENABLED", "true")
	cfg.Enabled, _ = strconv.ParseBool(enabledStr)

	var err error
	cfg.Endpoint, err = icl.client.GetSecret("ZITADEL_ENDPOINT")
	if err != nil && cfg.Enabled {
		return nil, fmt.Errorf("ZITADEL_ENDPOINT is required when Zitadel is enabled: %w", err)
	}

	cfg.ProjectID, _ = icl.client.GetSecret("ZITADEL_PROJECT_ID")
	cfg.ClientID, _ = icl.client.GetSecret("ZITADEL_CLIENT_ID")
	cfg.ClientPrivateKey, _ = icl.client.GetSecret("ZITADEL_CLIENT_PRIVATE_KEY")
	cfg.RedirectURL, _ = icl.client.GetSecret("ZITADEL_REDIRECT_URL")
	cfg.Issuer, _ = icl.client.GetSecret("ZITADEL_ISSUER")
	cfg.JWKSURL, _ = icl.client.GetSecret("ZITADEL_JWKS_URL")

	return cfg, nil
}

// LoadAuthConfig loads authentication configuration from Infisical
func (icl *InfisicalConfigLoader) LoadAuthConfig() (*AuthConfig, error) {
	cfg := &AuthConfig{}

	var err error
	cfg.JWTSecret, err = icl.client.GetSecret("JWT_SECRET")
	if err != nil {
		return nil, fmt.Errorf("JWT_SECRET is required: %w", err)
	}

	cfg.SetDefaults()
	return cfg, nil
}

// GetSecret retrieves a single secret from Infisical
func (icl *InfisicalConfigLoader) GetSecret(key string) (string, error) {
	return icl.client.GetSecret(key)
}

// GetSecretWithDefault retrieves a secret with a fallback default value
func (icl *InfisicalConfigLoader) GetSecretWithDefault(key, defaultValue string) string {
	return icl.client.GetSecretWithDefault(key, defaultValue)
}

// InjectAllSecretsIntoEnv injects all Infisical secrets into environment variables
// This is useful for gradual migration to Infisical
func (icl *InfisicalConfigLoader) InjectAllSecretsIntoEnv() error {
	return icl.client.InjectSecretsIntoEnv()
}

// IsInfisicalEnabled checks if Infisical is enabled via environment variables
func IsInfisicalEnabled() bool {
	clientID := os.Getenv("INFISICAL_CLIENT_ID")
	clientSecret := os.Getenv("INFISICAL_CLIENT_SECRET")
	projectID := os.Getenv("INFISICAL_PROJECT_ID")

	return clientID != "" && clientSecret != "" && projectID != ""
}

// LoadConfigWithInfisical attempts to load configuration from Infisical first,
// falling back to environment variables and YAML files
func LoadConfigWithInfisical(log logger.Logger, yamlPath string, config interface{}) error {
	// Check if Infisical is enabled
	if !IsInfisicalEnabled() {
		log.Info("Infisical not configured, using traditional config loading")
		// Fall back to traditional loading from YAML
		if yamlPath != "" {
			if err := LoadFromFile(yamlPath, config); err != nil {
				return fmt.Errorf("failed to load config from file: %w", err)
			}
		}
		return nil
	}

	// Initialize Infisical client
	loader, err := NewInfisicalConfigLoader(log)
	if err != nil {
		log.Warn(fmt.Sprintf("Failed to initialize Infisical, falling back to traditional config: %v", err))
		if yamlPath != "" {
			if err := LoadFromFile(yamlPath, config); err != nil {
				return fmt.Errorf("failed to load config from file: %w", err)
			}
		}
		return nil
	}

	// Inject all secrets into environment variables
	// This allows existing code to work without modification
	if err := loader.InjectAllSecretsIntoEnv(); err != nil {
		log.Warn(fmt.Sprintf("Failed to inject Infisical secrets into env: %v", err))
	} else {
		log.Info("Successfully loaded secrets from Infisical into environment")
	}

	// Still load from YAML for non-secret configuration
	if yamlPath != "" {
		if err := LoadFromFile(yamlPath, config); err != nil {
			return fmt.Errorf("failed to load config from file: %w", err)
		}
	}

	return nil
}
