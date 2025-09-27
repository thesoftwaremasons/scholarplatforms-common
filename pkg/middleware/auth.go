package middleware

import (
	"net/http"
	"strings"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/auth"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// AuthMiddleware provides authentication middleware
type AuthMiddleware struct {
	jwtManager *auth.JWTManager
	logger     logger.Logger
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(jwtManager *auth.JWTManager, log logger.Logger) *AuthMiddleware {
	return &AuthMiddleware{
		jwtManager: jwtManager,
		logger:     log,
	}
}

// RequireAuth middleware that requires valid JWT authentication
func (a *AuthMiddleware) RequireAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				a.logger.Warn("Missing Authorization header")
				http.Error(w, "Authorization header required", http.StatusUnauthorized)
				return
			}

			token := auth.ExtractTokenFromHeader(authHeader)
			if token == "" {
				a.logger.Warn("Invalid Authorization header format")
				http.Error(w, "Bearer token required", http.StatusUnauthorized)
				return
			}

			claims, err := a.jwtManager.ValidateToken(token)
			if err != nil {
				a.logger.Warn("Token validation failed", logger.ErrorField(err))
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			// Add claims to context
			ctx := auth.AddClaimsToContext(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole middleware that requires specific role
func (a *AuthMiddleware) RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return a.RequireAuth()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.GetClaimsFromContext(r.Context())
			if !ok {
				a.logger.Error("Claims not found in context")
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}

			if !a.hasRole(claims.Role, role) {
				a.logger.Warn("Insufficient role",
					logger.String("required", role),
					logger.String("actual", claims.Role),
				)
				http.Error(w, "Insufficient permissions", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		}))
	}
}

// RequireServiceAuth middleware for service-to-service authentication
func (a *AuthMiddleware) RequireServiceAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				a.logger.Warn("Missing Authorization header for service call")
				http.Error(w, "Authorization header required", http.StatusUnauthorized)
				return
			}

			token := auth.ExtractTokenFromHeader(authHeader)
			if token == "" {
				a.logger.Warn("Invalid Authorization header format for service call")
				http.Error(w, "Bearer token required", http.StatusUnauthorized)
				return
			}

			claims, err := a.jwtManager.ValidateToken(token)
			if err != nil {
				a.logger.Warn("Service token validation failed", logger.ErrorField(err))
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			// Check if it's a service token
			if claims.ServiceID == "" {
				a.logger.Warn("Not a service token")
				http.Error(w, "Service token required", http.StatusForbidden)
				return
			}

			// Add claims to context
			ctx := auth.AddClaimsToContext(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// hasRole checks if the user has the required role
func (a *AuthMiddleware) hasRole(userRole, requiredRole string) bool {
	// Simple role hierarchy: admin > moderator > user
	roles := map[string]int{
		"admin":     3,
		"moderator": 2,
		"user":      1,
	}

	userLevel, userExists := roles[strings.ToLower(userRole)]
	requiredLevel, requiredExists := roles[strings.ToLower(requiredRole)]

	if !userExists || !requiredExists {
		return userRole == requiredRole
	}

	return userLevel >= requiredLevel
}
