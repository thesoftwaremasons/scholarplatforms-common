package workloadidentity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestProviderUsesServiceAccountKeyAndCachesToken(t *testing.T) {
	key, path := writeKey(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != jwtBearerGrantType {
			t.Fatalf("grant_type = %q", r.Form.Get("grant_type"))
		}
		assertion, err := jwt.Parse(r.Form.Get("assertion"), func(*jwt.Token) (interface{}, error) { return &key.PublicKey, nil })
		if err != nil || !assertion.Valid {
			t.Fatalf("invalid assertion: %v", err)
		}
		_, _ = io.WriteString(w, `{"access_token":"machine-token","expires_in":300}`)
	}))
	defer server.Close()
	provider, err := NewProvider(Config{TokenURL: server.URL, IdentityID: "stale-user", KeyID: "stale-key", PrivateKeyPath: path, Scopes: []string{"openid", "project:aud"}, RefreshSkew: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if provider.IdentityID() != "user-1" || provider.keyID != "key-1" {
		t.Fatalf("provider retained stale configured IDs: identity=%q key=%q", provider.IdentityID(), provider.keyID)
	}
	for i := 0; i < 2; i++ {
		token, err := provider.Token(context.Background())
		if err != nil || token != "machine-token" {
			t.Fatalf("token=%q err=%v", token, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestTransportPreservesUserBearer(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Get("Authorization") }))
	defer server.Close()
	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	req.Header.Set("Authorization", "Bearer user-token")
	resp, err := (&http.Client{Transport: &Transport{}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got != "Bearer user-token" {
		t.Fatalf("authorization=%q", got)
	}
}

func writeKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	payload, _ := json.Marshal(serviceAccountKey{Type: "serviceaccount", Key: string(pemBytes), KeyID: "key-1", UserID: "user-1"})
	path := filepath.Join(t.TempDir(), "key.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return key, path
}
