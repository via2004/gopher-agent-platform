package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestNewTokenManagerRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		issuer string
		ttl    time.Duration
	}{
		{name: "short secret", secret: "too-short", issuer: "gopher-agent-platform", ttl: time.Hour},
		{name: "empty issuer", secret: testSecret, issuer: "   ", ttl: time.Hour},
		{name: "zero ttl", secret: testSecret, issuer: "gopher-agent-platform", ttl: 0},
		{name: "negative ttl", secret: testSecret, issuer: "gopher-agent-platform", ttl: -time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewTokenManager(tt.secret, tt.issuer, tt.ttl)
			if err == nil {
				t.Fatal("NewTokenManager() error = nil")
			}
			if manager != nil {
				t.Fatalf("NewTokenManager() manager = %#v, want nil", manager)
			}
		})
	}
}

func TestTokenManagerIssueAndVerify(t *testing.T) {
	manager := newTestTokenManager(t, testSecret, "gopher-agent-platform", time.Hour)

	tokenString, err := manager.Issue(42)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if strings.Count(tokenString, ".") != 2 {
		t.Fatalf("Issue() token = %q, want three JWT segments", tokenString)
	}

	userID, err := manager.Verify(tokenString)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if userID != 42 {
		t.Fatalf("Verify() user ID = %d, want 42", userID)
	}
}

func TestTokenManagerIssueRejectsZeroUserID(t *testing.T) {
	manager := newTestTokenManager(t, testSecret, "gopher-agent-platform", time.Hour)

	tokenString, err := manager.Issue(0)
	if err == nil {
		t.Fatal("Issue() error = nil")
	}
	if tokenString != "" {
		t.Fatalf("Issue() token = %q, want empty", tokenString)
	}
}

func TestTokenManagerVerifyRejectsInvalidToken(t *testing.T) {
	manager := newTestTokenManager(t, testSecret, "gopher-agent-platform", time.Hour)

	validToken, err := manager.Issue(42)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	otherSecretManager := newTestTokenManager(t,
		"abcdef0123456789abcdef0123456789",
		"gopher-agent-platform",
		time.Hour,
	)
	otherIssuerManager := newTestTokenManager(t, testSecret, "another-service", time.Hour)

	tests := []struct {
		name        string
		manager     *TokenManager
		tokenString string
	}{
		{name: "empty token", manager: manager, tokenString: ""},
		{name: "malformed token", manager: manager, tokenString: "not-a-jwt"},
		{name: "wrong secret", manager: otherSecretManager, tokenString: validToken},
		{name: "wrong issuer", manager: otherIssuerManager, tokenString: validToken},
		{name: "expired token", manager: manager, tokenString: signRegisteredClaims(t, manager, jwt.RegisteredClaims{
			Subject:   "42",
			Issuer:    manager.issuer,
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		})},
		{name: "missing expiration", manager: manager, tokenString: signRegisteredClaims(t, manager, jwt.RegisteredClaims{
			Subject:  "42",
			Issuer:   manager.issuer,
			IssuedAt: jwt.NewNumericDate(time.Now()),
		})},
		{name: "invalid subject", manager: manager, tokenString: signRegisteredClaims(t, manager, jwt.RegisteredClaims{
			Subject:   "not-a-user-id",
			Issuer:    manager.issuer,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		})},
		{name: "zero subject", manager: manager, tokenString: signRegisteredClaims(t, manager, jwt.RegisteredClaims{
			Subject:   "0",
			Issuer:    manager.issuer,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		})},
		{name: "wrong algorithm", manager: manager, tokenString: signWithMethod(t, manager, jwt.SigningMethodHS384)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID, err := tt.manager.Verify(tt.tokenString)
			if err == nil {
				t.Fatal("Verify() error = nil")
			}
			if userID != 0 {
				t.Fatalf("Verify() user ID = %d, want 0", userID)
			}
		})
	}
}

func newTestTokenManager(t *testing.T, secret, issuer string, ttl time.Duration) *TokenManager {
	t.Helper()
	manager, err := NewTokenManager(secret, issuer, ttl)
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}
	return manager
}

func signRegisteredClaims(t *testing.T, manager *TokenManager, claims jwt.RegisteredClaims) string {
	t.Helper()
	tokenString, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(manager.secret)
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return tokenString
}

func signWithMethod(t *testing.T, manager *TokenManager, method jwt.SigningMethod) string {
	t.Helper()
	claims := jwt.RegisteredClaims{
		Subject:   "42",
		Issuer:    manager.issuer,
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	tokenString, err := jwt.NewWithClaims(method, claims).SignedString(manager.secret)
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return tokenString
}
