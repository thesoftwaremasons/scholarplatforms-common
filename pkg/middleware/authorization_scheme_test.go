package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGatewayHeaderClaimsAuthorizesSlugRoleForSchemeSubmit(t *testing.T) {
	DefaultPermissionsClient = nil
	called := false
	handler := GatewayHeaderClaims()(RequirePermission("academics:scheme:submit")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })))
	req := httptest.NewRequest(http.MethodPost, "/syllabus/id/submit", nil)
	req.Header.Set("X-Gateway-Service", "scholarplatforms-apigateway")
	req.Header.Set("X-User-ID", "teacher-1")
	req.Header.Set("X-School-ID", "school-1")
	req.Header.Set("X-User-Role", "subject_teacher")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !called {
		t.Fatalf("status=%d called=%v", res.Code, called)
	}
}

func TestGatewayHeaderClaimsIgnoresUntrustedIdentityHeaders(t *testing.T) {
	DefaultPermissionsClient = nil
	handler := GatewayHeaderClaims()(RequirePermission("academics:scheme:submit")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	req := httptest.NewRequest(http.MethodPost, "/syllabus/id/submit", nil)
	req.Header.Set("X-User-ID", "forged-user")
	req.Header.Set("X-User-Role", "super_admin")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want %d", res.Code, http.StatusUnauthorized)
	}
}
