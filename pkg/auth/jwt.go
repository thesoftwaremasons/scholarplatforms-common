package auth

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims represents JWT claims
type Claims struct {
	UserID       string `json:"user_id"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	Organization string `json:"organization"`
	ServiceID    string `json:"service_id,omitempty"`
	jwt.RegisteredClaims
}

// JWTManager handles JWT token operations
type JWTManager struct {
	secretKey        []byte
	tokenExpiry      time.Duration
	refreshTokenExpiry time.Duration
}

// NewJWTManager creates a new JWT manager
func NewJWTManager(secretKey string, tokenExpiry, refreshTokenExpiry time.Duration) *JWTManager {
	return &JWTManager{
		secretKey:          []byte(secretKey),
		tokenExpiry:        tokenExpiry,
		refreshTokenExpiry: refreshTokenExpiry,
	}
}

// GenerateToken generates a new JWT token
func (j *JWTManager) GenerateToken(userID, email, role, organization string) (string, error) {
	claims := Claims{
		UserID:       userID,
		Email:        email,
		Role:         role,
		Organization: organization,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Subject:   userID,
			Audience:  []string{"scholarplatforms"},
			Issuer:    "scholarplatforms-auth",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(j.tokenExpiry)),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secretKey)
}

// GenerateServiceToken generates a token for service-to-service communication
func (j *JWTManager) GenerateServiceToken(serviceID string) (string, error) {
	claims := Claims{
		ServiceID: serviceID,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Subject:   serviceID,
			Audience:  []string{"scholarplatforms-services"},
			Issuer:    "scholarplatforms-gateway",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)), // Service tokens expire in 1 hour
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secretKey)
}

// ValidateToken validates and parses a JWT token
func (j *JWTManager) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return j.secretKey, nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}

	// Check if token is expired
	if claims.ExpiresAt.Time.Before(time.Now()) {
		return nil, errors.New("token expired")
	}

	return claims, nil
}

// RefreshToken generates a new token using a refresh token
func (j *JWTManager) RefreshToken(refreshTokenString string) (string, error) {
	claims, err := j.ValidateToken(refreshTokenString)
	if err != nil {
		return "", err
	}

	// Generate new token with same claims but updated expiry
	return j.GenerateToken(claims.UserID, claims.Email, claims.Role, claims.Organization)
}

// ExtractTokenFromHeader extracts the JWT token from Authorization header
func ExtractTokenFromHeader(authHeader string) string {
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		return authHeader[7:]
	}
	return ""
}

// ContextKey is used for storing values in context
type ContextKey string

const (
	UserIDKey       ContextKey = "user_id"
	UserEmailKey    ContextKey = "user_email"
	UserRoleKey     ContextKey = "user_role"
	OrganizationKey ContextKey = "organization"
	ServiceIDKey    ContextKey = "service_id"
	ClaimsKey       ContextKey = "claims"
)

// AddClaimsToContext adds JWT claims to the request context
func AddClaimsToContext(ctx context.Context, claims *Claims) context.Context {
	ctx = context.WithValue(ctx, UserIDKey, claims.UserID)
	ctx = context.WithValue(ctx, UserEmailKey, claims.Email)
	ctx = context.WithValue(ctx, UserRoleKey, claims.Role)
	ctx = context.WithValue(ctx, OrganizationKey, claims.Organization)
	ctx = context.WithValue(ctx, ServiceIDKey, claims.ServiceID)
	ctx = context.WithValue(ctx, ClaimsKey, claims)
	return ctx
}

// GetClaimsFromContext extracts JWT claims from context
func GetClaimsFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(ClaimsKey).(*Claims)
	return claims, ok
}

// GetUserIDFromContext extracts user ID from context
func GetUserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(UserIDKey).(string)
	return userID, ok
}

// GetServiceIDFromContext extracts service ID from context
func GetServiceIDFromContext(ctx context.Context) (string, bool) {
	serviceID, ok := ctx.Value(ServiceIDKey).(string)
	return serviceID, ok
}