package middleware

import (
	"context"
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

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				s.logger.Warn("Missing Authorization header for service authentication")
				http.Error(w, "Service authorization required", http.StatusUnauthorized)
				return
			}

			token := s.extractTokenFromHeader(authHeader)
			if token == "" {
				s.logger.Warn("Invalid Authorization header format for service authentication")
				http.Error(w, "Bearer token required", http.StatusUnauthorized)
				return
			}

			serviceCtx, err := s.tokenValidator.ValidateServiceToken(r.Context(), token)
			if err != nil {
				s.logger.Warn("Service token validation failed", logger.ErrorField(err))
				http.Error(w, "Invalid service token", http.StatusUnauthorized)
				return
			}

			// Add service context to request context
			ctx := s.addServiceContextToContext(r.Context(), serviceCtx)
			
			// If service context includes user context, add it as well
			if serviceCtx.UserContext != nil {
				ctx = s.addUserContextToContext(ctx, serviceCtx.UserContext)
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

// GetServiceContext retrieves service context from the request context
func GetServiceContext(ctx context.Context) *ServiceContext {
	if serviceCtx, ok := ctx.Value(serviceContextKey).(*ServiceContext); ok {
		return serviceCtx
	}
	return nil
}

// SimpleServiceTokenValidator is a basic implementation of ServiceTokenValidator
type SimpleServiceTokenValidator struct {
	endpoint  string
	projectID string
	enabled   bool
}

// NewSimpleServiceTokenValidator creates a simple service token validator
func NewSimpleServiceTokenValidator(endpoint, projectID string) *SimpleServiceTokenValidator {
	return &SimpleServiceTokenValidator{
		endpoint:  endpoint,
		projectID: projectID,
		enabled:   endpoint != "" && projectID != "",
	}
}

// ValidateServiceToken validates a service token
func (v *SimpleServiceTokenValidator) ValidateServiceToken(ctx context.Context, token string) (*ServiceContext, error) {
	// This is a placeholder implementation
	// In a real implementation, you would validate the token against Zitadel
	// For now, just return a basic service context
	return &ServiceContext{
		ServiceName: "notification-service",
		Claims:      map[string]interface{}{"token": token},
	}, nil
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