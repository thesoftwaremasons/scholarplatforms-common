package response

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/errors"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// SuccessResponse represents a successful API response
type SuccessResponse struct {
	Success   bool        `json:"success"`
	Data      interface{} `json:"data,omitempty"`
	Message   string      `json:"message,omitempty"`
	Timestamp string      `json:"timestamp"`
	TraceID   string      `json:"trace_id,omitempty"`
}

// ErrorResponse represents an error API response
type ErrorResponse struct {
	Success   bool        `json:"success"`
	Error     ErrorDetail `json:"error"`
	Timestamp string      `json:"timestamp"`
	TraceID   string      `json:"trace_id,omitempty"`
}

// ErrorDetail contains error information
type ErrorDetail struct {
	Code    errors.ErrorCode       `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// PaginationMeta contains pagination metadata
type PaginationMeta struct {
	Page       int  `json:"page"`
	PerPage    int  `json:"per_page"`
	Total      int  `json:"total"`
	TotalPages int  `json:"total_pages"`
	HasNext    bool `json:"has_next"`
	HasPrev    bool `json:"has_prev"`
}

// PaginatedResponse represents a paginated API response
type PaginatedResponse struct {
	Success    bool           `json:"success"`
	Data       interface{}    `json:"data"`
	Pagination PaginationMeta `json:"pagination"`
	Timestamp  string         `json:"timestamp"`
	TraceID    string         `json:"trace_id,omitempty"`
}

// ResponseWriter handles HTTP response writing
type ResponseWriter struct {
	logger logger.Logger
}

// NewResponseWriter creates a new response writer
func NewResponseWriter(logger logger.Logger) *ResponseWriter {
	return &ResponseWriter{
		logger: logger,
	}
}

// WriteSuccess writes a successful response
func (rw *ResponseWriter) WriteSuccess(w http.ResponseWriter, data interface{}, message string, traceID string) {
	response := SuccessResponse{
		Success:   true,
		Data:      data,
		Message:   message,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		TraceID:   traceID,
	}

	rw.writeJSON(w, http.StatusOK, response)
}

// WriteCreated writes a created response (201)
func (rw *ResponseWriter) WriteCreated(w http.ResponseWriter, data interface{}, message string, traceID string) {
	response := SuccessResponse{
		Success:   true,
		Data:      data,
		Message:   message,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		TraceID:   traceID,
	}

	rw.writeJSON(w, http.StatusCreated, response)
}

// WriteNoContent writes a no content response (204)
func (rw *ResponseWriter) WriteNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// WritePaginated writes a paginated response
func (rw *ResponseWriter) WritePaginated(w http.ResponseWriter, data interface{}, pagination PaginationMeta, traceID string) {
	response := PaginatedResponse{
		Success:    true,
		Data:       data,
		Pagination: pagination,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		TraceID:    traceID,
	}

	rw.writeJSON(w, http.StatusOK, response)
}

// WriteError writes an error response
func (rw *ResponseWriter) WriteError(w http.ResponseWriter, err error, traceID string) {
	var appErr *errors.AppError
	var statusCode int
	var errorDetail ErrorDetail

	if errors.IsAppError(err) {
		appErr, _ = errors.AsAppError(err)
		statusCode = appErr.StatusCode
		errorDetail = ErrorDetail{
			Code:    appErr.Code,
			Message: appErr.Message,
			Details: appErr.Details,
		}

		// Log error details for internal errors
		if rw.logger != nil && statusCode >= 500 {
			rw.logger.Error("Internal server error",
				logger.ErrorField(err),
				logger.String("trace_id", traceID),
				logger.String("code", string(appErr.Code)),
			)
		} else if rw.logger != nil {
			rw.logger.Warn("Client error",
				logger.ErrorField(err),
				logger.String("trace_id", traceID),
				logger.String("code", string(appErr.Code)),
				logger.Int("status_code", statusCode),
			)
		}
	} else {
		// Generic error
		statusCode = http.StatusInternalServerError
		errorDetail = ErrorDetail{
			Code:    errors.ErrCodeInternalError,
			Message: "An internal error occurred",
		}

		if rw.logger != nil {
			rw.logger.Error("Unexpected error",
				logger.ErrorField(err),
				logger.String("trace_id", traceID),
			)
		}
	}

	response := ErrorResponse{
		Success:   false,
		Error:     errorDetail,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		TraceID:   traceID,
	}

	rw.writeJSON(w, statusCode, response)
}

// writeJSON writes a JSON response
func (rw *ResponseWriter) writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		if rw.logger != nil {
			rw.logger.Error("Failed to encode JSON response", logger.ErrorField(err))
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// Convenience functions for quick responses

// Success writes a success response
func Success(w http.ResponseWriter, data interface{}, message string) {
	rw := &ResponseWriter{}
	rw.WriteSuccess(w, data, message, "")
}

// Created writes a created response
func Created(w http.ResponseWriter, data interface{}, message string) {
	rw := &ResponseWriter{}
	rw.WriteCreated(w, data, message, "")
}

// NoContent writes a no content response
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Error writes an error response
func Error(w http.ResponseWriter, err error) {
	rw := &ResponseWriter{}
	rw.WriteError(w, err, "")
}

// BadRequest writes a bad request error
func BadRequest(w http.ResponseWriter, message string) {
	err := errors.NewValidationError(message)
	Error(w, err)
}

// Unauthorized writes an unauthorized error
func Unauthorized(w http.ResponseWriter, message string) {
	err := errors.NewUnauthorizedError(message)
	Error(w, err)
}

// Forbidden writes a forbidden error
func Forbidden(w http.ResponseWriter, message string) {
	err := errors.NewForbiddenError(message)
	Error(w, err)
}

// NotFound writes a not found error
func NotFound(w http.ResponseWriter, resource string, id string) {
	err := errors.NewNotFoundError(resource, id)
	Error(w, err)
}

// Conflict writes a conflict error
func Conflict(w http.ResponseWriter, message string) {
	err := errors.NewConflictError(message)
	Error(w, err)
}

// InternalError writes an internal server error
func InternalError(w http.ResponseWriter, message string) {
	err := errors.NewInternalError(message)
	Error(w, err)
}
