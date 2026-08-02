package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWTManagerGenerate(t *testing.T) {
	manager := mustJWTManager(t, []byte(strings.Repeat("s", 32)))

	tests := []struct {
		name          string
		walletAddress string
		ttl           time.Duration
		wantError     bool
	}{
		{name: "generates token", walletAddress: " 0xabc ", ttl: time.Hour},
		{name: "rejects empty wallet", walletAddress: " ", ttl: time.Hour, wantError: true},
		{name: "rejects non-positive TTL", walletAddress: "0xabc", ttl: 0, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token, err := manager.Generate(test.walletAddress, test.ttl)
			if test.wantError {
				if err == nil {
					t.Fatal("Generate() expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Generate() unexpected error: %v", err)
			}
			principal, err := manager.Verify(token)
			if err != nil {
				t.Fatalf("Verify(generated token): %v", err)
			}
			if principal.WalletAddress != strings.TrimSpace(test.walletAddress) {
				t.Errorf("wallet address = %q", principal.WalletAddress)
			}
		})
	}
}

func TestJWTManagerVerify(t *testing.T) {
	secret := []byte(strings.Repeat("s", 32))
	otherSecret := []byte(strings.Repeat("o", 32))
	manager := mustJWTManager(t, secret)
	now := time.Now()

	tests := []struct {
		name       string
		token      func(t *testing.T) string
		wantWallet string
		wantError  bool
	}{
		{
			name: "accepts a valid HS256 token",
			token: func(t *testing.T) string {
				return signClaims(t, secret, jwt.SigningMethodHS256, Claims{
					WalletAddress: "0xvalid",
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
					},
				})
			},
			wantWallet: "0xvalid",
		},
		{
			name: "rejects wrong signature",
			token: func(t *testing.T) string {
				return signClaims(t, otherSecret, jwt.SigningMethodHS256, Claims{
					WalletAddress: "0xinvalid",
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
					},
				})
			},
			wantError: true,
		},
		{
			name: "rejects wrong signing method",
			token: func(t *testing.T) string {
				return signClaims(t, secret, jwt.SigningMethodHS384, Claims{
					WalletAddress: "0xinvalid",
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
					},
				})
			},
			wantError: true,
		},
		{
			name: "rejects expired token",
			token: func(t *testing.T) string {
				return signClaims(t, secret, jwt.SigningMethodHS256, Claims{
					WalletAddress: "0xexpired",
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(now.Add(-time.Hour)),
					},
				})
			},
			wantError: true,
		},
		{
			name: "rejects missing expiration",
			token: func(t *testing.T) string {
				return signClaims(t, secret, jwt.SigningMethodHS256, Claims{WalletAddress: "0xmissing"})
			},
			wantError: true,
		},
		{
			name: "rejects missing wallet claim",
			token: func(t *testing.T) string {
				return signClaims(t, secret, jwt.SigningMethodHS256, Claims{
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
					},
				})
			},
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			principal, err := manager.Verify(test.token(t))
			if test.wantError {
				if !errors.Is(err, ErrInvalidToken) {
					t.Fatalf("Verify() error = %v, want ErrInvalidToken", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Verify() unexpected error: %v", err)
			}
			if principal.WalletAddress != test.wantWallet {
				t.Errorf("wallet address = %q, want %q", principal.WalletAddress, test.wantWallet)
			}
		})
	}
}

func TestPrincipalContext(t *testing.T) {
	want := Principal{WalletAddress: "0xcontext"}
	ctx := ContextWithPrincipal(context.Background(), want)
	got, ok := PrincipalFromContext(ctx)
	if !ok || got != want {
		t.Fatalf("PrincipalFromContext() = %#v, %v; want %#v, true", got, ok, want)
	}
}

func mustJWTManager(t *testing.T, secret []byte) *JWTManager {
	t.Helper()
	manager, err := NewJWTManager(secret)
	if err != nil {
		t.Fatalf("NewJWTManager(): %v", err)
	}
	return manager
}

func signClaims(t *testing.T, secret []byte, method jwt.SigningMethod, claims jwt.Claims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(method, claims).SignedString(secret)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return token
}
