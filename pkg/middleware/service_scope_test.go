package middleware

import (
	"context"
	"testing"
)

func TestWithServiceScope(t *testing.T) {
	ctx := WithServiceScope(context.Background(), "tenant-1", "school-1")
	if got := GetTenantID(ctx); got != "tenant-1" {
		t.Fatalf("expected tenant-1, got %q", got)
	}
	if got := GetSchoolID(ctx); got != "school-1" {
		t.Fatalf("expected school-1, got %q", got)
	}
	if got := GetUserID(ctx); got != "" {
		t.Fatalf("service scope must not invent a user, got %q", got)
	}
}
