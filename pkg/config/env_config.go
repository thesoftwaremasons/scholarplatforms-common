// Package config provides centralized configuration management with environment variables
package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// EnvConfigManager handles environment variable configuration
type EnvConfigManager struct {
	logger      logger.Logger
	prefix      string
	defaults    map[string]interface{}
	validators  map[string]func(string) error
	required    map[string]bool
	sensitive   map[string]bool // Marks sensitive fields for secure logging
}

// ConfigOption represents a configuration option
type ConfigOption struct {
	Key          string
	DefaultValue interface{}
	Required     bool
	Sensitive    bool
	Validator    func(string) error
	Description  string
}

// NewEnvConfigManager creates a new environment configuration manager
func NewEnvConfigManager(prefix string, log logger.Logger) *EnvConfigManager {
	return &EnvConfigManager{
		logger:     log,
		prefix:     strings.ToUpper(prefix),
		defaults:   make(map[string]interface{}),
		validators: make(map[string]func(string) error),
		required:   make(map[string]bool),
		sensitive:  make(map[string]bool),
	}
}

// RegisterConfig registers configuration options
func (e *EnvConfigManager) RegisterConfig(options []ConfigOption) {
	for _, option := range options {
		key := e.buildEnvKey(option.Key)
		
		e.defaults[key] = option.DefaultValue
		e.required[key] = option.Required
		e.sensitive[key] = option.Sensitive
		
		if option.Validator != nil {
			e.validators[key] = option.Validator
		}
		
		e.logger.Debug("Registered config option",
			logger.String("key", key),
			logger.String("description", option.Description),
			logger.Bool("required", option.Required),
			logger.Bool("sensitive", option.Sensitive))
	}
}

// LoadConfig loads configuration into a struct using reflection
func (e *EnvConfigManager) LoadConfig(config interface{}) error {
	rv := reflect.ValueOf(config)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("config must be a non-nil pointer to a struct")
	}
	
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("config must be a pointer to a struct")
	}
	
	rt := rv.Type()
	
	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)
		fieldType := rt.Field(i)
		
		// Skip unexported fields
		if !field.CanSet() {
			continue
		}
		
		// Get environment variable key from tag or field name
		envKey := e.getEnvKey(fieldType)
		
		// Load the value
		if err := e.setFieldFromEnv(field, fieldType, envKey); err != nil {
			return fmt.Errorf("failed to set field %s: %w", fieldType.Name, err)
		}
	}
	
	return nil
}

// ValidateConfig validates all registered configuration
func (e *EnvConfigManager) ValidateConfig() error {
	var errors []string
	
	for key, isRequired := range e.required {
		value := os.Getenv(key)
		
		// Check required fields
		if isRequired && value == "" {
			errors = append(errors, fmt.Sprintf("required environment variable %s is not set", key))
			continue
		}
		
		// Run custom validators
		if validator, exists := e.validators[key]; exists && value != "" {
			if err := validator(value); err != nil {
				errors = append(errors, fmt.Sprintf("validation failed for %s: %v", key, err))
			}
		}
	}
	
	if len(errors) > 0 {
		return fmt.Errorf("configuration validation failed:\n%s", strings.Join(errors, "\n"))
	}
	
	return nil
}

// GetString gets a string environment variable with default
func (e *EnvConfigManager) GetString(key string, defaultValue string) string {
	envKey := e.buildEnvKey(key)
	if value := os.Getenv(envKey); value != "" {
		return value
	}
	return defaultValue
}

// GetInt gets an integer environment variable with default
func (e *EnvConfigManager) GetInt(key string, defaultValue int) int {
	envKey := e.buildEnvKey(key)
	if value := os.Getenv(envKey); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

// GetInt64 gets an int64 environment variable with default
func (e *EnvConfigManager) GetInt64(key string, defaultValue int64) int64 {
	envKey := e.buildEnvKey(key)
	if value := os.Getenv(envKey); value != "" {
		if int64Val, err := strconv.ParseInt(value, 10, 64); err == nil {
			return int64Val
		}
	}
	return defaultValue
}

// GetFloat64 gets a float64 environment variable with default
func (e *EnvConfigManager) GetFloat64(key string, defaultValue float64) float64 {
	envKey := e.buildEnvKey(key)
	if value := os.Getenv(envKey); value != "" {
		if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
			return floatVal
		}
	}
	return defaultValue
}

// GetBool gets a boolean environment variable with default
func (e *EnvConfigManager) GetBool(key string, defaultValue bool) bool {
	envKey := e.buildEnvKey(key)
	if value := os.Getenv(envKey); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultValue
}

// GetDuration gets a duration environment variable with default
func (e *EnvConfigManager) GetDuration(key string, defaultValue time.Duration) time.Duration {
	envKey := e.buildEnvKey(key)
	if value := os.Getenv(envKey); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

// GetStringSlice gets a string slice environment variable (comma-separated)
func (e *EnvConfigManager) GetStringSlice(key string, defaultValue []string) []string {
	envKey := e.buildEnvKey(key)
	if value := os.Getenv(envKey); value != "" {
		return strings.Split(strings.TrimSpace(value), ",")
	}
	return defaultValue
}

// SetDefault sets a default value for a key
func (e *EnvConfigManager) SetDefault(key string, value interface{}) {
	envKey := e.buildEnvKey(key)
	e.defaults[envKey] = value
}

// PrintConfig prints current configuration (masking sensitive values)
func (e *EnvConfigManager) PrintConfig() {
	e.logger.Info("Current configuration:")
	
	for key := range e.defaults {
		value := os.Getenv(key)
		if value == "" {
			if defaultVal, exists := e.defaults[key]; exists {
				value = fmt.Sprintf("%v (default)", defaultVal)
			} else {
				value = "<not set>"
			}
		}
		
		// Mask sensitive values
		if e.sensitive[key] {
			if value != "<not set>" {
				value = "***MASKED***"
			}
		}
		
		e.logger.Info("Config",
			logger.String("key", key),
			logger.String("value", value))
	}
}

// buildEnvKey builds environment variable key with prefix
func (e *EnvConfigManager) buildEnvKey(key string) string {
	return fmt.Sprintf("%s_%s", e.prefix, strings.ToUpper(key))
}

// getEnvKey extracts environment key from struct field tag
func (e *EnvConfigManager) getEnvKey(field reflect.StructField) string {
	// Check for env tag first
	if envTag := field.Tag.Get("env"); envTag != "" {
		return e.buildEnvKey(envTag)
	}
	
	// Check for yaml tag and convert
	if yamlTag := field.Tag.Get("yaml"); yamlTag != "" {
		// Convert snake_case to UPPER_CASE
		return e.buildEnvKey(strings.ToUpper(yamlTag))
	}
	
	// Use field name
	return e.buildEnvKey(field.Name)
}

// setFieldFromEnv sets struct field value from environment variable
func (e *EnvConfigManager) setFieldFromEnv(field reflect.Value, fieldType reflect.StructField, envKey string) error {
	value := os.Getenv(envKey)
	
	// Use default if no environment value
	if value == "" {
		if defaultVal, exists := e.defaults[envKey]; exists {
			return e.setFieldValue(field, defaultVal)
		}
		return nil // Leave zero value
	}
	
	// Set the field based on its type
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			// Handle time.Duration specially
			if duration, err := time.ParseDuration(value); err == nil {
				field.Set(reflect.ValueOf(duration))
			} else {
				return fmt.Errorf("invalid duration format: %s", value)
			}
		} else {
			if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
				field.SetInt(intVal)
			} else {
				return fmt.Errorf("invalid integer format: %s", value)
			}
		}
	case reflect.Float32, reflect.Float64:
		if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
			field.SetFloat(floatVal)
		} else {
			return fmt.Errorf("invalid float format: %s", value)
		}
	case reflect.Bool:
		if boolVal, err := strconv.ParseBool(value); err == nil {
			field.SetBool(boolVal)
		} else {
			return fmt.Errorf("invalid boolean format: %s", value)
		}
	case reflect.Slice:
		if field.Type().Elem().Kind() == reflect.String {
			// Handle []string
			values := strings.Split(strings.TrimSpace(value), ",")
			sliceVal := reflect.MakeSlice(field.Type(), len(values), len(values))
			for i, v := range values {
				sliceVal.Index(i).SetString(strings.TrimSpace(v))
			}
			field.Set(sliceVal)
		}
	default:
		return fmt.Errorf("unsupported field type: %v", field.Kind())
	}
	
	return nil
}

// setFieldValue sets field value using reflection
func (e *EnvConfigManager) setFieldValue(field reflect.Value, value interface{}) error {
	valueReflect := reflect.ValueOf(value)
	
	if !valueReflect.Type().AssignableTo(field.Type()) {
		return fmt.Errorf("cannot assign %v to %v", valueReflect.Type(), field.Type())
	}
	
	field.Set(valueReflect)
	return nil
}

// Common validators
func ValidatePort(value string) error {
	port, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid port number: %s", value)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got: %d", port)
	}
	return nil
}

func ValidateURL(value string) error {
	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		return fmt.Errorf("URL must start with http:// or https://")
	}
	return nil
}

func ValidateNonEmpty(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("value cannot be empty")
	}
	return nil
}