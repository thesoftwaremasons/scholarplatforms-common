package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/roles"
)

// Claims represents the authenticated user's claims from JWT
type Claims struct {
	UserID   string   `json:"user_id"`
	Email    string   `json:"email"`
	SchoolID string   `json:"school_id"`
	Roles    []string `json:"roles"`
}

// GetClaimsFromContext extracts claims from request context.
// The API Gateway sets these values after JWT validation.
func GetClaimsFromContext(ctx context.Context) (*Claims, bool) {
	userID, ok := ctx.Value("user_id").(string)
	if !ok || userID == "" {
		return nil, false
	}

	email, _ := ctx.Value("email").(string)
	schoolID, _ := ctx.Value("school_id").(string)

	var claimRoles []string
	if rolesInterface, ok := ctx.Value("roles").([]interface{}); ok {
		claimRoles = make([]string, len(rolesInterface))
		for i, r := range rolesInterface {
			if role, ok := r.(string); ok {
				claimRoles[i] = role
			}
		}
	} else if rolesStrings, ok := ctx.Value("roles").([]string); ok {
		claimRoles = rolesStrings
	}

	return &Claims{
		UserID:   userID,
		Email:    email,
		SchoolID: schoolID,
		Roles:    claimRoles,
	}, true
}

// GetUserIDFromContext is a convenience helper.
func GetUserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value("user_id").(string)
	return userID, ok && userID != ""
}

// GetSchoolIDFromContext is a convenience helper.
func GetSchoolIDFromContext(ctx context.Context) (string, bool) {
	schoolID, ok := ctx.Value("school_id").(string)
	return schoolID, ok && schoolID != ""
}

// RequireRole middleware checks if the user holds any of the required roles.
// SuperAdmin always passes.
func RequireRole(requiredRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetClaimsFromContext(r.Context())
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			if !roles.HasAny(claims.Roles, requiredRoles...) {
				http.Error(w, "Forbidden: insufficient permissions", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermission checks if the user has a named permission via the role→permission map.
func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetClaimsFromContext(r.Context())
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			if !checkPermission(claims.Roles, permission) {
				http.Error(w, "Forbidden: missing required permission", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAdmin requires SchoolAdmin or SuperAdmin.
func RequireAdmin() func(http.Handler) http.Handler {
	return RequireRole(roles.SchoolAdmin, roles.SuperAdmin)
}

// RequireTeacherOrAdmin requires ClassTeacher, SubjectTeacher, SchoolAdmin, or SuperAdmin.
func RequireTeacherOrAdmin() func(http.Handler) http.Handler {
	return RequireRole(roles.SchoolAdmin, roles.CampusAdmin, roles.ClassTeacher, roles.SubjectTeacher, roles.SuperAdmin)
}

// RequireSameSchool ensures the authenticated user belongs to the requested school.
// SuperAdmin bypasses this check.
func RequireSameSchool(getTargetSchoolID func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetClaimsFromContext(r.Context())
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			if roles.IsSuperAdmin(claims.Roles) {
				next.ServeHTTP(w, r)
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

// checkPermission maps named permissions to the roles that hold them.
// SuperAdmin always has every permission.
func checkPermission(userRoles []string, permission string) bool {
	if roles.IsSuperAdmin(userRoles) {
		return true
	}

	permissionMap := map[string][]string{
		// User management
		"users:read":   {roles.SchoolAdmin, roles.CampusAdmin, roles.ClassTeacher, roles.SubjectTeacher, roles.Registrar},
		"users:write":  {roles.SchoolAdmin, roles.CampusAdmin, roles.Registrar},
		"users:delete": {roles.SchoolAdmin},

		// Student management
		"students:read":   {roles.SchoolAdmin, roles.CampusAdmin, roles.StageCoordinator, roles.ClassTeacher, roles.SubjectTeacher, roles.Registrar, roles.SupportStaff},
		"students:write":  {roles.SchoolAdmin, roles.CampusAdmin, roles.ClassTeacher, roles.Registrar},
		"students:delete": {roles.SchoolAdmin, roles.Registrar},

		// Grades and assessments
		"grades:read":       {roles.SchoolAdmin, roles.CampusAdmin, roles.StageCoordinator, roles.ClassTeacher, roles.SubjectTeacher},
		"grades:write":      {roles.SchoolAdmin, roles.CampusAdmin, roles.StageCoordinator, roles.ClassTeacher, roles.SubjectTeacher},
		"assessments:read":  {roles.SchoolAdmin, roles.CampusAdmin, roles.StageCoordinator, roles.ClassTeacher, roles.SubjectTeacher},
		"assessments:write": {roles.SchoolAdmin, roles.CampusAdmin, roles.ClassTeacher, roles.SubjectTeacher},

		// Curriculum and subjects
		"curriculum:read":  {roles.SchoolAdmin, roles.CampusAdmin, roles.StageCoordinator, roles.ClassTeacher, roles.SubjectTeacher},
		"curriculum:write": {roles.SchoolAdmin, roles.CampusAdmin, roles.StageCoordinator},
		"subjects:read":    {roles.SchoolAdmin, roles.CampusAdmin, roles.StageCoordinator, roles.ClassTeacher, roles.SubjectTeacher},
		"subjects:write":   {roles.SchoolAdmin, roles.CampusAdmin, roles.StageCoordinator},

		// Timetable
		"timetable:read":  {roles.SchoolAdmin, roles.CampusAdmin, roles.StageCoordinator, roles.ClassTeacher, roles.SubjectTeacher},
		"timetable:write": {roles.SchoolAdmin, roles.CampusAdmin, roles.StageCoordinator},

		// Student monitoring
		"monitoring:read": {roles.SchoolAdmin, roles.CampusAdmin, roles.StageCoordinator, roles.ClassTeacher, roles.SupportStaff},

		// Finance
		"finance:read":     {roles.SchoolAdmin, roles.FinanceOfficer, roles.Cashier},
		"finance:write":    {roles.SchoolAdmin, roles.FinanceOfficer},
		"finance:payments": {roles.SchoolAdmin, roles.FinanceOfficer, roles.Cashier},
		"finance:payouts":  {roles.SchoolAdmin, roles.FinanceOfficer},

		// School settings
		"school:read":   {roles.SchoolAdmin, roles.CampusAdmin},
		"school:write":  {roles.SchoolAdmin},
		"school:delete": {roles.SchoolAdmin},

		// Role management
		"roles:assign": {roles.SchoolAdmin, roles.CampusAdmin},
		"roles:revoke": {roles.SchoolAdmin, roles.CampusAdmin},

		// Reports
		"reports:read": {roles.SchoolAdmin, roles.CampusAdmin, roles.StageCoordinator, roles.ClassTeacher, roles.SubjectTeacher, roles.FinanceOfficer, roles.Registrar, roles.SupportStaff},
	}

	allowedRoles, exists := permissionMap[permission]
	if !exists {
		return false
	}

	for _, ur := range userRoles {
		for _, ar := range allowedRoles {
			if strings.EqualFold(ur, ar) {
				return true
			}
		}
	}
	return false
}

// HasRole checks if the user holds a specific role.
func HasRole(claims *Claims, role string) bool {
	for _, userRole := range claims.Roles {
		if strings.EqualFold(userRole, role) {
			return true
		}
	}
	return false
}

// HasPermission checks if the user has a specific permission.
func HasPermission(claims *Claims, permission string) bool {
	return checkPermission(claims.Roles, permission)
}

// IsAdmin checks if the user is a SchoolAdmin or SuperAdmin.
func IsAdmin(claims *Claims) bool {
	return HasRole(claims, roles.SchoolAdmin) || HasRole(claims, roles.SuperAdmin)
}

// IsTeacher checks if the user is a ClassTeacher or SubjectTeacher.
func IsTeacher(claims *Claims) bool {
	return HasRole(claims, roles.ClassTeacher) || HasRole(claims, roles.SubjectTeacher)
}

// IsStudent checks if the user is a Student.
func IsStudent(claims *Claims) bool {
	return HasRole(claims, roles.Student)
}
