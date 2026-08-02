package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var ErrInvalidToken = errors.New("invalid access token")

type Claims struct {
	WalletAddress string `json:"wallet_address"`
	jwt.RegisteredClaims
}

type JWTManager struct {
	secret []byte
}

func NewJWTManager(secret []byte) (*JWTManager, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("auth: JWT secret must contain at least 32 bytes")
	}

	secretCopy := append([]byte(nil), secret...)
	return &JWTManager{secret: secretCopy}, nil
}

func (m *JWTManager) Generate(walletAddress string, ttl time.Duration) (string, error) {
	walletAddress = strings.TrimSpace(walletAddress)
	if walletAddress == "" {
		return "", fmt.Errorf("auth: wallet address is required")
	}
	if ttl <= 0 {
		return "", fmt.Errorf("auth: token TTL must be positive")
	}

	now := time.Now()
	claims := Claims{
		WalletAddress: walletAddress,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

func (m *JWTManager) Verify(tokenString string) (Principal, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(*jwt.Token) (any, error) { return m.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return Principal{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	claims.WalletAddress = strings.TrimSpace(claims.WalletAddress)
	if claims.WalletAddress == "" {
		return Principal{}, fmt.Errorf("%w: wallet_address claim is required", ErrInvalidToken)
	}

	return Principal{WalletAddress: claims.WalletAddress}, nil
}
