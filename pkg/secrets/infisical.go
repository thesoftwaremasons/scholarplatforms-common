package secrets

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	infisical "github.com/infisical/go-sdk"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// InfisicalConfig holds the configuration for Infisical client
type InfisicalConfig struct {
	ClientID     string
	ClientSecret string
	ProjectID    string
	Environment  string
	SiteURL      string
	CacheTTL     time.Duration
}

// InfisicalClient is a wrapper around the Infisical SDK with caching
type InfisicalClient struct {
	client      infisical.InfisicalClientInterface
	config      InfisicalConfig
	cache       map[string]cachedSecret
	cacheMutex  sync.RWMutex
	logger      logger.Logger
	ctx         context.Context
}

// cachedSecret represents a cached secret with expiration
type cachedSecret struct {
	value     string
	expiresAt time.Time
}

// NewInfisicalClient creates a new Infisical client with configuration
func NewInfisicalClient(cfg InfisicalConfig, log logger.Logger) (*InfisicalClient, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, fmt.Errorf("Infisical client ID and secret are required")
	}

	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("Infisical project ID is required")
	}

	if cfg.Environment == "" {
		cfg.Environment = "dev"
	}

	if cfg.SiteURL == "" {
		cfg.SiteURL = os.Getenv("INFISICAL_API_URL")
		if cfg.SiteURL == "" {
			cfg.SiteURL = "https://app.infisical.com"
		}
	}

	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 5 * time.Minute // Default cache TTL
	}

	client := infisical.NewInfisicalClient(context.Background(), infisical.Config{
		SiteUrl: cfg.SiteURL,
	})

	// Authenticate with machine identity (service account)
	_, err := client.Auth().UniversalAuthLogin(cfg.ClientID, cfg.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate with Infisical: %w", err)
	}

	log.Info("Successfully authenticated with Infisical")

	return &InfisicalClient{
		client:     client,
		config:     cfg,
		cache:      make(map[string]cachedSecret),
		logger:     log,
		ctx:        context.Background(),
	}, nil
}

// NewInfisicalClientFromEnv creates a new Infisical client from environment variables
func NewInfisicalClientFromEnv(log logger.Logger) (*InfisicalClient, error) {
	cfg := InfisicalConfig{
		ClientID:     os.Getenv("INFISICAL_CLIENT_ID"),
		ClientSecret: os.Getenv("INFISICAL_CLIENT_SECRET"),
		ProjectID:    os.Getenv("INFISICAL_PROJECT_ID"),
		Environment:  os.Getenv("INFISICAL_ENVIRONMENT"),
		SiteURL:      os.Getenv("INFISICAL_API_URL"),
	}

	if cacheTTLStr := os.Getenv("INFISICAL_CACHE_TTL"); cacheTTLStr != "" {
		if ttl, err := time.ParseDuration(cacheTTLStr); err == nil {
			cfg.CacheTTL = ttl
		}
	}

	return NewInfisicalClient(cfg, log)
}

// GetSecret retrieves a secret by key with caching
func (ic *InfisicalClient) GetSecret(key string) (string, error) {
	// Check cache first
	ic.cacheMutex.RLock()
	if cached, exists := ic.cache[key]; exists {
		if time.Now().Before(cached.expiresAt) {
			ic.cacheMutex.RUnlock()
			ic.logger.Debug(fmt.Sprintf("Retrieved secret '%s' from cache", key))
			return cached.value, nil
		}
	}
	ic.cacheMutex.RUnlock()

	// Fetch from Infisical
	secret, err := ic.client.Secrets().Retrieve(infisical.RetrieveSecretOptions{
		ProjectID:   ic.config.ProjectID,
		Environment: ic.config.Environment,
		SecretKey:   key,
	})
	if err != nil {
		return "", fmt.Errorf("failed to retrieve secret '%s': %w", key, err)
	}

	// Update cache
	ic.cacheMutex.Lock()
	ic.cache[key] = cachedSecret{
		value:     secret.SecretValue,
		expiresAt: time.Now().Add(ic.config.CacheTTL),
	}
	ic.cacheMutex.Unlock()

	ic.logger.Info(fmt.Sprintf("Retrieved secret '%s' from Infisical", key))
	return secret.SecretValue, nil
}

// GetSecretWithDefault retrieves a secret or returns a default value if not found
func (ic *InfisicalClient) GetSecretWithDefault(key, defaultValue string) string {
	value, err := ic.GetSecret(key)
	if err != nil {
		ic.logger.Warn(fmt.Sprintf("Failed to get secret '%s', using default: %v", key, err))
		return defaultValue
	}
	return value
}

// GetAllSecrets retrieves all secrets for the project and environment
func (ic *InfisicalClient) GetAllSecrets() (map[string]string, error) {
	secrets, err := ic.client.Secrets().List(infisical.ListSecretsOptions{
		ProjectID:   ic.config.ProjectID,
		Environment: ic.config.Environment,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	result := make(map[string]string)
	ic.cacheMutex.Lock()
	defer ic.cacheMutex.Unlock()

	for _, secret := range secrets {
		result[secret.SecretKey] = secret.SecretValue

		// Update cache
		ic.cache[secret.SecretKey] = cachedSecret{
			value:     secret.SecretValue,
			expiresAt: time.Now().Add(ic.config.CacheTTL),
		}
	}

	ic.logger.Info(fmt.Sprintf("Retrieved %d secrets from Infisical", len(result)))
	return result, nil
}

// CreateSecret creates a new secret
func (ic *InfisicalClient) CreateSecret(key, value string) error {
	_, err := ic.client.Secrets().Create(infisical.CreateSecretOptions{
		ProjectID:   ic.config.ProjectID,
		Environment: ic.config.Environment,
		SecretKey:   key,
		SecretValue: value,
	})
	if err != nil {
		return fmt.Errorf("failed to create secret '%s': %w", key, err)
	}

	// Invalidate cache for this key
	ic.cacheMutex.Lock()
	delete(ic.cache, key)
	ic.cacheMutex.Unlock()

	ic.logger.Info(fmt.Sprintf("Created secret '%s' in Infisical", key))
	return nil
}

// UpdateSecret updates an existing secret
func (ic *InfisicalClient) UpdateSecret(key, value string) error {
	_, err := ic.client.Secrets().Update(infisical.UpdateSecretOptions{
		ProjectID:      ic.config.ProjectID,
		Environment:    ic.config.Environment,
		SecretKey:      key,
		NewSecretValue: value,
	})
	if err != nil {
		return fmt.Errorf("failed to update secret '%s': %w", key, err)
	}

	// Invalidate cache for this key
	ic.cacheMutex.Lock()
	delete(ic.cache, key)
	ic.cacheMutex.Unlock()

	ic.logger.Info(fmt.Sprintf("Updated secret '%s' in Infisical", key))
	return nil
}

// DeleteSecret deletes a secret
func (ic *InfisicalClient) DeleteSecret(key string) error {
	_, err := ic.client.Secrets().Delete(infisical.DeleteSecretOptions{
		ProjectID:   ic.config.ProjectID,
		Environment: ic.config.Environment,
		SecretKey:   key,
	})
	if err != nil {
		return fmt.Errorf("failed to delete secret '%s': %w", key, err)
	}

	// Remove from cache
	ic.cacheMutex.Lock()
	delete(ic.cache, key)
	ic.cacheMutex.Unlock()

	ic.logger.Info(fmt.Sprintf("Deleted secret '%s' from Infisical", key))
	return nil
}

// ClearCache clears all cached secrets
func (ic *InfisicalClient) ClearCache() {
	ic.cacheMutex.Lock()
	ic.cache = make(map[string]cachedSecret)
	ic.cacheMutex.Unlock()
	ic.logger.Info("Cleared Infisical secret cache")
}

// RefreshCache refreshes all cached secrets
func (ic *InfisicalClient) RefreshCache() error {
	_, err := ic.GetAllSecrets()
	return err
}

// InjectSecretsIntoEnv retrieves all secrets and injects them into environment variables
// This is useful for legacy applications that read from environment variables
func (ic *InfisicalClient) InjectSecretsIntoEnv() error {
	secrets, err := ic.GetAllSecrets()
	if err != nil {
		return fmt.Errorf("failed to inject secrets into environment: %w", err)
	}

	for key, value := range secrets {
		if err := os.Setenv(key, value); err != nil {
			ic.logger.Warn(fmt.Sprintf("Failed to set environment variable '%s': %v", key, err))
		}
	}

	ic.logger.Info(fmt.Sprintf("Injected %d secrets into environment variables", len(secrets)))
	return nil
}
