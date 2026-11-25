package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/roles"
)

// SessionUser contains user information from session
type SessionUser struct {
	UserID         string   `json:"user_id"`
	SchoolID       string   `json:"school_id"` // From Zitadel organizationId
	Email          string   `json:"email"`
	Username       string   `json:"username"`
	FirstName      string   `json:"first_name"`
	LastName       string   `json:"last_name"`
	Role           string   `json:"role"`
	OrganizationID string   `json:"organization_id"` // Zitadel org ID
	CampusIDs      []string `json:"campus_ids"`      // List of accessible campuses
	AuthToken      string   `json:"-"`               // Original auth token for service-to-service calls
}

// SessionAuthMiddleware validates session tokens and extracts user context
type SessionAuthMiddleware struct {
	logger logger.Logger
}

// NewSessionAuthMiddleware creates a new session auth middleware
func NewSessionAuthMiddleware(log logger.Logger) *SessionAuthMiddleware {
	return &SessionAuthMiddleware{
		logger: log,
	}
}

// ExtractSessionContext extracts user information from session headers
// This middleware should be added to routes that need user context
func (m *SessionAuthMiddleware) ExtractSessionContext() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !m.isTrustedIdentitySource(r) {
				if m.hasIdentityHeaders(r) {
					m.logger.Warn("ignoring untrusted identity headers",
						logger.String("remote_addr", r.RemoteAddr),
						logger.String("path", r.URL.Path))
				}
				next.ServeHTTP(w, r)
				return
			}

			// Extract user info from headers (set by API Gateway or frontend)
			userID := r.Header.Get("X-User-ID")
			schoolID := r.Header.Get("X-School-ID")
			tenantID := r.Header.Get("X-Tenant-ID")
			email := r.Header.Get("X-User-Email")
			username := r.Header.Get("X-User-Name")
			role := r.Header.Get("X-User-Role")
			authToken := r.Header.Get("Authorization") // Preserve auth token for service-to-service calls
			if authToken == "" {
				authToken = r.Header.Get("X-User-Token")
			}
			campusIDsHeader := r.Header.Get("X-Campus-Ids")
			var campusIDs []string
			if campusIDsHeader != "" {
				campusIDs = strings.Split(campusIDsHeader, ",")
			}

			// If headers are not set, try to extract from session data header
			if userID == "" {
				sessionData := r.Header.Get("X-Session-Data")
				if sessionData != "" {
					user, err := m.parseSessionData(sessionData)
					if err != nil {
						m.logger.Warn("failed to parse session data", logger.ErrorField(err))
					} else if user != nil {
						userID = user.UserID
						schoolID = user.SchoolID
						email = user.Email
						username = user.Username
						role = user.Role
						tenantID = user.OrganizationID
						campusIDs = user.CampusIDs
					}
				}
			}

			// Create session user if we have at least userID
			if userID != "" {
				sessionUser := &SessionUser{
					UserID:         userID,
					SchoolID:       schoolID,
					Email:          email,
					Username:       username,
					Role:           role,
					OrganizationID: tenantID,
					CampusIDs:      campusIDs,
					AuthToken:      authToken,
				}

				// Add to context
				ctx := context.WithValue(r.Context(), sessionContextKey, sessionUser)

				m.logger.Debug("extracted session context",
					logger.String("user_id", userID),
					logger.String("school_id", schoolID),
					logger.String("role", role))

				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// No session context found, continue without it
			m.logger.Debug("no session context found, proceeding without authentication")
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuth ensures user is authenticated
func (m *SessionAuthMiddleware) RequireAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetSessionUser(r.Context())
			if user == nil || user.UserID == "" {
				m.logger.Warn("authentication required but no session found")
				respondJSON(w, http.StatusUnauthorized, map[string]interface{}{
					"success": false,
					"error":   "Authentication required. Please log in.",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireSchool ensures user has school context
func (m *SessionAuthMiddleware) RequireSchool() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetSessionUser(r.Context())
			if user == nil || user.SchoolID == "" {
				m.logger.Warn("school context required but not found")
				respondJSON(w, http.StatusForbidden, map[string]interface{}{
					"success": false,
					"error":   "School context required. Please ensure you are logged in with a school account.",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole ensures user has specific role
func (m *SessionAuthMiddleware) RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetSessionUser(r.Context())
			if user == nil {
				m.logger.Warn("authentication required for role check")
				respondJSON(w, http.StatusUnauthorized, map[string]interface{}{
					"success": false,
					"error":   "Authentication required",
				})
				return
			}

			hasRole := false
			for _, role := range roles {
				if m.userHasRole(user.Role, role) {
					hasRole = true
					break
				}
			}

			if !hasRole {
				m.logger.Warn("insufficient permissions",
					logger.String("user_role", user.Role),
					logger.Any("required_roles", roles))
				respondJSON(w, http.StatusForbidden, map[string]interface{}{
					"success": false,
					"error":   "Insufficient permissions",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (m *SessionAuthMiddleware) isTrustedIdentitySource(r *http.Request) bool {
	return r.Header.Get("X-Gateway-Service") != "" ||
		r.Header.Get("X-Gateway-Instance") != "" ||
		r.Header.Get("X-Service-Name") != ""
}

func (m *SessionAuthMiddleware) hasIdentityHeaders(r *http.Request) bool {
	return r.Header.Get("X-User-ID") != "" ||
		r.Header.Get("X-School-ID") != "" ||
		r.Header.Get("X-User-Email") != "" ||
		r.Header.Get("X-User-Role") != "" ||
		r.Header.Get("X-Tenant-ID") != "" ||
		r.Header.Get("X-Campus-Ids") != "" ||
		r.Header.Get("X-Session-Data") != ""
}

func (m *SessionAuthMiddleware) userHasRole(userRoles string, requiredRole string) bool {
	normalizedRequired := roles.Normalize(requiredRole)
	for _, role := range strings.Split(userRoles, ",") {
		normalized := roles.Normalize(strings.TrimSpace(role))
		// SuperAdmin bypasses all role checks
		if strings.EqualFold(normalized, "SuperAdmin") {
			return true
		}
		if strings.EqualFold(normalized, normalizedRequired) {
			return true
		}
	}
	return false
}

// parseSessionData parses JSON session data from header
func (m *SessionAuthMiddleware) parseSessionData(data string) (*SessionUser, error) {
	var user SessionUser
	if err := json.Unmarshal([]byte(data), &user); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %w", err)
	}
	return &user, nil
}

// Context key
type sessionContextKeyType string

const sessionContextKey sessionContextKeyType = "session_user"

// GetSessionUser retrieves session user from context
func GetSessionUser(ctx context.Context) *SessionUser {
	if user, ok := ctx.Value(sessionContextKey).(*SessionUser); ok {
		return user
	}
	return nil
}

// GetUserID retrieves user ID from session context
func GetUserID(ctx context.Context) string {
	if user := GetSessionUser(ctx); user != nil {
		return user.UserID
	}
	return ""
}

// GetSchoolID retrieves school ID from session context
func GetSchoolID(ctx context.Context) string {
	if user := GetSessionUser(ctx); user != nil {
		return user.SchoolID
	}
	return ""
}

// GetTenantID retrieves the tenant/organization ID from session context.
func GetTenantID(ctx context.Context) string {
	if user := GetSessionUser(ctx); user != nil {
		if user.OrganizationID != "" {
			return user.OrganizationID
		}
		return user.SchoolID
	}
	return ""
}

// WithServiceScope attaches tenant and school identity to a background context
// reconstructed from an authenticated event. It never adds a user bearer or role.
func WithServiceScope(ctx context.Context, tenantID, schoolID string) context.Context {
	user := &SessionUser{}
	if existing := GetSessionUser(ctx); existing != nil {
		clone := *existing
		user = &clone
	}
	user.OrganizationID = strings.TrimSpace(tenantID)
	user.SchoolID = strings.TrimSpace(schoolID)
	return context.WithValue(ctx, sessionContextKey, user)
}

// GetUsername retrieves username from session context
func GetUsername(ctx context.Context) string {
	if user := GetSessionUser(ctx); user != nil {
		if user.Username != "" {
			return user.Username
		}
		// Fallback to first + last name
		if user.FirstName != "" || user.LastName != "" {
			return strings.TrimSpace(user.FirstName + " " + user.LastName)
		}
	}
	return "Unknown User"
}

// GetUserEmail retrieves user email from session context
func GetUserEmail(ctx context.Context) string {
	if user := GetSessionUser(ctx); user != nil {
		return user.Email
	}
	return ""
}

// GetCampusIDs retrieves campus IDs from session context
func GetCampusIDs(ctx context.Context) []string {
	if user := GetSessionUser(ctx); user != nil {
		return user.CampusIDs
	}
	return []string{}
}

// GetAuthToken retrieves the authorization token from session context
// This is useful for service-to-service calls that need to forward the original auth token
func GetAuthToken(ctx context.Context) string {
	if user := GetSessionUser(ctx); user != nil {
		return user.AuthToken
	}
	if userCtx := GetUserContext(ctx); userCtx != nil {
		return userCtx.AuthToken
	}
	return ""
}

// GetRoles retrieves the user's roles as a slice from session context.
// The Role field may be a single value or a comma-separated list.
func GetRoles(ctx context.Context) []string {
	user := GetSessionUser(ctx)
	if user == nil || user.Role == "" {
		return []string{}
	}
	parts := strings.Split(user.Role, ",")
	roles := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			roles = append(roles, t)
		}
	}
	return roles
}

// respondJSON sends JSON response
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
