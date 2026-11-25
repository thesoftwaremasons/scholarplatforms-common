package roles

import "strings"

// Platform-level role constants
const (
	SuperAdmin      = "SuperAdmin"
	PlatformSupport = "PlatformSupport"
	PlatformBilling = "PlatformBilling"
	SchoolOnboarder = "SchoolOnboarder"
)

// School-level role constants
const (
	SchoolAdmin      = "SchoolAdmin"
	CampusAdmin      = "CampusAdmin"
	StageCoordinator = "StageCoordinator"
	ClassTeacher     = "ClassTeacher"
	SubjectTeacher   = "SubjectTeacher"
	FinanceOfficer   = "FinanceOfficer"
	Cashier          = "Cashier"
	Registrar        = "Registrar"
	SupportStaff     = "SupportStaff"
	Parent           = "Parent"
	Student          = "Student"
)

// All contains every valid role string in one slice.
var All = []string{
	SuperAdmin, PlatformSupport, PlatformBilling, SchoolOnboarder,
	SchoolAdmin, CampusAdmin, StageCoordinator, ClassTeacher, SubjectTeacher,
	FinanceOfficer, Cashier, Registrar, SupportStaff, Parent, Student,
}

// assignableBy maps an actor role to the set of roles it may grant to others.
// SuperAdmin is handled separately (can assign everything).
var assignableBy = map[string][]string{
	SchoolAdmin: {
		CampusAdmin, StageCoordinator, ClassTeacher, SubjectTeacher,
		FinanceOfficer, Cashier, Registrar, SupportStaff, Parent, Student,
	},
	CampusAdmin: {
		ClassTeacher, SubjectTeacher, SupportStaff, Parent, Student,
	},
}

// displayNames maps role constants to human-readable labels.
var displayNames = map[string]string{
	SuperAdmin:       "Platform Super Admin",
	PlatformSupport:  "Platform Support",
	PlatformBilling:  "Platform Billing",
	SchoolOnboarder:  "School Onboarder",
	SchoolAdmin:      "School Admin",
	CampusAdmin:      "Campus Admin",
	StageCoordinator: "Stage Coordinator",
	ClassTeacher:     "Class Teacher",
	SubjectTeacher:   "Subject Teacher",
	FinanceOfficer:   "Finance Officer",
	Cashier:          "Cashier",
	Registrar:        "Registrar",
	SupportStaff:     "Support Staff",
	Parent:           "Parent",
	Student:          "Student",
}

// IsSuperAdmin returns true if any role in the list is SuperAdmin.
func IsSuperAdmin(userRoles []string) bool {
	for _, r := range userRoles {
		if strings.EqualFold(Normalize(r), SuperAdmin) {
			return true
		}
	}
	return false
}

// HasAny returns true if the user holds at least one of the allowed roles.
// SuperAdmin always passes — it bypasses all role checks.
// Input roles are normalized (snake_case → PascalCase) before comparison so that
// JWTs emitting "school_admin" match the canonical "SchoolAdmin" constant.
func HasAny(userRoles []string, allowedRoles ...string) bool {
	if IsSuperAdmin(userRoles) {
		return true
	}
	for _, ur := range userRoles {
		normalized := Normalize(ur)
		for _, ar := range allowedRoles {
			if strings.EqualFold(normalized, ar) {
				return true
			}
		}
	}
	return false
}

// CanAssign returns true if any role in actorRoles is permitted to grant targetRole.
// SuperAdmin may assign any role.
func CanAssign(actorRoles []string, targetRole string) bool {
	if IsSuperAdmin(actorRoles) {
		return true
	}
	for _, actor := range actorRoles {
		normalizedActor := Normalize(actor)
		for canonicalActor, grantable := range assignableBy {
			if strings.EqualFold(normalizedActor, canonicalActor) {
				for _, g := range grantable {
					if strings.EqualFold(Normalize(targetRole), g) {
						return true
					}
				}
			}
		}
	}
	return false
}

// CanInvite returns true when an actor may invite a user with the target role.
// School administrators may invite another SchoolAdmin into their own school so
// tenant-level maker/checker workflows can be completed without platform staff.
// Role mutation and revocation continue to use CanAssign's narrower policy.
func CanInvite(actorRoles []string, targetRole string) bool {
	if CanAssign(actorRoles, targetRole) {
		return true
	}
	return HasAny(actorRoles, SchoolAdmin) && strings.EqualFold(Normalize(targetRole), SchoolAdmin)
}

// AssignableRoles returns the distinct set of roles that actorRoles may grant.
func AssignableRoles(actorRoles []string) []string {
	if IsSuperAdmin(actorRoles) {
		return All
	}
	seen := map[string]bool{}
	var result []string
	for _, actor := range actorRoles {
		normalizedActor := Normalize(actor)
		for canonicalActor, grantable := range assignableBy {
			if strings.EqualFold(normalizedActor, canonicalActor) {
				for _, g := range grantable {
					if !seen[g] {
						seen[g] = true
						result = append(result, g)
					}
				}
			}
		}
	}
	return result
}

// DisplayName returns the human-readable label for a role constant.
// Returns the role string itself if no mapping is found.
func DisplayName(role string) string {
	for k, v := range displayNames {
		if strings.EqualFold(role, k) {
			return v
		}
	}
	return role
}

// IsValid returns true if the role string is one of the canonical role constants.
func IsValid(role string) bool {
	for _, r := range All {
		if strings.EqualFold(role, r) {
			return true
		}
	}
	return false
}

// legacyNormMap maps historical snake_case or alternate spellings to PascalCase canonical form.
var legacyNormMap = map[string]string{
	"super_admin":       SuperAdmin,
	"platform_support":  PlatformSupport,
	"platform_billing":  PlatformBilling,
	"school_onboarder":  SchoolOnboarder,
	"school_admin":      SchoolAdmin,
	"campus_admin":      CampusAdmin,
	"stage_coordinator": StageCoordinator,
	"class_teacher":     ClassTeacher,
	"subject_teacher":   SubjectTeacher,
	"finance_officer":   FinanceOfficer,
	"cashier":           Cashier,
	"registrar":         Registrar,
	"support_staff":     SupportStaff,
	"parent":            Parent,
	"student":           Student,
	// old alias used in some places
	"teacher": ClassTeacher,
}

// Normalize converts a role string to its canonical PascalCase form.
// If the string is already canonical (case-insensitive match) it returns the canonical constant.
// If it matches a known legacy alias, it returns the canonical constant.
// Otherwise it returns the input unchanged.
func Normalize(role string) string {
	lower := strings.ToLower(strings.TrimSpace(role))
	// Role assignments use stable kebab-case slugs (for example
	// "school-admin"), while older JWTs and service contracts use snake_case.
	// Treat separators consistently so workflow roles resolve to real users.
	lower = strings.NewReplacer("-", "_", " ", "_").Replace(lower)
	if mapped, ok := legacyNormMap[lower]; ok {
		return mapped
	}
	for _, r := range All {
		if strings.EqualFold(role, r) {
			return r
		}
	}
	return role
}

// NormalizeAll normalizes every element in the slice and deduplicates results.
// Unrecognized role strings are dropped.
func NormalizeAll(rawRoles []string) []string {
	seen := make(map[string]bool, len(rawRoles))
	out := make([]string, 0, len(rawRoles))
	for _, r := range rawRoles {
		normalized := Normalize(r)
		if IsValid(normalized) && !seen[normalized] {
			seen[normalized] = true
			out = append(out, normalized)
		}
	}
	return out
}
