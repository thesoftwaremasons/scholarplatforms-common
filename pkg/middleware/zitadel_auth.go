package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// ZitadelTokenValidator interface for services that can validate Zitadel tokens
type ZitadelTokenValidator interface {
	ValidateToken(ctx context.Context, token string) (interface{}, error)
	IsEnabled() bool
}

// UserContext represents authenticated user information
type UserContext struct {
	UserID    string                 `json:"user_id"`
	Username  string                 `json:"username"`
	Email     string                 `json:"email"`
	Roles     []string               `json:"roles"`
	Claims    map[string]interface{} `json:"claims"`
	AuthToken string                 `json:"-"` // Raw token for downstream calls
}

// ZitadelAuthMiddleware provides Zitadel authentication middleware
type ZitadelAuthMiddleware struct {
	tokenValidator ZitadelTokenValidator
	logger         logger.Logger
}

// NewZitadelAuthMiddleware creates a new Zitadel authentication middleware
func NewZitadelAuthMiddleware(tokenValidator ZitadelTokenValidator, log logger.Logger) *ZitadelAuthMiddleware {
	return &ZitadelAuthMiddleware{
		tokenValidator: tokenValidator,
		logger:         log,
	}
}

// RequireAuthorization middleware that requires valid Zitadel authentication
// Optional role parameter for role-based access control
func (z *ZitadelAuthMiddleware) RequireAuthorization(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !z.tokenValidator.IsEnabled() {
				z.logger.Debug("Zitadel authentication is disabled, skipping auth check")
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				z.logger.Warn("Missing Authorization header")
				http.Error(w, "Authorization header required", http.StatusUnauthorized)
				return
			}

			token := z.extractTokenFromHeader(authHeader)
			if token == "" {
				z.logger.Warn("Invalid Authorization header format")
				http.Error(w, "Bearer token required", http.StatusUnauthorized)
				return
			}

			tokenInfo, err := z.tokenValidator.ValidateToken(r.Context(), token)
			if err != nil {
				z.logger.Warn("Token validation failed", logger.ErrorField(err))
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			// Convert token info to UserContext
			userCtx := z.tokenInfoToUserContext(tokenInfo)
			if userCtx == nil {
				z.logger.Error("Failed to convert token info to user context")
				http.Error(w, "Invalid token format", http.StatusUnauthorized)
				return
			}

			// Store raw token for downstream services
			userCtx.AuthToken = token

			// Check roles if specified
			if len(roles) > 0 && !z.hasAnyRole(userCtx, roles) {
				z.logger.Warn("Insufficient role",
					logger.Any("required_roles", roles),
					logger.Any("user_roles", userCtx.Roles),
				)
				http.Error(w, "Insufficient permissions", http.StatusForbidden)
				return
			}

			// Add user context to request context
			ctx := z.addUserContextToContext(r.Context(), userCtx)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth middleware that requires valid Zitadel authentication (alias for RequireAuthorization)
func (z *ZitadelAuthMiddleware) RequireAuth() func(http.Handler) http.Handler {
	return z.RequireAuthorization()
}

// RequireScope middleware that requires specific OAuth scope
func (z *ZitadelAuthMiddleware) RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return z.RequireAuthorization()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userCtx := GetUserContext(r.Context())
			if userCtx == nil {
				z.logger.Error("User context not found")
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}

			if !z.hasScope(userCtx.Claims, scope) {
				z.logger.Warn("Insufficient scope",
					logger.String("required", scope),
				)
				http.Error(w, "Insufficient permissions", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		}))
	}
}

// extractTokenFromHeader extracts the token from Authorization header
func (z *ZitadelAuthMiddleware) extractTokenFromHeader(authHeader string) string {
	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		return ""
	}
	return strings.TrimPrefix(authHeader, bearerPrefix)
}

// Context keys for user context
type contextKey string

const (
	userContextKey contextKey = "zitadel_user_context"
)

// addUserContextToContext adds user context to the request context
func (z *ZitadelAuthMiddleware) addUserContextToContext(ctx context.Context, userCtx *UserContext) context.Context {
	return context.WithValue(ctx, userContextKey, userCtx)
}

// GetUserContext retrieves user context from the request context (public method for handlers)
func GetUserContext(ctx context.Context) *UserContext {
	if userCtx, ok := ctx.Value(userContextKey).(*UserContext); ok {
		return userCtx
	}
	return nil
}

// UserInfo represents user information stored in context
type UserInfo struct {
	AuthID   string `json:"auth_id"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// GetUserInfo retrieves user info from context - alias for compatibility
func GetUserInfo(ctx context.Context) *UserInfo {
	if userCtx := GetUserContext(ctx); userCtx != nil {
		return &UserInfo{
			AuthID:   userCtx.UserID, // AuthID maps to UserID
			UserID:   userCtx.UserID,
			Username: userCtx.Username,
			Email:    userCtx.Email,
		}
	}
	return nil
}

// tokenInfoToUserContext converts token info to UserContext
func (z *ZitadelAuthMiddleware) tokenInfoToUserContext(tokenInfo interface{}) *UserContext {
	// Try to cast to the expected ZitadelTokenInfo structure
	if tokenInfoMap, ok := tokenInfo.(map[string]interface{}); ok {
		userCtx := &UserContext{
			Claims: tokenInfoMap,
		}

		// Extract standard fields
		if sub, ok := tokenInfoMap["sub"].(string); ok {
			userCtx.UserID = sub
		}
		if email, ok := tokenInfoMap["email"].(string); ok {
			userCtx.Email = email
		}
		if username, ok := tokenInfoMap["username"].(string); ok {
			userCtx.Username = username
		}

		// Extract roles from token (this might vary based on your Zitadel configuration)
		if rolesInterface, ok := tokenInfoMap["roles"]; ok {
			if rolesSlice, ok := rolesInterface.([]interface{}); ok {
				userCtx.Roles = make([]string, len(rolesSlice))
				for i, role := range rolesSlice {
					if roleStr, ok := role.(string); ok {
						userCtx.Roles[i] = roleStr
					}
				}
			} else if rolesStr, ok := rolesInterface.(string); ok {
				// Handle case where roles is a space-separated string
				userCtx.Roles = strings.Fields(rolesStr)
			}
		}

		// If no roles found, check scopes for role information
		if len(userCtx.Roles) == 0 {
			if scope, ok := tokenInfoMap["scope"].(string); ok {
				scopes := strings.Split(scope, " ")
				for _, s := range scopes {
					// Extract role-like scopes (e.g., "role:admin", "admin", etc.)
					if strings.Contains(s, "admin") {
						userCtx.Roles = append(userCtx.Roles, "admin")
					} else if strings.Contains(s, "user") {
						userCtx.Roles = append(userCtx.Roles, "user")
					}
				}
			}
		}

		// Default to "user" role if no roles found
		if len(userCtx.Roles) == 0 {
			userCtx.Roles = []string{"user"}
		}

		return userCtx
	}

	// Try direct struct conversion if it's a typed struct
	if tokenStruct, ok := tokenInfo.(interface {
		Subject() string
		Email() string
		Username() string
	}); ok {
		return &UserContext{
			UserID:   tokenStruct.Subject(),
			Username: tokenStruct.Username(),
			Email:    tokenStruct.Email(),
			Roles:    []string{"user"}, // Default role
			Claims:   map[string]interface{}{},
		}
	}

	z.logger.Error("Unable to convert token info to user context", logger.Any("token_info", tokenInfo))
	return nil
}

// hasAnyRole checks if the user has any of the required roles
func (z *ZitadelAuthMiddleware) hasAnyRole(userCtx *UserContext, requiredRoles []string) bool {
	for _, userRole := range userCtx.Roles {
		for _, requiredRole := range requiredRoles {
			if z.hasRole(userRole, requiredRole) {
				return true
			}
		}
	}
	return false
}

// hasRole checks if the user has the required role (with hierarchy)
func (z *ZitadelAuthMiddleware) hasRole(userRole, requiredRole string) bool {
	// Simple role hierarchy: admin > moderator > user
	roles := map[string]int{
		"admin":     3,
		"moderator": 2,
		"user":      1,
	}

	userLevel, userExists := roles[strings.ToLower(userRole)]
	requiredLevel, requiredExists := roles[strings.ToLower(requiredRole)]

	if !userExists || !requiredExists {
		return strings.EqualFold(userRole, requiredRole)
	}

	return userLevel >= requiredLevel
}

// hasScope checks if the token has the required scope
func (z *ZitadelAuthMiddleware) hasScope(claims map[string]interface{}, requiredScope string) bool {
	if scope, exists := claims["scope"]; exists {
		if scopeStr, ok := scope.(string); ok {
			scopes := strings.Split(scopeStr, " ")
			for _, s := range scopes {
				if strings.TrimSpace(s) == requiredScope {
					return true
				}
			}
		}
	}
	return false
}
