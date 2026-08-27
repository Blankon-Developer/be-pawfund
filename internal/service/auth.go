package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/cache"
	"github.com/ethereum/go-ethereum/common"
	"github.com/spruceid/siwe-go"
)

const authMessageKeyPrefix = "siwe:message:"

var (
	ErrInvalidWalletAddress = errors.New("invalid wallet address")
	ErrInvalidMessage       = errors.New("invalid SIWE message")
	ErrInvalidSignature     = errors.New("invalid Ethereum signature")
	ErrSIWEVerification     = errors.New("SIWE verification failed")
)

type AuthConfig struct {
	Domain         string
	URI            string
	ChainID        int
	MessageTTL     time.Duration
	AccessTokenTTL time.Duration
}

type MessageStore interface {
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	GetDelete(ctx context.Context, key string) ([]byte, error)
}

type AuthProfileRepository interface {
	FindProfileByWalletAddress(ctx context.Context, walletAddress string) (domain.AuthProfile, bool, error)
}

type AccessTokenGenerator interface {
	Generate(walletAddress string, role domain.UserRole, ttl time.Duration) (string, error)
}

type VerifyAuthResult struct {
	AccessToken string
	Address     string
	Profile     *domain.AuthProfile
}

type AuthService struct {
	store          MessageStore
	repository     AuthProfileRepository
	tokenGenerator AccessTokenGenerator
	config         AuthConfig
}

func NewAuthService(
	store MessageStore,
	repository AuthProfileRepository,
	tokenGenerator AccessTokenGenerator,
	config AuthConfig,
) *AuthService {
	return &AuthService{
		store:          store,
		repository:     repository,
		tokenGenerator: tokenGenerator,
		config:         config,
	}
}

func (s *AuthService) CreateMessage(ctx context.Context, walletAddress string) (string, error) {
	walletAddress = strings.TrimSpace(walletAddress)
	if !common.IsHexAddress(walletAddress) {
		return "", ErrInvalidWalletAddress
	}
	walletAddress = common.HexToAddress(walletAddress).Hex()

	nonce := rand.Text()
	now := time.Now().UTC()
	expiresAt := now.Add(s.config.MessageTTL)
	siweMessage, err := siwe.InitMessage(
		s.config.Domain,
		walletAddress,
		s.config.URI,
		nonce,
		map[string]any{
			"statement":      "Sign in to Pawfund.",
			"chainId":        s.config.ChainID,
			"issuedAt":       now.Format(time.RFC3339),
			"expirationTime": expiresAt.Format(time.RFC3339),
		},
	)
	if err != nil {
		return "", fmt.Errorf("service: create SIWE message: %w", err)
	}

	msgStr := siweMessage.String()
	if err := s.store.Set(
		ctx,
		authMessageKeyPrefix+nonce,
		[]byte(msgStr),
		s.config.MessageTTL,
	); err != nil {
		return "", fmt.Errorf("service: store SIWE message: %w", err)
	}

	return msgStr, nil
}

func (s *AuthService) Verify(
	ctx context.Context,
	rawMessage string,
	signature string,
) (VerifyAuthResult, error) {
	if strings.TrimSpace(rawMessage) == "" {
		return VerifyAuthResult{}, ErrInvalidMessage
	}
	signature = strings.TrimSpace(signature)
	if !isValidEthereumSignature(signature) {
		return VerifyAuthResult{}, ErrInvalidSignature
	}

	siweMsg, err := siwe.ParseMessage(rawMessage)
	if err != nil {
		return VerifyAuthResult{}, fmt.Errorf("%w: %v", ErrInvalidMessage, err)
	}
	messageURI := siweMsg.GetURI()
	if messageURI.String() != s.config.URI || siweMsg.GetChainID() != s.config.ChainID {
		return VerifyAuthResult{}, ErrSIWEVerification
	}

	siweDomain := s.config.Domain
	if _, err := siweMsg.Verify(signature, &siweDomain, nil, nil); err != nil {
		return VerifyAuthResult{}, fmt.Errorf("%w: %v", ErrSIWEVerification, err)
	}

	storedMessage, err := s.store.GetDelete(ctx, authMessageKeyPrefix+siweMsg.GetNonce())
	if err != nil {
		if errors.Is(err, cache.ErrMiss) {
			return VerifyAuthResult{}, ErrSIWEVerification
		}
		return VerifyAuthResult{}, fmt.Errorf("service: consume SIWE message: %w", err)
	}
	if string(storedMessage) != rawMessage {
		return VerifyAuthResult{}, ErrSIWEVerification
	}

	address := siweMsg.GetAddress().Hex()
	profile, registered, err := s.repository.FindProfileByWalletAddress(ctx, address)
	if err != nil {
		return VerifyAuthResult{}, fmt.Errorf("service: find auth profile: %w", err)
	}

	var role domain.UserRole
	if registered {
		role = profile.Role
	}
	accessToken, err := s.tokenGenerator.Generate(address, role, s.config.AccessTokenTTL)
	if err != nil {
		return VerifyAuthResult{}, fmt.Errorf("service: generate access token: %w", err)
	}

	result := VerifyAuthResult{
		AccessToken: accessToken,
		Address:     address,
	}
	if registered {
		result.Profile = &profile
	}

	return result, nil
}

func (s *AuthService) GetMe(ctx context.Context, walletAddress string) (domain.AuthProfile, error) {
	profile, exists, err := s.repository.FindProfileByWalletAddress(
		ctx,
		strings.TrimSpace(walletAddress),
	)
	if err != nil {
		return domain.AuthProfile{}, fmt.Errorf("service: find auth profile: %w", err)
	}
	if !exists {
		return domain.AuthProfile{}, ErrProfileNotFound
	}

	return profile, nil
}

func isValidEthereumSignature(signature string) bool {
	if len(signature) != 2+65*2 || !strings.HasPrefix(signature, "0x") {
		return false
	}
	decoded, err := hex.DecodeString(signature[2:])
	return err == nil && len(decoded) == 65
}
