package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/errors"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/validation"
	"gopkg.in/yaml.v3"
)

// ConfigLoader loads and validates configuration from multiple sources
type ConfigLoader struct {
	validator validation.Validator
}

// NewConfigLoader creates a new configuration loader
func NewConfigLoader() *ConfigLoader {
	return &ConfigLoader{
		validator: validation.NewValidator(),
	}
}

// LoadFromFile loads configuration from a YAML file with environment variable substitution
func (cl *ConfigLoader) LoadFromFile(filename string, config interface{}) error {
	// Read the YAML file
	data, err := os.ReadFile(filename)
	if err != nil {
		return errors.WrapError(err, errors.ErrCodeInternalError, "Failed to read config file", 500)
	}
	
	// Substitute environment variables in the YAML content
	content := string(data)
	content = cl.substituteEnvVars(content)
	
	// Unmarshal YAML
	if err := yaml.Unmarshal([]byte(content), config); err != nil {
		return errors.WrapError(err, errors.ErrCodeInvalidFormat, "Failed to parse config file", 400)
	}
	
	// Load environment variables
	if err := cl.loadEnvVars(config); err != nil {
		return err
	}
	
	// Validate configuration
	if err := cl.validator.ValidateStruct(config); err != nil {
		return errors.WrapError(err, errors.ErrCodeValidationFailed, "Configuration validation failed", 400)
	}
	
	return nil
}

// LoadFromEnv loads configuration only from environment variables
func (cl *ConfigLoader) LoadFromEnv(config interface{}) error {
	// Load environment variables
	if err := cl.loadEnvVars(config); err != nil {
		return err
	}
	
	// Validate configuration
	if err := cl.validator.ValidateStruct(config); err != nil {
		return errors.WrapError(err, errors.ErrCodeValidationFailed, "Configuration validation failed", 400)
	}
	
	return nil
}

// substituteEnvVars replaces ${VAR_NAME} or $VAR_NAME patterns with environment variable values
func (cl *ConfigLoader) substituteEnvVars(content string) string {
	// Replace ${VAR_NAME} pattern
	for {
		start := strings.Index(content, "${")
		if start == -1 {
			break
		}
		
		end := strings.Index(content[start:], "}")
		if end == -1 {
			break
		}
		end += start
		
		varName := content[start+2 : end]
		envValue := os.Getenv(varName)
		
		// If environment variable is not set, keep the placeholder
		if envValue == "" {
			envValue = content[start : end+1] // Keep original ${VAR_NAME}
		}
		
		content = content[:start] + envValue + content[end+1:]
	}
	
	return content
}

// loadEnvVars loads environment variables into the configuration struct
func (cl *ConfigLoader) loadEnvVars(config interface{}) error {
	val := reflect.ValueOf(config)
	if val.Kind() != reflect.Ptr {
		return errors.NewValidationError("Config must be a pointer")
	}
	
	val = val.Elem()
	if val.Kind() != reflect.Struct {
		return errors.NewValidationError("Config must be a struct")
	}
	
	return cl.loadEnvVarsRecursive(val, reflect.TypeOf(config).Elem())
}

// loadEnvVarsRecursive recursively loads environment variables for nested structs
func (cl *ConfigLoader) loadEnvVarsRecursive(val reflect.Value, typ reflect.Type) error {
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)
		
		// Skip unexported fields
		if !field.CanSet() {
			continue
		}
		
		// Handle nested structs
		if field.Kind() == reflect.Struct {
			if err := cl.loadEnvVarsRecursive(field, fieldType.Type); err != nil {
				return err
			}
			continue
		}
		
		// Get environment variable tag
		envTag := fieldType.Tag.Get("env")
		if envTag == "" {
			continue
		}
		
		// Get environment variable value
		envValue := os.Getenv(envTag)
		if envValue == "" {
			continue // Skip if not set
		}
		
		// Set field value based on type
		if err := cl.setFieldValue(field, envValue, fieldType.Name); err != nil {
			return errors.NewInvalidInputError(fieldType.Name, fmt.Sprintf("Failed to parse env var %s: %v", envTag, err))
		}
	}
	
	return nil
}

// setFieldValue sets a field value from a string, converting to the appropriate type
func (cl *ConfigLoader) setFieldValue(field reflect.Value, value, fieldName string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
		
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			// Handle time.Duration
			duration, err := time.ParseDuration(value)
			if err != nil {
				return fmt.Errorf("invalid duration: %v", err)
			}
			field.Set(reflect.ValueOf(duration))
		} else {
			// Handle regular integers
			intVal, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid integer: %v", err)
			}
			field.SetInt(intVal)
		}
		
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintVal, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid unsigned integer: %v", err)
		}
		field.SetUint(uintVal)
		
	case reflect.Float32, reflect.Float64:
		floatVal, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid float: %v", err)
		}
		field.SetFloat(floatVal)
		
	case reflect.Bool:
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean: %v", err)
		}
		field.SetBool(boolVal)
		
	case reflect.Slice:
		// Handle slices (comma-separated values)
		if field.Type().Elem().Kind() == reflect.String {
			values := strings.Split(value, ",")
			for i, v := range values {
				values[i] = strings.TrimSpace(v)
			}
			field.Set(reflect.ValueOf(values))
		} else {
			return fmt.Errorf("unsupported slice type: %v", field.Type())
		}
		
	default:
		return fmt.Errorf("unsupported field type: %v", field.Type())
	}
	
	return nil
}

// ValidateConfig validates a configuration struct
func (cl *ConfigLoader) ValidateConfig(config interface{}) error {
	return cl.validator.ValidateStruct(config)
}

// GetRequiredEnvVars returns a list of required environment variables for a config struct
func (cl *ConfigLoader) GetRequiredEnvVars(config interface{}) ([]string, error) {
	var envVars []string
	
	val := reflect.ValueOf(config)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	
	if val.Kind() != reflect.Struct {
		return nil, errors.NewValidationError("Config must be a struct")
	}
	
	typ := val.Type()
	cl.getRequiredEnvVarsRecursive(typ, &envVars)
	
	return envVars, nil
}

// getRequiredEnvVarsRecursive recursively finds required environment variables
func (cl *ConfigLoader) getRequiredEnvVarsRecursive(typ reflect.Type, envVars *[]string) {
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		
		// Handle nested structs
		if field.Type.Kind() == reflect.Struct {
			cl.getRequiredEnvVarsRecursive(field.Type, envVars)
			continue
		}
		
		// Check for env tag
		envTag := field.Tag.Get("env")
		if envTag == "" {
			continue
		}
		
		// Check if field is required
		validateTag := field.Tag.Get("validate")
		if strings.Contains(validateTag, "required") {
			*envVars = append(*envVars, envTag)
		}
	}
}

// PrintConfig prints a configuration struct with sensitive values masked
func (cl *ConfigLoader) PrintConfig(config interface{}) {
	val := reflect.ValueOf(config)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	
	if val.Kind() != reflect.Struct {
		fmt.Println("Config must be a struct")
		return
	}
	
	typ := val.Type()
	fmt.Println("Configuration:")
	cl.printConfigRecursive(val, typ, "")
}

// printConfigRecursive recursively prints configuration values
func (cl *ConfigLoader) printConfigRecursive(val reflect.Value, typ reflect.Type, prefix string) {
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)
		
		if !field.CanInterface() {
			continue
		}
		
		fieldName := prefix + fieldType.Name
		
		// Handle nested structs
		if field.Kind() == reflect.Struct {
			fmt.Printf("  %s:\n", fieldName)
			cl.printConfigRecursive(field, fieldType.Type, "  ")
			continue
		}
		
		// Mask sensitive values
		value := field.Interface()
		if cl.isSensitiveField(fieldType.Name) {
			if str, ok := value.(string); ok && str != "" {
				value = strings.Repeat("*", min(len(str), 8))
			}
		}
		
		fmt.Printf("  %s: %v\n", fieldName, value)
	}
}

// isSensitiveField checks if a field contains sensitive information
func (cl *ConfigLoader) isSensitiveField(fieldName string) bool {
	sensitive := []string{"password", "secret", "key", "token", "credential"}
	fieldLower := strings.ToLower(fieldName)
	
	for _, s := range sensitive {
		if strings.Contains(fieldLower, s) {
			return true
		}
	}
	
	return false
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Global config loader instance
var defaultLoader = NewConfigLoader()

// Load loads configuration using the default loader
func Load(filename string, config interface{}) error {
	return defaultLoader.LoadFromFile(filename, config)
}

// LoadFromEnv loads configuration from environment variables using the default loader
func LoadFromEnv(config interface{}) error {
	return defaultLoader.LoadFromEnv(config)
}

// Validate validates configuration using the default loader
func Validate(config interface{}) error {
	return defaultLoader.ValidateConfig(config)
}

// PrintConfig prints configuration using the default loader
func PrintConfig(config interface{}) {
	defaultLoader.PrintConfig(config)
}