package middleware

import (
	"strings"
	"testing"
)

func TestRedactSensitiveQuery(t *testing.T) {
	query := redactSensitiveQuery("user_id=123&token=secret&service_id=notification")

	if strings.Contains(query, "secret") {
		t.Fatalf("expected token to be redacted, got %q", query)
	}
	if !strings.Contains(query, "token=%2A%2A%2A") {
		t.Fatalf("expected redacted token marker, got %q", query)
	}
	if !strings.Contains(query, "user_id=123") {
		t.Fatalf("expected non-sensitive query fields to remain, got %q", query)
	}
}

func TestRedactSensitiveQueryFallback(t *testing.T) {
	query := redactSensitiveQuery("%zz=bad&access_token=secret")

	if strings.Contains(query, "secret") {
		t.Fatalf("expected access token to be redacted, got %q", query)
	}
	if !strings.Contains(query, "access_token=***") {
		t.Fatalf("expected fallback redacted token marker, got %q", query)
	}
}
