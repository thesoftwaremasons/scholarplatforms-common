// Package config provides secure secrets management
package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// SecretsManager handles encrypted secrets and sensitive configuration
type SecretsManager struct {
	logger    logger.Logger
	masterKey []byte
	secretDir string
}

// Secret represents an encrypted secret
type Secret struct {
	Name      string `json:"name"`
	Value     string `json:"value"`      // Base64 encoded encrypted value
	Nonce     string `json:"nonce"`      // Base64 encoded nonce
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// SecretConfig holds configuration for the secrets manager
type SecretConfig struct {
	SecretDir   string // Directory to store encrypted secrets
	MasterKey   string // Master key for encryption (from env var)
	KeyDerivation bool // Whether to use key derivation from a passphrase
}

// NewSecretsManager creates a new secrets manager
func NewSecretsManager(config SecretConfig, log logger.Logger) (*SecretsManager, error) {
	// Ensure secret directory exists
	if err := os.MkdirAll(config.SecretDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create secret directory: %w", err)
	}
	
	// Derive master key
	masterKey, err := deriveMasterKey(config.MasterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive master key: %w", err)
	}
	
	sm := &SecretsManager{
		logger:    log,
		masterKey: masterKey,
		secretDir: config.SecretDir,
	}
	
	log.Info("Secrets manager initialized",
		logger.String("secret_dir", config.SecretDir))
	
	return sm, nil
}

// SetSecret encrypts and stores a secret
func (sm *SecretsManager) SetSecret(name, value string) error {
	// Encrypt the value
	encryptedValue, nonce, err := sm.encrypt(value)
	if err != nil {
		return fmt.Errorf("failed to encrypt secret: %w", err)
	}
	
	// Store as environment variable for runtime access
	envKey := fmt.Sprintf("SECRET_%s", strings.ToUpper(name))
	os.Setenv(envKey, value)
	
	sm.logger.Info("Secret stored",
		logger.String("name", name),
		logger.String("env_key", envKey))
	
	// Optionally store encrypted version to disk for persistence
	secretFile := filepath.Join(sm.secretDir, fmt.Sprintf("%s.secret", name))
	secretData := fmt.Sprintf("%s:%s", base64.StdEncoding.EncodeToString(encryptedValue), 
		base64.StdEncoding.EncodeToString(nonce))
	
	if err := os.WriteFile(secretFile, []byte(secretData), 0600); err != nil {
		sm.logger.Warn("Failed to persist secret to disk",
			logger.String("name", name),
			logger.ErrorField(err))
	}
	
	return nil
}

// GetSecret retrieves and decrypts a secret
func (sm *SecretsManager) GetSecret(name string) (string, error) {
	// First try environment variable
	envKey := fmt.Sprintf("SECRET_%s", strings.ToUpper(name))
	if value := os.Getenv(envKey); value != "" {
		return value, nil
	}
	
	// Try loading from disk
	secretFile := filepath.Join(sm.secretDir, fmt.Sprintf("%s.secret", name))
	data, err := os.ReadFile(secretFile)
	if err != nil {
		return "", fmt.Errorf("secret not found: %s", name)
	}
	
	// Parse encrypted data
	parts := strings.SplitN(string(data), ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid secret format for: %s", name)
	}
	
	encryptedValue, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("failed to decode encrypted value: %w", err)
	}
	
	nonce, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("failed to decode nonce: %w", err)
	}
	
	// Decrypt
	plaintext, err := sm.decrypt(encryptedValue, nonce)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt secret: %w", err)
	}
	
	return plaintext, nil
}

// LoadSecretsFromEnv loads common secrets from environment variables
func (sm *SecretsManager) LoadSecretsFromEnv() error {
	commonSecrets := map[string]string{
		"database_password":     "DB_PASSWORD",
		"redis_password":        "VALKEY_PASSWORD",
		"jwt_secret":           "JWT_SECRET",
		"api_key":              "API_KEY",
		"service_token":        "SERVICE_TOKEN",
		"zitadel_client_secret": "ZITADEL_CLIENT_SECRET",
		"email_api_key":        "EMAIL_API_KEY",
		"sms_auth_token":       "SMS_AUTH_TOKEN",
		"slack_bot_token":      "SLACK_BOT_TOKEN",
		"encryption_key":       "ENCRYPTION_KEY",
	}
	
	for secretName, envVar := range commonSecrets {
		if value := os.Getenv(envVar); value != "" {
			if err := sm.SetSecret(secretName, value); err != nil {
				sm.logger.Error("Failed to store secret",
					logger.String("name", secretName),
					logger.ErrorField(err))
			}
		}
	}
	
	return nil
}

// GenerateSecrets generates default secrets for development
func (sm *SecretsManager) GenerateSecrets() error {
	secrets := map[string]func() string{
		"jwt_secret":     func() string { return generateRandomString(64) },
		"api_key":        func() string { return generateRandomString(32) },
		"service_token":  func() string { return generateRandomString(32) },
		"encryption_key": func() string { return generateRandomString(32) },
	}
	
	for name, generator := range secrets {
		// Only generate if not already set
		if _, err := sm.GetSecret(name); err != nil {
			value := generator()
			if err := sm.SetSecret(name, value); err != nil {
				return fmt.Errorf("failed to generate secret %s: %w", name, err)
			}
			sm.logger.Info("Generated secret", logger.String("name", name))
		}
	}
	
	return nil
}

// ListSecrets returns names of stored secrets
func (sm *SecretsManager) ListSecrets() []string {
	var secrets []string
	
	// Check environment variables
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "SECRET_") {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				secretName := strings.ToLower(strings.TrimPrefix(parts[0], "SECRET_"))
				secrets = append(secrets, secretName)
			}
		}
	}
	
	return secrets
}

// encrypt encrypts plaintext using AES-GCM
func (sm *SecretsManager) encrypt(plaintext string) ([]byte, []byte, error) {
	block, err := aes.NewCipher(sm.masterKey)
	if err != nil {
		return nil, nil, err
	}
	
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return ciphertext, nonce, nil
}

// decrypt decrypts ciphertext using AES-GCM
func (sm *SecretsManager) decrypt(ciphertext, nonce []byte) (string, error) {
	block, err := aes.NewCipher(sm.masterKey)
	if err != nil {
		return "", err
	}
	
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	
	return string(plaintext), nil
}

// deriveMasterKey derives a 32-byte key from the master key string
func deriveMasterKey(masterKey string) ([]byte, error) {
	if masterKey == "" {
		return nil, fmt.Errorf("master key cannot be empty")
	}
	
	// Use SHA-256 to derive a consistent 32-byte key
	hash := sha256.Sum256([]byte(masterKey))
	return hash[:], nil
}

// generateRandomString generates a random string of specified length
func generateRandomString(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a deterministic but unique string
		return fmt.Sprintf("default-key-%d", length)
	}
	return base64.URLEncoding.EncodeToString(bytes)[:length]
}

// ConfigWithSecrets integrates secrets manager with environment config
type ConfigWithSecrets struct {
	*EnvConfigManager
	*SecretsManager
}

// NewConfigWithSecrets creates a combined configuration manager
func NewConfigWithSecrets(envPrefix string, secretConfig SecretConfig, log logger.Logger) (*ConfigWithSecrets, error) {
	envManager := NewEnvConfigManager(envPrefix, log)
	
	secretsManager, err := NewSecretsManager(secretConfig, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create secrets manager: %w", err)
	}
	
	// Load secrets from environment
	if err := secretsManager.LoadSecretsFromEnv(); err != nil {
		log.Warn("Failed to load some secrets from environment", logger.ErrorField(err))
	}
	
	return &ConfigWithSecrets{
		EnvConfigManager: envManager,
		SecretsManager:   secretsManager,
	}, nil
}

// GetSecureString gets a string value, trying secrets first, then environment variables
func (c *ConfigWithSecrets) GetSecureString(key string, defaultValue string) string {
	// Try secrets first
	if value, err := c.SecretsManager.GetSecret(key); err == nil {
		return value
	}
	
	// Fallback to environment variable
	return c.EnvConfigManager.GetString(key, defaultValue)
}