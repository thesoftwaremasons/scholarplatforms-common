package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// PermissionsClient calls the userprofile service's internal permission check
// endpoint. It is used by RequirePermission when the DB-backed RBAC system is
// enabled (USE_DB_PERMISSIONS=true).
//
// Configure once at startup with SetPermissionsClient; RequirePermission picks
// it up automatically for every subsequent request.
type PermissionsClient struct {
	baseURL       string // e.g. "http://userprofile:8080"
	internalToken string
	httpClient    *http.Client
}

// DefaultPermissionsClient is the package-level client. nil = legacy Go map.
// Set it at server startup via SetPermissionsClient.
var DefaultPermissionsClient *PermissionsClient

// SetPermissionsClient configures the package-level permissions client.
// Call this once in NewServer if cfg.Features.UseDBPermissions is true.
func SetPermissionsClient(baseURL, internalToken string) {
	DefaultPermissionsClient = &PermissionsClient{
		baseURL:       baseURL,
		internalToken: internalToken,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

// checkPermissionRequest mirrors the userprofile handler's request body.
type checkPermissionRequest struct {
	UserID     string `json:"user_id"`
	SchoolID   string `json:"school_id"`
	Permission string `json:"permission"`
}

// checkPermissionResponse mirrors the userprofile handler's response body.
type checkPermissionResponse struct {
	Allowed bool `json:"allowed"`
}

// Check calls POST /internal/permissions/check and returns whether the user
// holds the named permission. On any network or decode error it returns false
// and the error; callers typically fall back to the legacy Go map.
func (c *PermissionsClient) Check(ctx context.Context, userID, schoolID, permission string) (bool, error) {
	body, err := json.Marshal(checkPermissionRequest{
		UserID:     userID,
		SchoolID:   schoolID,
		Permission: permission,
	})
	if err != nil {
		return false, fmt.Errorf("permissions client: marshal: %w", err)
	}

	url := c.baseURL + "/internal/permissions/check"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("permissions client: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", c.internalToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("permissions client: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("permissions client: unexpected status %d", resp.StatusCode)
	}

	var out checkPermissionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, fmt.Errorf("permissions client: decode: %w", err)
	}
	return out.Allowed, nil
}
