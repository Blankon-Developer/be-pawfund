package service

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/cache"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/spruceid/siwe-go"
)

type authStoreStub struct {
	values       map[string][]byte
	setErr       error
	getDeleteErr error
	setKey       string
	setTTL       time.Duration
	getDeleteKey string
}

func (s *authStoreStub) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if s.setErr != nil {
		return s.setErr
	}
	if s.values == nil {
		s.values = make(map[string][]byte)
	}
	s.setKey = key
	s.setTTL = ttl
	s.values[key] = append([]byte(nil), value...)
	return nil
}

func (s *authStoreStub) GetDelete(_ context.Context, key string) ([]byte, error) {
	s.getDeleteKey = key
	if s.getDeleteErr != nil {
		return nil, s.getDeleteErr
	}
	value, ok := s.values[key]
	if !ok {
		return nil, cache.ErrMiss
	}
	delete(s.values, key)
	return append([]byte(nil), value...), nil
}

type authProfileRepositoryStub struct {
	profile    domain.AuthProfile
	registered bool
	err        error
	address    string
}

func (r *authProfileRepositoryStub) FindProfileByWalletAddress(
	_ context.Context,
	walletAddress string,
) (domain.AuthProfile, bool, error) {
	r.address = walletAddress
	return r.profile, r.registered, r.err
}

type authTokenGeneratorStub struct {
	token   string
	err     error
	address string
	role    domain.UserRole
	ttl     time.Duration
}

func (g *authTokenGeneratorStub) Generate(
	walletAddress string,
	role domain.UserRole,
	ttl time.Duration,
) (string, error) {
	g.address = walletAddress
	g.role = role
	g.ttl = ttl
	return g.token, g.err
}

func TestAuthServiceCreateMessage(t *testing.T) {
	privateKey := mustPrivateKey(t)
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()

	tests := []struct {
		name      string
		address   string
		storeErr  error
		wantError error
	}{
		{name: "creates message", address: strings.ToLower(address)},
		{name: "rejects invalid address", address: "0xinvalid", wantError: ErrInvalidWalletAddress},
		{name: "maps cache failure", address: address, storeErr: errors.New("cache unavailable")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &authStoreStub{values: make(map[string][]byte), setErr: test.storeErr}
			service := newTestAuthService(store, &authProfileRepositoryStub{}, &authTokenGeneratorStub{})

			msgStr, err := service.CreateMessage(t.Context(), test.address)
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("CreateMessage() error = %v, want %v", err, test.wantError)
				}
				return
			}
			if test.storeErr != nil {
				if err == nil || !strings.Contains(err.Error(), "store SIWE message") {
					t.Fatalf("CreateMessage() error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateMessage() unexpected error: %v", err)
			}

			message, err := siwe.ParseMessage(msgStr)
			if err != nil {
				t.Fatalf("parse message: %v", err)
			}
			uri := message.GetURI()
			if message.GetDomain() != testAuthConfig().Domain || uri.String() != testAuthConfig().URI {
				t.Errorf("SIWE origin = %q/%q", message.GetDomain(), uri.String())
			}
			if message.GetChainID() != testAuthConfig().ChainID {
				t.Errorf("chain ID = %d", message.GetChainID())
			}
			if message.GetAddress() != common.HexToAddress(address) {
				t.Errorf("message address = %s, want %s", message.GetAddress(), address)
			}
			if store.setKey != authMessageKeyPrefix+message.GetNonce() {
				t.Errorf("cache key = %q", store.setKey)
			}
			if store.setTTL != testAuthConfig().MessageTTL {
				t.Errorf("cache TTL = %v", store.setTTL)
			}
			if string(store.values[store.setKey]) != msgStr {
				t.Errorf("stored message does not match response")
			}
			if expiration := message.GetExpirationTime(); expiration == nil {
				t.Error("message has no expiration time")
			} else if expiresAt, err := time.Parse(time.RFC3339, *expiration); err != nil {
				t.Errorf("parse expiration: %v", err)
			} else if remaining := time.Until(expiresAt); remaining < 4*time.Minute || remaining > 6*time.Minute {
				t.Errorf("message expiration remaining = %v", remaining)
			}
		})
	}
}

func TestAuthServiceVerify(t *testing.T) {
	imageKey := "profiles/cat.png"
	profile := domain.AuthProfile{
		Name:           "Cat Lover",
		Role:           domain.UserRoleSupporter,
		ImageObjectKey: &imageKey,
	}

	tests := []struct {
		name                 string
		profile              domain.AuthProfile
		registered           bool
		repositoryErr        error
		tokenErr             error
		storeErr             error
		prepare              func(t *testing.T, fixture *authVerifyFixture)
		wantError            error
		wantProfile          bool
		wantTokenRole        domain.UserRole
		wantMessageRemaining bool
	}{
		{name: "verifies unregistered wallet"},
		{
			name:          "returns registered profile",
			profile:       profile,
			registered:    true,
			wantProfile:   true,
			wantTokenRole: domain.UserRoleSupporter,
		},
		{
			name: "rejects malformed message",
			prepare: func(_ *testing.T, fixture *authVerifyFixture) {
				fixture.message = "not a SIWE message"
			},
			wantError:            ErrInvalidMessage,
			wantMessageRemaining: true,
		},
		{
			name: "rejects malformed signature without consuming message",
			prepare: func(_ *testing.T, fixture *authVerifyFixture) {
				fixture.signature = "0x1234"
			},
			wantError:            ErrInvalidSignature,
			wantMessageRemaining: true,
		},
		{
			name: "rejects wrong signer without consuming message",
			prepare: func(t *testing.T, fixture *authVerifyFixture) {
				fixture.signature = signMessage(t, fixture.message, mustPrivateKey(t))
			},
			wantError:            ErrSIWEVerification,
			wantMessageRemaining: true,
		},
		{
			name: "rejects mismatched domain without consuming message",
			prepare: func(_ *testing.T, fixture *authVerifyFixture) {
				fixture.service.config.Domain = "other.example.com"
			},
			wantError:            ErrSIWEVerification,
			wantMessageRemaining: true,
		},
		{
			name: "rejects mismatched chain without consuming message",
			prepare: func(_ *testing.T, fixture *authVerifyFixture) {
				fixture.service.config.ChainID = 1
			},
			wantError:            ErrSIWEVerification,
			wantMessageRemaining: true,
		},
		{
			name: "rejects missing message",
			prepare: func(_ *testing.T, fixture *authVerifyFixture) {
				clear(fixture.store.values)
			},
			wantError: ErrSIWEVerification,
		},
		{
			name: "rejects stored message mismatch",
			prepare: func(_ *testing.T, fixture *authVerifyFixture) {
				fixture.store.values[fixture.store.setKey] = []byte("different")
			},
			wantError: ErrSIWEVerification,
		},
		{
			name:                 "maps cache failure",
			storeErr:             errors.New("cache unavailable"),
			wantError:            nil,
			wantMessageRemaining: true,
		},
		{
			name:          "maps repository failure",
			repositoryErr: errors.New("database unavailable"),
			wantError:     nil,
		},
		{
			name:      "maps token failure",
			tokenErr:  errors.New("signing failed"),
			wantError: nil,
		},
		{
			name: "rejects replay",
			prepare: func(t *testing.T, fixture *authVerifyFixture) {
				if _, err := fixture.service.Verify(t.Context(), fixture.message, fixture.signature); err != nil {
					t.Fatalf("first Verify() error: %v", err)
				}
			},
			wantError: ErrSIWEVerification,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAuthVerifyFixture(t, test.profile, test.registered)
			fixture.repository.err = test.repositoryErr
			fixture.tokenGenerator.err = test.tokenErr
			if test.storeErr != nil {
				fixture.store.getDeleteErr = test.storeErr
			}
			if test.prepare != nil {
				test.prepare(t, fixture)
			}

			result, err := fixture.service.Verify(t.Context(), fixture.message, fixture.signature)
			switch {
			case test.wantError != nil:
				if !errors.Is(err, test.wantError) {
					t.Fatalf("Verify() error = %v, want %v", err, test.wantError)
				}
			case test.storeErr != nil:
				if err == nil || !strings.Contains(err.Error(), "consume SIWE message") {
					t.Fatalf("Verify() error = %v", err)
				}
			case test.repositoryErr != nil:
				if err == nil || !strings.Contains(err.Error(), "find auth profile") {
					t.Fatalf("Verify() error = %v", err)
				}
			case test.tokenErr != nil:
				if err == nil || !strings.Contains(err.Error(), "generate access token") {
					t.Fatalf("Verify() error = %v", err)
				}
			default:
				if err != nil {
					t.Fatalf("Verify() unexpected error: %v", err)
				}
				wantAddress := crypto.PubkeyToAddress(fixture.privateKey.PublicKey).Hex()
				if result.AccessToken != "access-token" || result.Address != wantAddress {
					t.Errorf("result = %#v", result)
				}
				if (result.Profile != nil) != test.wantProfile {
					t.Errorf("profile = %#v", result.Profile)
				}
				if fixture.repository.address != wantAddress {
					t.Errorf("repository address = %q, want %q", fixture.repository.address, wantAddress)
				}
				if fixture.tokenGenerator.ttl != testAuthConfig().AccessTokenTTL {
					t.Errorf("token TTL = %v", fixture.tokenGenerator.ttl)
				}
				if fixture.tokenGenerator.role != test.wantTokenRole {
					t.Errorf("token role = %q, want %q", fixture.tokenGenerator.role, test.wantTokenRole)
				}
			}

			_, messageRemaining := fixture.store.values[fixture.store.setKey]
			if messageRemaining != test.wantMessageRemaining {
				t.Errorf("message remaining = %v, want %v", messageRemaining, test.wantMessageRemaining)
			}
		})
	}
}

func TestAuthServiceGetMe(t *testing.T) {
	imageKey := "profiles/cat.png"
	profile := domain.AuthProfile{
		Name:           "Cat Lover",
		Role:           domain.UserRoleSupporter,
		ImageObjectKey: &imageKey,
	}
	repositoryFailure := errors.New("database unavailable")

	tests := []struct {
		name          string
		walletAddress string
		profile       domain.AuthProfile
		registered    bool
		repositoryErr error
		wantError     error
		wantWrapped   error
		wantAddress   string
	}{
		{
			name:          "returns profile for wallet",
			walletAddress: " 0x1234567890123456789012345678901234567890 ",
			profile:       profile,
			registered:    true,
			wantAddress:   "0x1234567890123456789012345678901234567890",
		},
		{
			name:          "returns not found for unregistered wallet",
			walletAddress: "0x2234567890123456789012345678901234567890",
			wantError:     ErrProfileNotFound,
			wantAddress:   "0x2234567890123456789012345678901234567890",
		},
		{
			name:          "wraps repository failure",
			walletAddress: "0x3234567890123456789012345678901234567890",
			repositoryErr: repositoryFailure,
			wantWrapped:   repositoryFailure,
			wantAddress:   "0x3234567890123456789012345678901234567890",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &authProfileRepositoryStub{
				profile:    test.profile,
				registered: test.registered,
				err:        test.repositoryErr,
			}
			service := newTestAuthService(&authStoreStub{}, repository, &authTokenGeneratorStub{})

			result, err := service.GetMe(t.Context(), test.walletAddress)
			switch {
			case test.wantError != nil:
				if !errors.Is(err, test.wantError) {
					t.Fatalf("GetMe() error = %v, want %v", err, test.wantError)
				}
			case test.wantWrapped != nil:
				if !errors.Is(err, test.wantWrapped) || !strings.Contains(err.Error(), "find auth profile") {
					t.Fatalf("GetMe() error = %v, want wrapped %v", err, test.wantWrapped)
				}
			default:
				if err != nil {
					t.Fatalf("GetMe() unexpected error: %v", err)
				}
				if result != profile {
					t.Errorf("GetMe() result = %#v, want %#v", result, profile)
				}
			}
			if repository.address != test.wantAddress {
				t.Errorf("repository address = %q, want %q", repository.address, test.wantAddress)
			}
		})
	}
}

type authVerifyFixture struct {
	service        *AuthService
	store          *authStoreStub
	repository     *authProfileRepositoryStub
	tokenGenerator *authTokenGeneratorStub
	privateKey     *ecdsa.PrivateKey
	message        string
	signature      string
}

func newAuthVerifyFixture(
	t *testing.T,
	profile domain.AuthProfile,
	registered bool,
) *authVerifyFixture {
	t.Helper()
	store := &authStoreStub{values: make(map[string][]byte)}
	repository := &authProfileRepositoryStub{profile: profile, registered: registered}
	tokenGenerator := &authTokenGeneratorStub{token: "access-token"}
	service := newTestAuthService(store, repository, tokenGenerator)
	privateKey := mustPrivateKey(t)
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	msg, err := service.CreateMessage(t.Context(), address)
	if err != nil {
		t.Fatalf("CreateMessage(): %v", err)
	}

	return &authVerifyFixture{
		service:        service,
		store:          store,
		repository:     repository,
		tokenGenerator: tokenGenerator,
		privateKey:     privateKey,
		message:        msg,
		signature:      signMessage(t, msg, privateKey),
	}
}

func newTestAuthService(
	store MessageStore,
	repository AuthProfileRepository,
	tokenGenerator AccessTokenGenerator,
) *AuthService {
	return NewAuthService(store, repository, tokenGenerator, testAuthConfig())
}

func testAuthConfig() AuthConfig {
	return AuthConfig{
		Domain:         "app.example.com",
		URI:            "https://app.example.com/login",
		ChainID:        84532,
		MessageTTL:     5 * time.Minute,
		AccessTokenTTL: 24 * time.Hour,
	}
}

func mustPrivateKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	return privateKey
}

func signMessage(t *testing.T, message string, privateKey *ecdsa.PrivateKey) string {
	t.Helper()
	payload := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
	hash := crypto.Keccak256Hash([]byte(payload))
	signature, err := crypto.Sign(hash.Bytes(), privateKey)
	if err != nil {
		t.Fatalf("sign message: %v", err)
	}
	signature[64] += 27
	return "0x" + hex.EncodeToString(signature)
}
