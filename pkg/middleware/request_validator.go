// Package middleware provides request validation functionality
package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// RequestValidator provides comprehensive request validation
type RequestValidator struct {
	config ValidationConfig
	logger logger.Logger
	
	// Pre-compiled regex patterns for performance
	sqlInjectionPatterns []*regexp.Regexp
	xssPatterns          []*regexp.Regexp
	pathTraversalPattern *regexp.Regexp
}

// ValidationConfig holds configuration for request validation
type ValidationConfig struct {
	// Size limits
	MaxRequestSize    int64         // Maximum request body size in bytes
	MaxHeaderSize     int           // Maximum header size in bytes
	MaxURILength      int           // Maximum URI length
	MaxQueryParams    int           // Maximum number of query parameters
	
	// Timeout settings
	RequestTimeout    time.Duration // Maximum request processing time
	
	// Content validation
	AllowedContentTypes []string      // Allowed Content-Type headers
	RequireContentType  bool          // Require Content-Type for POST/PUT/PATCH
	
	// Security validation
	EnableSQLInjectionCheck  bool // Enable SQL injection pattern detection
	EnableXSSCheck          bool // Enable XSS pattern detection
	EnablePathTraversalCheck bool // Enable path traversal detection
	
	// Header validation
	RequiredHeaders     []string // Headers that must be present
	ForbiddenHeaders    []string // Headers that are not allowed
	MaxHeaderCount      int      // Maximum number of headers allowed
	
	// Custom validation rules
	CustomRules []ValidationRule
	
	Logger logger.Logger
}

// ValidationRule defines a custom validation rule
type ValidationRule struct {
	Name        string
	Description string
	Check       func(*http.Request) error
	Enabled     bool
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// NewRequestValidator creates a new request validator
func NewRequestValidator(config ValidationConfig) *RequestValidator {
	// Set defaults
	if config.MaxRequestSize <= 0 {
		config.MaxRequestSize = 10 * 1024 * 1024 // 10MB default
	}
	if config.MaxHeaderSize <= 0 {
		config.MaxHeaderSize = 8192 // 8KB default
	}
	if config.MaxURILength <= 0 {
		config.MaxURILength = 2048 // 2KB default
	}
	if config.MaxQueryParams <= 0 {
		config.MaxQueryParams = 100 // 100 params default
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = 30 * time.Second // 30s default
	}
	if config.MaxHeaderCount <= 0 {
		config.MaxHeaderCount = 50 // 50 headers default
	}
	
	validator := &RequestValidator{
		config: config,
		logger: config.Logger,
	}
	
	// Pre-compile security patterns for performance
	if config.EnableSQLInjectionCheck {
		validator.compileSQLInjectionPatterns()
	}
	if config.EnableXSSCheck {
		validator.compileXSSPatterns()
	}
	if config.EnablePathTraversalCheck {
		validator.pathTraversalPattern = regexp.MustCompile(`\.\.[\\/]`)
	}
	
	return validator
}

// Middleware returns the request validation middleware
func (rv *RequestValidator) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			
			// Validate request
			if err := rv.ValidateRequest(r); err != nil {
				rv.handleValidationError(w, r, err, start)
				return
			}
			
			// Request is valid, proceed
			next.ServeHTTP(w, r)
		})
	}
}

// ValidateRequest performs comprehensive request validation
func (rv *RequestValidator) ValidateRequest(r *http.Request) error {
	// 1. Size validations
	if err := rv.validateRequestSize(r); err != nil {
		return err
	}
	
	// 2. URI validation
	if err := rv.validateURI(r); err != nil {
		return err
	}
	
	// 3. Header validation
	if err := rv.validateHeaders(r); err != nil {
		return err
	}
	
	// 4. Content-Type validation
	if err := rv.validateContentType(r); err != nil {
		return err
	}
	
	// 5. Security validation
	if err := rv.validateSecurity(r); err != nil {
		return err
	}
	
	// 6. Custom validation rules
	if err := rv.validateCustomRules(r); err != nil {
		return err
	}
	
	return nil
}

// validateRequestSize validates request body and header sizes
func (rv *RequestValidator) validateRequestSize(r *http.Request) error {
	// Check Content-Length header
	if contentLength := r.Header.Get("Content-Length"); contentLength != "" {
		if length, err := strconv.ParseInt(contentLength, 10, 64); err == nil {
			if length > rv.config.MaxRequestSize {
				return fmt.Errorf("request body too large: %d bytes (max: %d)", length, rv.config.MaxRequestSize)
			}
		}
	}
	
	// Calculate total header size
	totalHeaderSize := 0
	for name, values := range r.Header {
		for _, value := range values {
			totalHeaderSize += len(name) + len(value) + 4 // +4 for ": " and "\r\n"
		}
	}
	
	if totalHeaderSize > rv.config.MaxHeaderSize {
		return fmt.Errorf("headers too large: %d bytes (max: %d)", totalHeaderSize, rv.config.MaxHeaderSize)
	}
	
	return nil
}

// validateURI validates URI length and query parameters
func (rv *RequestValidator) validateURI(r *http.Request) error {
	// Check URI length
	if len(r.RequestURI) > rv.config.MaxURILength {
		return fmt.Errorf("URI too long: %d characters (max: %d)", len(r.RequestURI), rv.config.MaxURILength)
	}
	
	// Check query parameter count
	queryParams := r.URL.Query()
	if len(queryParams) > rv.config.MaxQueryParams {
		return fmt.Errorf("too many query parameters: %d (max: %d)", len(queryParams), rv.config.MaxQueryParams)
	}
	
	// Check for path traversal in URL
	if rv.config.EnablePathTraversalCheck && rv.pathTraversalPattern != nil {
		if rv.pathTraversalPattern.MatchString(r.URL.Path) {
			return fmt.Errorf("path traversal attempt detected in URL: %s", r.URL.Path)
		}
	}
	
	return nil
}

// validateHeaders validates HTTP headers
func (rv *RequestValidator) validateHeaders(r *http.Request) error {
	// Check header count
	if len(r.Header) > rv.config.MaxHeaderCount {
		return fmt.Errorf("too many headers: %d (max: %d)", len(r.Header), rv.config.MaxHeaderCount)
	}
	
	// Check required headers
	for _, requiredHeader := range rv.config.RequiredHeaders {
		if r.Header.Get(requiredHeader) == "" {
			return fmt.Errorf("missing required header: %s", requiredHeader)
		}
	}
	
	// Check forbidden headers
	for _, forbiddenHeader := range rv.config.ForbiddenHeaders {
		if r.Header.Get(forbiddenHeader) != "" {
			return fmt.Errorf("forbidden header present: %s", forbiddenHeader)
		}
	}
	
	return nil
}

// validateContentType validates Content-Type header
func (rv *RequestValidator) validateContentType(r *http.Request) error {
	// Check if Content-Type is required for this method
	if rv.config.RequireContentType {
		method := strings.ToUpper(r.Method)
		if method == "POST" || method == "PUT" || method == "PATCH" {
			contentType := r.Header.Get("Content-Type")
			if contentType == "" {
				return fmt.Errorf("Content-Type header required for %s requests", method)
			}
			
			// Validate against allowed content types
			if len(rv.config.AllowedContentTypes) > 0 {
				allowed := false
				for _, allowedType := range rv.config.AllowedContentTypes {
					if strings.HasPrefix(contentType, allowedType) {
						allowed = true
						break
					}
				}
				if !allowed {
					return fmt.Errorf("unsupported Content-Type: %s", contentType)
				}
			}
		}
	}
	
	return nil
}

// validateSecurity performs security-related validations
func (rv *RequestValidator) validateSecurity(r *http.Request) error {
	// Read request body if present for content validation
	var body []byte
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, rv.config.MaxRequestSize))
		if err != nil {
			return fmt.Errorf("failed to read request body: %w", err)
		}
		// Restore the body for later use
		r.Body = io.NopCloser(bytes.NewBuffer(body))
	}
	
	// Combine all text content for security checks
	var contentToCheck strings.Builder
	
	// Add URL path and query parameters
	contentToCheck.WriteString(r.URL.Path)
	contentToCheck.WriteString(" ")
	contentToCheck.WriteString(r.URL.RawQuery)
	contentToCheck.WriteString(" ")
	
	// Add request body
	contentToCheck.Write(body)
	
	content := contentToCheck.String()
	
	// SQL Injection check
	if rv.config.EnableSQLInjectionCheck {
		for _, pattern := range rv.sqlInjectionPatterns {
			if pattern.MatchString(content) {
				return fmt.Errorf("potential SQL injection detected")
			}
		}
	}
	
	// XSS check
	if rv.config.EnableXSSCheck {
		for _, pattern := range rv.xssPatterns {
			if pattern.MatchString(content) {
				return fmt.Errorf("potential XSS attempt detected")
			}
		}
	}
	
	return nil
}

// validateCustomRules applies custom validation rules
func (rv *RequestValidator) validateCustomRules(r *http.Request) error {
	for _, rule := range rv.config.CustomRules {
		if rule.Enabled && rule.Check != nil {
			if err := rule.Check(r); err != nil {
				return fmt.Errorf("custom rule '%s' failed: %w", rule.Name, err)
			}
		}
	}
	
	return nil
}

// compileSQLInjectionPatterns compiles SQL injection detection patterns
func (rv *RequestValidator) compileSQLInjectionPatterns() {
	patterns := []string{
		`(?i)(union\s+select|select\s+.*\s+from|insert\s+into|update\s+.*\s+set|delete\s+from)`,
		`(?i)(or\s+1\s*=\s*1|and\s+1\s*=\s*1|'.*or.*'.*=.*')`,
		`(?i)(exec\s*\(|execute\s*\(|sp_executesql)`,
		`(?i)(--|\#|\/\*|\*\/)`,
		`(?i)(drop\s+table|alter\s+table|create\s+table)`,
		`(?i)(xp_cmdshell|sp_makewebtask|xp_regwrite)`,
	}
	
	for _, pattern := range patterns {
		if compiled, err := regexp.Compile(pattern); err == nil {
			rv.sqlInjectionPatterns = append(rv.sqlInjectionPatterns, compiled)
		}
	}
}

// compileXSSPatterns compiles XSS detection patterns
func (rv *RequestValidator) compileXSSPatterns() {
	patterns := []string{
		`(?i)<script[^>]*>.*?</script>`,
		`(?i)<iframe[^>]*>.*?</iframe>`,
		`(?i)javascript:`,
		`(?i)vbscript:`,
		`(?i)onload\s*=`,
		`(?i)onerror\s*=`,
		`(?i)onclick\s*=`,
		`(?i)onmouseover\s*=`,
		`(?i)<img[^>]*src[^>]*=.*?javascript:`,
		`(?i)eval\s*\(`,
		`(?i)expression\s*\(`,
	}
	
	for _, pattern := range patterns {
		if compiled, err := regexp.Compile(pattern); err == nil {
			rv.xssPatterns = append(rv.xssPatterns, compiled)
		}
	}
}

// handleValidationError handles validation errors
func (rv *RequestValidator) handleValidationError(w http.ResponseWriter, r *http.Request, err error, start time.Time) {
	// Log validation error
	rv.logger.Warn("Request validation failed",
		logger.String("error", err.Error()),
		logger.String("method", r.Method),
		logger.String("path", r.URL.Path),
		logger.String("client_ip", extractClientIP(r)),
		logger.String("user_agent", r.Header.Get("User-Agent")),
		logger.Duration("duration", time.Since(start)))
	
	// Set security headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	
	// Return error response
	w.WriteHeader(http.StatusBadRequest)
	
	response := ValidationError{
		Field:   "request",
		Rule:    "validation",
		Message: err.Error(),
	}
	
	if jsonBytes, err := json.Marshal(response); err == nil {
		w.Write(jsonBytes)
	} else {
		w.Write([]byte(`{"error":"Request validation failed"}`))
	}
}

// extractClientIP extracts client IP from request
func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}
	
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	
	clientIP := r.RemoteAddr
	if colonIndex := strings.LastIndex(clientIP, ":"); colonIndex != -1 {
		clientIP = clientIP[:colonIndex]
	}
	
	return clientIP
}

// CreateJSONSchemaValidator creates a validation rule for JSON schema validation
func CreateJSONSchemaValidator(name string, schema map[string]interface{}) ValidationRule {
	return ValidationRule{
		Name:        name,
		Description: "JSON schema validation",
		Enabled:     true,
		Check: func(r *http.Request) error {
			if r.Header.Get("Content-Type") != "application/json" {
				return nil // Skip if not JSON
			}
			
			// Read body
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return fmt.Errorf("failed to read request body")
			}
			
			// Restore body
			r.Body = io.NopCloser(bytes.NewBuffer(body))
			
			// Parse JSON
			var data interface{}
			if err := json.Unmarshal(body, &data); err != nil {
				return fmt.Errorf("invalid JSON format")
			}
			
			// Basic schema validation would go here
			// In a real implementation, you'd use a proper JSON schema library
			
			return nil
		},
	}
}

// CreateIPWhitelistValidator creates a validation rule for IP whitelisting
func CreateIPWhitelistValidator(allowedIPs []string) ValidationRule {
	allowedSet := make(map[string]bool)
	for _, ip := range allowedIPs {
		allowedSet[ip] = true
	}
	
	return ValidationRule{
		Name:        "ip_whitelist",
		Description: "IP address whitelist validation",
		Enabled:     true,
		Check: func(r *http.Request) error {
			clientIP := extractClientIP(r)
			if !allowedSet[clientIP] {
				return fmt.Errorf("IP address %s not allowed", clientIP)
			}
			return nil
		},
	}
}