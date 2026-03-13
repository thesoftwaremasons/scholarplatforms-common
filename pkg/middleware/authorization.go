package middleware

import (
	"context"
	"net/http"
	"strings"
)

// Claims represents the authenticated user's claims from JWT
type Claims struct {
	UserID   string   `json:"user_id"`
	Email    string   `json:"email"`
	SchoolID string   `json:"school_id"`
	Roles    []string `json:"roles"`
}

// GetClaimsFromContext extracts claims from request context
// The API Gateway sets these values after JWT validation
func GetClaimsFromContext(ctx context.Context) (*Claims, bool) {
	userID, ok := ctx.Value("user_id").(string)
	if !ok || userID == "" {
		return nil, false
	}

	email, _ := ctx.Value("email").(string)
	schoolID, _ := ctx.Value("school_id").(string)

	// Roles might be []interface{} from JWT, convert to []string
	var roles []string
	if rolesInterface, ok := ctx.Value("roles").([]interface{}); ok {
		roles = make([]string, len(rolesInterface))
		for i, r := range rolesInterface {
			if role, ok := r.(string); ok {
				roles[i] = role
			}
		}
	} else if rolesStrings, ok := ctx.Value("roles").([]string); ok {
		roles = rolesStrings
	}

	return &Claims{
		UserID:   userID,
		Email:    email,
		SchoolID: schoolID,
		Roles:    roles,
	}, true
}

// GetUserIDFromContext is a convenience function to just get user_id
func GetUserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value("user_id").(string)
	return userID, ok && userID != ""
}

// GetSchoolIDFromContext is a convenience function to just get school_id
func GetSchoolIDFromContext(ctx context.Context) (string, bool) {
	schoolID, ok := ctx.Value("school_id").(string)
	return schoolID, ok && schoolID != ""
}

// RequireRole middleware checks if user has any of the required roles
// Usage: r.Handle("/admin/users", middleware.RequireRole("admin")(adminHandler))
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetClaimsFromContext(r.Context())
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Check if user has any of the required roles
			hasRole := false
			for _, requiredRole := range roles {
				for _, userRole := range claims.Roles {
					if strings.EqualFold(userRole, requiredRole) {
						hasRole = true
						break
					}
				}
				if hasRole {
					break
				}
			}

			if !hasRole {
				http.Error(w, "Forbidden: insufficient permissions", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermission checks if user has a specific permission
// Permissions are mapped from roles using a permission system
// Usage: r.Handle("/api/grades", middleware.RequirePermission("grades:write")(gradesHandler))
func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetClaimsFromContext(r.Context())
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			hasPermission := checkPermission(claims.Roles, permission)
			if !hasPermission {
				http.Error(w, "Forbidden: missing required permission", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAdmin is a convenience middleware that requires admin role
// Usage: r.Handle("/admin/settings", middleware.RequireAdmin()(settingsHandler))
func RequireAdmin() func(http.Handler) http.Handler {
	return RequireRole("admin")
}

// RequireTeacherOrAdmin requires either teacher or admin role
// Usage: r.Handle("/grades", middleware.RequireTeacherOrAdmin()(gradesHandler))
func RequireTeacherOrAdmin() func(http.Handler) http.Handler {
	return RequireRole("admin", "teacher")
}

// RequireSameSchool checks if the authenticated user belongs to the same school
// This prevents cross-school data access
func RequireSameSchool(getTargetSchoolID func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetClaimsFromContext(r.Context())
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			targetSchoolID := getTargetSchoolID(r)
			if targetSchoolID != "" && claims.SchoolID != targetSchoolID {
				http.Error(w, "Forbidden: cross-school access denied", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// checkPermission maps permissions to roles
// This is a simple implementation - you can expand this to a full RBAC system
func checkPermission(roles []string, permission string) bool {
	// Admin has all permissions
	for _, role := range roles {
		if strings.EqualFold(role, "admin") {
			return true
		}
	}

	// Map specific permissions to roles
	permissionMap := map[string][]string{
		// User management
		"users:read":   {"admin", "teacher"},
		"users:write":  {"admin"},
		"users:delete": {"admin"},

		// Student management
		"students:read":   {"admin", "teacher"},
		"students:write":  {"admin", "teacher"},
		"students:delete": {"admin"},

		// Grades and assessments
		"grades:read":       {"admin", "teacher"},
		"grades:write":      {"admin", "teacher"},
		"assessments:read":  {"admin", "teacher"},
		"assessments:write": {"admin", "teacher"},

		// Curriculum and subjects
		"curriculum:read":  {"admin", "teacher"},
		"curriculum:write": {"admin"},
		"subjects:read":    {"admin", "teacher"},
		"subjects:write":   {"admin"},

		// Timetable
		"timetable:read":  {"admin", "teacher"},
		"timetable:write": {"admin"},

		// School settings
		"school:read":   {"admin"},
		"school:write":  {"admin"},
		"school:delete": {"admin"},

		// Reports
		"reports:read": {"admin", "teacher"},
	}

	allowedRoles, exists := permissionMap[permission]
	if !exists {
		// Permission not defined - deny by default
		return false
	}

	for _, role := range roles {
		for _, allowedRole := range allowedRoles {
			if strings.EqualFold(role, allowedRole) {
				return true
			}
		}
	}

	return false
}

// HasRole checks if the user has a specific role (utility function)
func HasRole(claims *Claims, role string) bool {
	for _, userRole := range claims.Roles {
		if strings.EqualFold(userRole, role) {
			return true
		}
	}
	return false
}

// HasPermission checks if the user has a specific permission (utility function)
func HasPermission(claims *Claims, permission string) bool {
	return checkPermission(claims.Roles, permission)
}

// IsAdmin checks if the user is an admin (utility function)
func IsAdmin(claims *Claims) bool {
	return HasRole(claims, "admin")
}

// IsTeacher checks if the user is a teacher (utility function)
func IsTeacher(claims *Claims) bool {
	return HasRole(claims, "teacher")
}

// IsStudent checks if the user is a student (utility function)
func IsStudent(claims *Claims) bool {
	return HasRole(claims, "student")
}
