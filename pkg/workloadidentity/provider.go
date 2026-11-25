package workloadidentity

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const jwtBearerGrantType = "urn:ietf:params:oauth:grant-type:jwt-bearer"

type Config struct {
	TokenURL       string
	IdentityID     string
	KeyID          string
	PrivateKeyPath string
	Audience       string
	Scopes         []string
	RefreshSkew    time.Duration
	InsecureTLS    bool
}

type Provider struct {
	tokenURL    string
	identityID  string
	keyID       string
	audience    string
	scopes      []string
	refreshSkew time.Duration
	privateKey  *rsa.PrivateKey
	httpClient  *http.Client
	now         func() time.Time

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

type serviceAccountKey struct {
	Type   string `json:"type"`
	Key    string `json:"key"`
	KeyID  string `json:"keyId"`
	UserID string `json:"userId"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

func NewProvider(cfg Config) (*Provider, error) {
	if strings.TrimSpace(cfg.TokenURL) == "" {
		return nil, fmt.Errorf("workload identity token URL is required")
	}
	if strings.TrimSpace(cfg.PrivateKeyPath) == "" {
		return nil, fmt.Errorf("workload identity private key path is required")
	}

	keyData, err := os.ReadFile(cfg.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read workload identity key: %w", err)
	}
	var serviceKey serviceAccountKey
	if json.Unmarshal(keyData, &serviceKey) == nil && strings.TrimSpace(serviceKey.Key) != "" {
		if serviceKey.Type != "" && serviceKey.Type != "serviceaccount" {
			return nil, fmt.Errorf("workload identity key must be a Zitadel service-account key, got %q", serviceKey.Type)
		}
		keyData = []byte(serviceKey.Key)
		// The credential file is authoritative. Zitadel rotations can change both
		// identifiers, so separately configured values become stale after recovery.
		cfg.IdentityID = serviceKey.UserID
		cfg.KeyID = serviceKey.KeyID
	}
	if strings.TrimSpace(cfg.IdentityID) == "" {
		return nil, fmt.Errorf("workload identity service-account user ID is required")
	}
	if strings.TrimSpace(cfg.KeyID) == "" {
		return nil, fmt.Errorf("workload identity key ID is required")
	}

	privateKey, err := parseRSAPrivateKey(keyData)
	if err != nil {
		return nil, err
	}
	if cfg.Audience == "" {
		parsed, parseErr := url.Parse(cfg.TokenURL)
		if parseErr != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("derive workload identity audience from token URL %q", cfg.TokenURL)
		}
		cfg.Audience = parsed.Scheme + "://" + parsed.Host
	}
	if cfg.RefreshSkew <= 0 {
		cfg.RefreshSkew = time.Minute
	}
	scopes := make([]string, 0, len(cfg.Scopes))
	hasAudienceScope := false
	for _, scope := range cfg.Scopes {
		if scope = strings.TrimSpace(scope); scope != "" {
			scopes = append(scopes, scope)
			hasAudienceScope = hasAudienceScope || strings.HasSuffix(scope, ":aud")
		}
	}
	if !hasAudienceScope {
		return nil, fmt.Errorf("workload identity requires a Zitadel project audience scope ending in :aud")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.InsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // Development-only option.
	}
	return &Provider{
		tokenURL: cfg.TokenURL, identityID: cfg.IdentityID, keyID: cfg.KeyID,
		audience: cfg.Audience, scopes: scopes, refreshSkew: cfg.RefreshSkew,
		privateKey: privateKey, httpClient: &http.Client{Timeout: 15 * time.Second, Transport: transport}, now: time.Now,
	}, nil
}

func parseRSAPrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("decode workload identity private key: no PEM block found")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse workload identity private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("workload identity private key is not RSA")
	}
	return key, nil
}

func (p *Provider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if p.token != "" && now.Add(p.refreshSkew).Before(p.expiresAt) {
		return p.token, nil
	}
	assertion, err := p.assertion(now)
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {jwtBearerGrantType}, "assertion": {assertion}}
	form.Set("scope", strings.Join(p.scopes, " "))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create workload token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request workload token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("workload token endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var token tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", fmt.Errorf("decode workload token response: %w", err)
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return "", fmt.Errorf("workload token response did not contain an access token")
	}
	if token.ExpiresIn <= 0 {
		token.ExpiresIn = 300
	}
	p.token = token.AccessToken
	p.expiresAt = now.Add(time.Duration(token.ExpiresIn) * time.Second)
	return p.token, nil
}

func (p *Provider) assertion(now time.Time) (string, error) {
	claims := jwt.RegisteredClaims{
		Issuer: p.identityID, Subject: p.identityID, Audience: jwt.ClaimStrings{p.audience},
		ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)), NotBefore: jwt.NewNumericDate(now),
		IssuedAt: jwt.NewNumericDate(now), ID: uuid.NewString(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = p.keyID
	signed, err := token.SignedString(p.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign workload assertion: %w", err)
	}
	return signed, nil
}

func (p *Provider) IdentityID() string { return p.identityID }

type Transport struct {
	Provider *Provider
	Base     http.RoundTripper
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Authorization") != "" || t.Provider == nil {
		return t.base().RoundTrip(req)
	}
	token, err := t.Provider.Token(req.Context())
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+token)
	return t.base().RoundTrip(clone)
}

func (t *Transport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}
