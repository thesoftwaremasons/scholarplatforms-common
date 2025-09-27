package errors

import (
	"fmt"
	"net/http"
)

// ErrorCode represents standardized error codes
type ErrorCode string

const (
	// Authentication and Authorization
	ErrCodeUnauthorized         ErrorCode = "UNAUTHORIZED"
	ErrCodeForbidden           ErrorCode = "FORBIDDEN"
	ErrCodeInvalidCredentials  ErrorCode = "INVALID_CREDENTIALS"
	ErrCodeTokenExpired        ErrorCode = "TOKEN_EXPIRED"
	ErrCodeInvalidToken        ErrorCode = "INVALID_TOKEN"
	
	// Validation
	ErrCodeValidationFailed    ErrorCode = "VALIDATION_FAILED"
	ErrCodeInvalidInput        ErrorCode = "INVALID_INPUT"
	ErrCodeMissingField        ErrorCode = "MISSING_FIELD"
	ErrCodeInvalidFormat       ErrorCode = "INVALID_FORMAT"
	
	// Resources
	ErrCodeNotFound           ErrorCode = "NOT_FOUND"
	ErrCodeAlreadyExists      ErrorCode = "ALREADY_EXISTS"
	ErrCodeConflict           ErrorCode = "CONFLICT"
	
	// System
	ErrCodeInternalError      ErrorCode = "INTERNAL_ERROR"
	ErrCodeServiceUnavailable ErrorCode = "SERVICE_UNAVAILABLE"
	ErrCodeTimeout            ErrorCode = "TIMEOUT"
	ErrCodeRateLimitExceeded  ErrorCode = "RATE_LIMIT_EXCEEDED"
	
	// Business Logic
	ErrCodeBusinessRule       ErrorCode = "BUSINESS_RULE_VIOLATION"
	ErrCodeInsufficientFunds  ErrorCode = "INSUFFICIENT_FUNDS"
	ErrCodeQuotaExceeded      ErrorCode = "QUOTA_EXCEEDED"
)

// AppError represents a structured application error
type AppError struct {
	Code       ErrorCode              `json:"code"`
	Message    string                 `json:"message"`
	StatusCode int                    `json:"-"`
	Details    map[string]interface{} `json:"details,omitempty"`
	Cause      error                  `json:"-"`
	TraceID    string                 `json:"trace_id,omitempty"`
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (caused by: %s)", e.Code, e.Message, e.Cause.Error())
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause error
func (e *AppError) Unwrap() error {
	return e.Cause
}

// WithDetails adds details to the error
func (e *AppError) WithDetails(details map[string]interface{}) *AppError {
	if e.Details == nil {
		e.Details = make(map[string]interface{})
	}
	for k, v := range details {
		e.Details[k] = v
	}
	return e
}

// WithDetail adds a single detail to the error
func (e *AppError) WithDetail(key string, value interface{}) *AppError {
	if e.Details == nil {
		e.Details = make(map[string]interface{})
	}
	e.Details[key] = value
	return e
}

// WithTraceID adds a trace ID to the error
func (e *AppError) WithTraceID(traceID string) *AppError {
	e.TraceID = traceID
	return e
}

// WithCause adds an underlying cause error
func (e *AppError) WithCause(cause error) *AppError {
	e.Cause = cause
	return e
}

// Error constructors

// NewAppError creates a new application error
func NewAppError(code ErrorCode, message string, statusCode int) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		StatusCode: statusCode,
		Details:    make(map[string]interface{}),
	}
}

// Authentication errors
func NewUnauthorizedError(message string) *AppError {
	if message == "" {
		message = "Authentication required"
	}
	return NewAppError(ErrCodeUnauthorized, message, http.StatusUnauthorized)
}

func NewForbiddenError(message string) *AppError {
	if message == "" {
		message = "Access forbidden"
	}
	return NewAppError(ErrCodeForbidden, message, http.StatusForbidden)
}

func NewInvalidCredentialsError() *AppError {
	return NewAppError(ErrCodeInvalidCredentials, "Invalid credentials", http.StatusUnauthorized)
}

func NewTokenExpiredError() *AppError {
	return NewAppError(ErrCodeTokenExpired, "Token has expired", http.StatusUnauthorized)
}

func NewInvalidTokenError() *AppError {
	return NewAppError(ErrCodeInvalidToken, "Invalid token", http.StatusUnauthorized)
}

// Validation errors
func NewValidationError(message string) *AppError {
	if message == "" {
		message = "Validation failed"
	}
	return NewAppError(ErrCodeValidationFailed, message, http.StatusBadRequest)
}

func NewInvalidInputError(field string, reason string) *AppError {
	err := NewAppError(ErrCodeInvalidInput, fmt.Sprintf("Invalid input for field '%s'", field), http.StatusBadRequest)
	return err.WithDetail("field", field).WithDetail("reason", reason)
}

func NewMissingFieldError(field string) *AppError {
	err := NewAppError(ErrCodeMissingField, fmt.Sprintf("Missing required field '%s'", field), http.StatusBadRequest)
	return err.WithDetail("field", field)
}

func NewInvalidFormatError(field string, expectedFormat string) *AppError {
	err := NewAppError(ErrCodeInvalidFormat, fmt.Sprintf("Invalid format for field '%s'", field), http.StatusBadRequest)
	return err.WithDetail("field", field).WithDetail("expected_format", expectedFormat)
}

// Resource errors
func NewNotFoundError(resource string, id string) *AppError {
	message := fmt.Sprintf("%s not found", resource)
	if id != "" {
		message = fmt.Sprintf("%s with ID '%s' not found", resource, id)
	}
	err := NewAppError(ErrCodeNotFound, message, http.StatusNotFound)
	return err.WithDetail("resource", resource).WithDetail("id", id)
}

func NewAlreadyExistsError(resource string, identifier string) *AppError {
	message := fmt.Sprintf("%s already exists", resource)
	if identifier != "" {
		message = fmt.Sprintf("%s with identifier '%s' already exists", resource, identifier)
	}
	err := NewAppError(ErrCodeAlreadyExists, message, http.StatusConflict)
	return err.WithDetail("resource", resource).WithDetail("identifier", identifier)
}

func NewConflictError(message string) *AppError {
	if message == "" {
		message = "Conflict occurred"
	}
	return NewAppError(ErrCodeConflict, message, http.StatusConflict)
}

// System errors
func NewInternalError(message string) *AppError {
	if message == "" {
		message = "Internal server error"
	}
	return NewAppError(ErrCodeInternalError, message, http.StatusInternalServerError)
}

func NewServiceUnavailableError(service string) *AppError {
	message := "Service unavailable"
	if service != "" {
		message = fmt.Sprintf("Service '%s' unavailable", service)
	}
	err := NewAppError(ErrCodeServiceUnavailable, message, http.StatusServiceUnavailable)
	return err.WithDetail("service", service)
}

func NewTimeoutError(operation string) *AppError {
	message := "Operation timed out"
	if operation != "" {
		message = fmt.Sprintf("Operation '%s' timed out", operation)
	}
	err := NewAppError(ErrCodeTimeout, message, http.StatusRequestTimeout)
	return err.WithDetail("operation", operation)
}

func NewRateLimitExceededError(limit int, window string) *AppError {
	message := "Rate limit exceeded"
	err := NewAppError(ErrCodeRateLimitExceeded, message, http.StatusTooManyRequests)
	return err.WithDetail("limit", limit).WithDetail("window", window)
}

// Business logic errors
func NewBusinessRuleError(message string) *AppError {
	if message == "" {
		message = "Business rule violation"
	}
	return NewAppError(ErrCodeBusinessRule, message, http.StatusUnprocessableEntity)
}

func NewInsufficientFundsError(available float64, required float64) *AppError {
	err := NewAppError(ErrCodeInsufficientFunds, "Insufficient funds", http.StatusUnprocessableEntity)
	return err.WithDetail("available", available).WithDetail("required", required)
}

func NewQuotaExceededError(quotaType string, limit int, current int) *AppError {
	message := fmt.Sprintf("%s quota exceeded", quotaType)
	err := NewAppError(ErrCodeQuotaExceeded, message, http.StatusUnprocessableEntity)
	return err.WithDetail("quota_type", quotaType).WithDetail("limit", limit).WithDetail("current", current)
}

// IsAppError checks if an error is an AppError
func IsAppError(err error) bool {
	_, ok := err.(*AppError)
	return ok
}

// AsAppError converts an error to AppError if possible
func AsAppError(err error) (*AppError, bool) {
	if appErr, ok := err.(*AppError); ok {
		return appErr, true
	}
	return nil, false
}

// WrapError wraps a generic error into an AppError
func WrapError(err error, code ErrorCode, message string, statusCode int) *AppError {
	if appErr, ok := AsAppError(err); ok {
		return appErr
	}
	
	return NewAppError(code, message, statusCode).WithCause(err)
}

// GetHTTPStatus returns the appropriate HTTP status code for the error
func GetHTTPStatus(err error) int {
	if appErr, ok := AsAppError(err); ok {
		return appErr.StatusCode
	}
	return http.StatusInternalServerError
}