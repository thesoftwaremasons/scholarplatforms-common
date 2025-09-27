package validation

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/errors"
)

// Validator interface defines validation methods
type Validator interface {
	Validate(interface{}) *errors.AppError
	ValidateStruct(interface{}) *errors.AppError
	ValidateField(fieldName string, value interface{}, rules string) *errors.AppError
}

// DefaultValidator implements the Validator interface
type DefaultValidator struct {
	customValidators map[string]ValidationFunc
}

// ValidationFunc represents a custom validation function
type ValidationFunc func(value interface{}, param string) bool

// ValidationRule represents a single validation rule
type ValidationRule struct {
	Name  string
	Param string
}

// NewValidator creates a new validator instance
func NewValidator() *DefaultValidator {
	v := &DefaultValidator{
		customValidators: make(map[string]ValidationFunc),
	}
	
	// Register built-in validators
	v.RegisterValidator("email", validateEmail)
	v.RegisterValidator("phone", validatePhone)
	v.RegisterValidator("url", validateURL)
	v.RegisterValidator("uuid", validateUUID)
	v.RegisterValidator("date", validateDate)
	v.RegisterValidator("time", validateTime)
	
	return v
}

// RegisterValidator registers a custom validation function
func (v *DefaultValidator) RegisterValidator(name string, fn ValidationFunc) {
	v.customValidators[name] = fn
}

// Validate validates a struct or field
func (v *DefaultValidator) Validate(data interface{}) *errors.AppError {
	return v.ValidateStruct(data)
}

// ValidateStruct validates a struct using struct tags
func (v *DefaultValidator) ValidateStruct(data interface{}) *errors.AppError {
	val := reflect.ValueOf(data)
	typ := reflect.TypeOf(data)
	
	// Handle pointers
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return errors.NewValidationError("Cannot validate nil pointer")
		}
		val = val.Elem()
		typ = typ.Elem()
	}
	
	if val.Kind() != reflect.Struct {
		return errors.NewValidationError("Data must be a struct")
	}
	
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)
		
		// Skip unexported fields
		if !field.CanInterface() {
			continue
		}
		
		// Get validation tag
		validationTag := fieldType.Tag.Get("validate")
		if validationTag == "" {
			continue
		}
		
		// Get JSON tag for field name (fallback to struct field name)
		fieldName := fieldType.Tag.Get("json")
		if fieldName == "" {
			fieldName = fieldType.Name
		}
		fieldName = strings.Split(fieldName, ",")[0] // Remove json options like omitempty
		
		// Validate field
		if err := v.ValidateField(fieldName, field.Interface(), validationTag); err != nil {
			return err
		}
	}
	
	return nil
}

// ValidateField validates a single field with given rules
func (v *DefaultValidator) ValidateField(fieldName string, value interface{}, rules string) *errors.AppError {
	if rules == "" {
		return nil
	}
	
	// Parse validation rules
	validationRules := parseValidationRules(rules)
	
	for _, rule := range validationRules {
		if err := v.validateRule(fieldName, value, rule); err != nil {
			return err
		}
	}
	
	return nil
}

// validateRule validates a single rule
func (v *DefaultValidator) validateRule(fieldName string, value interface{}, rule ValidationRule) *errors.AppError {
	switch rule.Name {
	case "required":
		if !v.isRequired(value) {
			return errors.NewMissingFieldError(fieldName)
		}
	case "min":
		if !v.validateMin(value, rule.Param) {
			return errors.NewInvalidInputError(fieldName, fmt.Sprintf("must be at least %s", rule.Param))
		}
	case "max":
		if !v.validateMax(value, rule.Param) {
			return errors.NewInvalidInputError(fieldName, fmt.Sprintf("must not exceed %s", rule.Param))
		}
	case "len":
		if !v.validateLen(value, rule.Param) {
			return errors.NewInvalidInputError(fieldName, fmt.Sprintf("must have length %s", rule.Param))
		}
	case "oneof":
		if !v.validateOneOf(value, rule.Param) {
			return errors.NewInvalidInputError(fieldName, fmt.Sprintf("must be one of: %s", rule.Param))
		}
	case "regex":
		if !v.validateRegex(value, rule.Param) {
			return errors.NewInvalidFormatError(fieldName, "must match pattern: "+rule.Param)
		}
	default:
		// Check custom validators
		if fn, exists := v.customValidators[rule.Name]; exists {
			if !fn(value, rule.Param) {
				return errors.NewInvalidInputError(fieldName, fmt.Sprintf("failed %s validation", rule.Name))
			}
		} else {
			return errors.NewValidationError(fmt.Sprintf("Unknown validation rule: %s", rule.Name))
		}
	}
	
	return nil
}

// Built-in validation functions

func (v *DefaultValidator) isRequired(value interface{}) bool {
	if value == nil {
		return false
	}
	
	val := reflect.ValueOf(value)
	switch val.Kind() {
	case reflect.String:
		return strings.TrimSpace(val.String()) != ""
	case reflect.Slice, reflect.Array, reflect.Map:
		return val.Len() > 0
	case reflect.Ptr:
		return !val.IsNil()
	default:
		return !val.IsZero()
	}
}

func (v *DefaultValidator) validateMin(value interface{}, param string) bool {
	minVal, err := strconv.Atoi(param)
	if err != nil {
		return false
	}
	
	val := reflect.ValueOf(value)
	switch val.Kind() {
	case reflect.String:
		return len(val.String()) >= minVal
	case reflect.Slice, reflect.Array:
		return val.Len() >= minVal
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return val.Int() >= int64(minVal)
	case reflect.Float32, reflect.Float64:
		return val.Float() >= float64(minVal)
	}
	
	return false
}

func (v *DefaultValidator) validateMax(value interface{}, param string) bool {
	maxVal, err := strconv.Atoi(param)
	if err != nil {
		return false
	}
	
	val := reflect.ValueOf(value)
	switch val.Kind() {
	case reflect.String:
		return len(val.String()) <= maxVal
	case reflect.Slice, reflect.Array:
		return val.Len() <= maxVal
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return val.Int() <= int64(maxVal)
	case reflect.Float32, reflect.Float64:
		return val.Float() <= float64(maxVal)
	}
	
	return false
}

func (v *DefaultValidator) validateLen(value interface{}, param string) bool {
	expectedLen, err := strconv.Atoi(param)
	if err != nil {
		return false
	}
	
	val := reflect.ValueOf(value)
	switch val.Kind() {
	case reflect.String:
		return len(val.String()) == expectedLen
	case reflect.Slice, reflect.Array:
		return val.Len() == expectedLen
	}
	
	return false
}

func (v *DefaultValidator) validateOneOf(value interface{}, param string) bool {
	options := strings.Split(param, " ")
	valueStr := fmt.Sprintf("%v", value)
	
	for _, option := range options {
		if strings.TrimSpace(option) == valueStr {
			return true
		}
	}
	
	return false
}

func (v *DefaultValidator) validateRegex(value interface{}, param string) bool {
	regex, err := regexp.Compile(param)
	if err != nil {
		return false
	}
	
	valueStr := fmt.Sprintf("%v", value)
	return regex.MatchString(valueStr)
}

// Custom validation functions

func validateEmail(value interface{}, param string) bool {
	email, ok := value.(string)
	if !ok {
		return false
	}
	
	// Basic email regex
	emailRegex := `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`
	regex, err := regexp.Compile(emailRegex)
	if err != nil {
		return false
	}
	
	return regex.MatchString(email)
}

func validatePhone(value interface{}, param string) bool {
	phone, ok := value.(string)
	if !ok {
		return false
	}
	
	// Remove common phone number characters
	cleanPhone := strings.ReplaceAll(phone, " ", "")
	cleanPhone = strings.ReplaceAll(cleanPhone, "-", "")
	cleanPhone = strings.ReplaceAll(cleanPhone, "(", "")
	cleanPhone = strings.ReplaceAll(cleanPhone, ")", "")
	cleanPhone = strings.ReplaceAll(cleanPhone, "+", "")
	
	// Check if remaining characters are digits and length is reasonable
	phoneRegex := `^\d{10,15}$`
	regex, err := regexp.Compile(phoneRegex)
	if err != nil {
		return false
	}
	
	return regex.MatchString(cleanPhone)
}

func validateURL(value interface{}, param string) bool {
	url, ok := value.(string)
	if !ok {
		return false
	}
	
	urlRegex := `^https?://[^\s/$.?#].[^\s]*$`
	regex, err := regexp.Compile(urlRegex)
	if err != nil {
		return false
	}
	
	return regex.MatchString(url)
}

func validateUUID(value interface{}, param string) bool {
	uuidStr, ok := value.(string)
	if !ok {
		return false
	}
	
	_, err := uuid.Parse(uuidStr)
	return err == nil
}

func validateDate(value interface{}, param string) bool {
	dateStr, ok := value.(string)
	if !ok {
		return false
	}
	
	format := param
	if format == "" {
		format = "2006-01-02" // Default ISO date format
	}
	
	_, err := time.Parse(format, dateStr)
	return err == nil
}

func validateTime(value interface{}, param string) bool {
	timeStr, ok := value.(string)
	if !ok {
		return false
	}
	
	format := param
	if format == "" {
		format = "15:04:05" // Default time format
	}
	
	_, err := time.Parse(format, timeStr)
	return err == nil
}

// Utility functions

func parseValidationRules(rules string) []ValidationRule {
	var validationRules []ValidationRule
	parts := strings.Split(rules, ",")
	
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		
		// Split rule and parameter (e.g., "min=5")
		ruleParts := strings.Split(part, "=")
		rule := ValidationRule{
			Name: strings.TrimSpace(ruleParts[0]),
		}
		
		if len(ruleParts) > 1 {
			rule.Param = strings.TrimSpace(ruleParts[1])
		}
		
		validationRules = append(validationRules, rule)
	}
	
	return validationRules
}

// Global validator instance
var globalValidator = NewValidator()

// Validate validates using the global validator instance
func Validate(data interface{}) *errors.AppError {
	return globalValidator.Validate(data)
}

// ValidateStruct validates a struct using the global validator instance
func ValidateStruct(data interface{}) *errors.AppError {
	return globalValidator.ValidateStruct(data)
}

// ValidateField validates a field using the global validator instance
func ValidateField(fieldName string, value interface{}, rules string) *errors.AppError {
	return globalValidator.ValidateField(fieldName, value, rules)
}

// RegisterCustomValidator registers a custom validator with the global instance
func RegisterCustomValidator(name string, fn ValidationFunc) {
	globalValidator.RegisterValidator(name, fn)
}