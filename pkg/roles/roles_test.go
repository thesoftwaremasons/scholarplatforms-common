package roles

import "testing"

func TestNormalizeAcceptsStableRoleSlugs(t *testing.T) {
	tests := map[string]string{
		"school-admin":      SchoolAdmin,
		"campus-admin":      CampusAdmin,
		"stage-coordinator": StageCoordinator,
		"subject-teacher":   SubjectTeacher,
		"class teacher":     ClassTeacher,
	}
	for input, want := range tests {
		if got := Normalize(input); got != want {
			t.Errorf("Normalize(%q) = %q; want %q", input, got, want)
		}
	}
}

func TestCanInviteSchoolAdminWithinSchool(t *testing.T) {
	if !CanInvite([]string{SchoolAdmin}, SchoolAdmin) {
		t.Fatal("SchoolAdmin should be able to invite another SchoolAdmin")
	}
	if !CanInvite([]string{"school_admin"}, "school_admin") {
		t.Fatal("legacy role names should be normalized for invitations")
	}
}

func TestCanInviteDoesNotBroadenRoleMutationPolicy(t *testing.T) {
	if CanAssign([]string{SchoolAdmin}, SchoolAdmin) {
		t.Fatal("SchoolAdmin must not gain unrestricted SchoolAdmin role mutation rights")
	}
	if CanInvite([]string{FinanceOfficer}, SchoolAdmin) {
		t.Fatal("FinanceOfficer must not be able to invite a SchoolAdmin")
	}
}
