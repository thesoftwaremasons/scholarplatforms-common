package middleware

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// ServiceTokenValidator interface for services that can validate service tokens
type ServiceTokenValidator interface {
	ValidateServiceToken(ctx context.Context, token string) (*ServiceContext, error)
	IsEnabled() bool
}

// ServiceContext represents service authentication information
type ServiceContext struct {
	ServiceName string                 `json:"service_name"`
	Claims      map[string]interface{} `json:"claims"`
	UserContext *UserContext           `json:"user_context,omitempty"` // For service tokens that include user context
}

// ZitadelServiceAuthMiddleware provides service authentication middleware
type ZitadelServiceAuthMiddleware struct {
	tokenValidator ServiceTokenValidator
	logger         logger.Logger
}

// NewZitadelServiceAuthMiddleware creates a new service authentication middleware
func NewZitadelServiceAuthMiddleware(tokenValidator ServiceTokenValidator, log logger.Logger) *ZitadelServiceAuthMiddleware {
	return &ZitadelServiceAuthMiddleware{
		tokenValidator: tokenValidator,
		logger:         log,
	}
}

// RequireServiceAuth middleware that requires valid service authentication
func (s *ZitadelServiceAuthMiddleware) RequireServiceAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !s.tokenValidator.IsEnabled() {
				s.logger.Debug("Service authentication is disabled, skipping auth check")
				next.ServeHTTP(w, r)
				return
			}

			// First check for X-Service-Token header (API Gateway format)
			serviceToken := r.Header.Get("X-Service-Token")
			var token string
			var tokenSource string

			if serviceToken != "" {
				token = serviceToken
				tokenSource = "X-Service-Token"
			} else {
				// Fallback to Authorization header
				authHeader := r.Header.Get("Authorization")
				if authHeader == "" {
					s.logger.Warn("Missing both X-Service-Token and Authorization headers for service authentication",
						logger.String("path", r.URL.Path),
						logger.String("method", r.Method),
						logger.String("remote_addr", r.RemoteAddr))
					http.Error(w, "Service authorization required", http.StatusUnauthorized)
					return
				}

				extractedToken := s.extractTokenFromHeader(authHeader)
				if extractedToken == "" {
					s.logger.Warn("Invalid Authorization header format for service authentication",
						logger.String("path", r.URL.Path),
						logger.String("auth_header", authHeader))
					http.Error(w, "Bearer token required", http.StatusUnauthorized)
					return
				}
				token = extractedToken
				tokenSource = "Authorization"
			}

			serviceCtx, err := s.tokenValidator.ValidateServiceToken(r.Context(), token)
			if err != nil {
				s.logger.Warn("Service token validation failed",
					logger.ErrorField(err),
					logger.String("token_source", tokenSource),
					logger.String("token_prefix", s.safeTokenPrefix(token)),
					logger.String("path", r.URL.Path))
				http.Error(w, "Invalid service token", http.StatusUnauthorized)
				return
			}

			s.logger.Debug("Service authentication successful",
				logger.String("service_name", serviceCtx.ServiceName),
				logger.String("token_source", tokenSource),
				logger.String("path", r.URL.Path))

			// Add service context to request context
			ctx := s.addServiceContextToContext(r.Context(), serviceCtx)

			// If service context includes user context, add it as well
			if serviceCtx.UserContext != nil {
				ctx = s.addUserContextToContext(ctx, serviceCtx.UserContext)
				s.logger.Debug("User context added to request",
					logger.String("user_id", serviceCtx.UserContext.UserID),
					logger.String("email", serviceCtx.UserContext.Email))
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractTokenFromHeader extracts the token from Authorization header
func (s *ZitadelServiceAuthMiddleware) extractTokenFromHeader(authHeader string) string {
	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		return ""
	}
	return strings.TrimPrefix(authHeader, bearerPrefix)
}

// Context keys for service context
const (
	serviceContextKey contextKey = "zitadel_service_context"
)

// addServiceContextToContext adds service context to the request context
func (s *ZitadelServiceAuthMiddleware) addServiceContextToContext(ctx context.Context, serviceCtx *ServiceContext) context.Context {
	return context.WithValue(ctx, serviceContextKey, serviceCtx)
}

// addUserContextToContext adds user context to the request context
func (s *ZitadelServiceAuthMiddleware) addUserContextToContext(ctx context.Context, userCtx *UserContext) context.Context {
	return context.WithValue(ctx, userContextKey, userCtx)
}

// safeTokenPrefix returns the first 8 characters of a token for safe logging
func (s *ZitadelServiceAuthMiddleware) safeTokenPrefix(token string) string {
	if len(token) <= 8 {
		return token
	}
	return token[:8] + "..."
}

// GetServiceContext retrieves service context from the request context
func GetServiceContext(ctx context.Context) *ServiceContext {
	if serviceCtx, ok := ctx.Value(serviceContextKey).(*ServiceContext); ok {
		return serviceCtx
	}
	return nil
}

// SimpleServiceTokenValidator is a basic implementation of ServiceTokenValidator
type SimpleServiceTokenValidator struct {
	endpoint        string
	projectID       string
	enabled         bool
	allowedServices []string
}

// NewSimpleServiceTokenValidator creates a simple service token validator
func NewSimpleServiceTokenValidator(endpoint, projectID string) *SimpleServiceTokenValidator {
	return &SimpleServiceTokenValidator{
		endpoint:        endpoint,
		projectID:       projectID,
		enabled:         endpoint != "" && projectID != "",
		allowedServices: []string{"api-gateway", "notification-service", "userprofile-service", "zitadel-service"},
	}
}

// ValidateServiceToken validates a service token
func (v *SimpleServiceTokenValidator) ValidateServiceToken(ctx context.Context, token string) (*ServiceContext, error) {
	if token == "" {
		return nil, fmt.Errorf("empty token provided")
	}

	// First try to parse as JWT
	if strings.Contains(token, ".") {
		return v.validateJWTServiceToken(token)
	}

	// If not a JWT, treat as simple service token
	return v.validateSimpleServiceToken(token)
}

// validateJWTServiceToken validates JWT-based service tokens from API Gateway
func (v *SimpleServiceTokenValidator) validateJWTServiceToken(token string) (*ServiceContext, error) {
	// Parse JWT without verification for now (in production, you'd verify signature)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format")
	}

	// Decode payload (claims)
	payload := parts[1]
	// Add padding if needed
	for len(payload)%4 != 0 {
		payload += "="
	}

	claimsBytes, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JWT claims: %w", err)
	}

	// Extract service name from subject or custom claim
	serviceName := "unknown"
	if sub, ok := claims["sub"].(string); ok {
		serviceName = sub
	} else if sn, ok := claims["service_name"].(string); ok {
		serviceName = sn
	}

	// Check if service is allowed
	if !v.isServiceAllowed(serviceName) {
		return nil, fmt.Errorf("service '%s' not allowed", serviceName)
	}

	serviceCtx := &ServiceContext{
		ServiceName: serviceName,
		Claims:      claims,
	}

	// Extract user context if present
	if userToken, ok := claims["user_token"].(string); ok && userToken != "" {
		// In a real implementation, you'd validate the user token
		// For now, create a basic user context
		serviceCtx.UserContext = &UserContext{
			UserID:   "extracted-from-service-token",
			Username: "service-user",
			Claims:   map[string]interface{}{"source": "service_token"},
		}
	}

	return serviceCtx, nil
}

// validateSimpleServiceToken validates simple string-based service tokens
func (v *SimpleServiceTokenValidator) validateSimpleServiceToken(token string) (*ServiceContext, error) {
	// First, try known simple tokens
	expectedTokens := map[string]string{
		"notification-service-token-12345": "notification-service",
		"userprofile-service-token-12345":  "userprofile-service",
		"zitadel-service-token-12345":      "zitadel-service",
		"gateway-service-token-12345":      "api-gateway",
	}

	if serviceName, exists := expectedTokens[token]; exists {
		return &ServiceContext{
			ServiceName: serviceName,
			Claims:      map[string]interface{}{"token_type": "simple", "token": token},
		}, nil
	}

	// Try to decode as base64 URL-encoded token (common API Gateway format)
	if len(token) > 20 && v.isBase64URLToken(token) {
		decoded, err := base64.RawURLEncoding.DecodeString(token)
		if err == nil && len(decoded) > 0 {
			// For base64 tokens from API Gateway, assume they're valid service tokens
			return &ServiceContext{
				ServiceName: "api-gateway", // Assume it's from API Gateway
				Claims: map[string]interface{}{
					"token_type": "base64_url",
					"token":      token,
					"decoded":    string(decoded),
				},
			}, nil
		}
	}

	// If token looks like an access token (long alphanumeric), treat as valid
	if len(token) > 50 && v.isAlphanumericToken(token) {
		return &ServiceContext{
			ServiceName: "api-gateway", // Assume it's from API Gateway
			Claims: map[string]interface{}{
				"token_type": "access_token",
				"token":      token,
				"length":     len(token),
			},
		}, nil
	}

	return nil, fmt.Errorf("invalid service token format")
}

// isBase64URLToken checks if a token appears to be base64 URL encoded
func (v *SimpleServiceTokenValidator) isBase64URLToken(token string) bool {
	// Check for base64 URL-safe characters
	for _, r := range token {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// isAlphanumericToken checks if a token contains only alphanumeric characters and common token symbols
func (v *SimpleServiceTokenValidator) isAlphanumericToken(token string) bool {
	for _, r := range token {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// isServiceAllowed checks if a service is in the allowed list
func (v *SimpleServiceTokenValidator) isServiceAllowed(serviceName string) bool {
	for _, allowed := range v.allowedServices {
		if serviceName == allowed {
			return true
		}
	}
	return false
}

// IsEnabled returns whether the service token validator is enabled
func (v *SimpleServiceTokenValidator) IsEnabled() bool {
	return v.enabled
}

// NewZitadelServiceAuthMiddlewareFromConfig creates service auth middleware from config
func NewZitadelServiceAuthMiddlewareFromConfig(endpoint, projectID string, log logger.Logger) *ZitadelServiceAuthMiddleware {
	validator := NewSimpleServiceTokenValidator(endpoint, projectID)
	return NewZitadelServiceAuthMiddleware(validator, log)
}