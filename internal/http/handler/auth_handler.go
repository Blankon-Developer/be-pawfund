package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Blankon-Developer/be-pawfund/internal/auth"
	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/http/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/storage"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
)

const maxAuthBodyBytes = 16 << 10

type AuthService interface {
	CreateMessage(ctx context.Context, walletAddress string) (string, error)
	Verify(ctx context.Context, message, signature string) (service.VerifyAuthResult, error)
	GetMe(ctx context.Context, walletAddress string) (domain.AuthProfile, error)
}

type AuthHandler struct {
	service    AuthService
	urlBuilder *storage.PublicURLBuilder
	chainID    int
	httpx.Responder
}

func NewAuthHandler(
	service AuthService,
	urlBuilder *storage.PublicURLBuilder,
	chainID int,
	logger *slog.Logger,
) *AuthHandler {
	return &AuthHandler{
		service:    service,
		urlBuilder: urlBuilder,
		chainID:    chainID,
		Responder:  httpx.NewResponder(logger),
	}
}

func (h *AuthHandler) HandleCreateMessage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var request createAuthMessageRequest
	if err := httpx.ReadJSON(w, r, &request, maxAuthBodyBytes); err != nil {
		h.ReadError(w, err, "Request body exceeds the 16 KiB limit.")
		return
	}

	request.normalize()
	if fieldErrors := request.validate(); fieldErrors != nil {
		h.ValidationError(w, fieldErrors)
		return
	}

	msg, err := h.service.CreateMessage(r.Context(), request.Address)
	if err != nil {
		if errors.Is(err, service.ErrInvalidWalletAddress) {
			h.ValidationError(w, httpx.FieldErrors{"address": {"address must be a valid Ethereum address!"}})
			return
		}
		h.Logger.Error("create auth message", "error", err)
		h.InternalError(w)
		return
	}

	h.Success(
		w,
		http.StatusOK,
		"AUTH_MESSAGE_CREATED",
		"Authentication message created successfully.",
		createAuthMessageResponse{Message: msg},
	)
}

func (h *AuthHandler) HandleVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var request verifyAuthRequest
	if err := httpx.ReadJSON(w, r, &request, maxAuthBodyBytes); err != nil {
		h.ReadError(w, err, "Request body exceeds the 16 KiB limit.")
		return
	}

	request.normalize()
	if fieldErrors := request.validate(); fieldErrors != nil {
		h.ValidationError(w, fieldErrors)
		return
	}

	result, err := h.service.Verify(r.Context(), request.Message, request.Signature)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidMessage):
			h.ValidationError(w, httpx.FieldErrors{"message": {"message must be a valid SIWE message!"}})
		case errors.Is(err, service.ErrInvalidSignature):
			h.ValidationError(w, httpx.FieldErrors{"signature": {"signature must be a valid 65-byte Ethereum signature!"}})
		case errors.Is(err, service.ErrSIWEVerification):
			h.Error(
				w,
				http.StatusUnauthorized,
				"SIWE_VERIFICATION_FAILED",
				"The SIWE message or signature is invalid or expired.",
				nil,
			)
		default:
			h.Logger.Error("verify auth message", "error", err)
			h.InternalError(w)
		}
		return
	}

	response := verifyAuthResponse{
		AccessToken:     result.AccessToken,
		IsNotRegistered: result.Profile == nil,
		Address:         result.Address,
		ChainID:         h.chainID,
	}
	if result.Profile != nil {
		response.Name = &result.Profile.Name
		response.Role = &result.Profile.Role
		response.ImageURL = h.urlBuilder.Build(result.Profile.ImageObjectKey)
	}

	h.Success(w, http.StatusOK, "AUTH_VERIFIED", "Wallet signature verified successfully.", response)
}

func (h *AuthHandler) HandleGetMe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	principal, ok := auth.PrincipalFromContext(r.Context())
	walletAddress := strings.TrimSpace(principal.WalletAddress)
	if !ok || walletAddress == "" {
		w.Header().Set("WWW-Authenticate", "Bearer")
		h.Error(
			w,
			http.StatusUnauthorized,
			"INVALID_ACCESS_TOKEN",
			"The access token is invalid or expired.",
			nil,
		)
		return
	}

	profile, err := h.service.GetMe(r.Context(), walletAddress)
	if err != nil {
		if !errors.Is(err, service.ErrProfileNotFound) {
			h.Logger.Error("get authenticated profile", "error", err)
			h.InternalError(w)
			return
		}

		h.Success(
			w,
			http.StatusOK,
			"PROFILE_RETRIEVED",
			"Profile retrieved successfully.",
			getMeResponse{
				IsNotRegistered: true,
				Address:         walletAddress,
				ChainID:         h.chainID,
			},
		)
		return
	}

	response := getMeResponse{
		Address:  walletAddress,
		Name:     &profile.Name,
		Role:     &profile.Role,
		ImageURL: h.urlBuilder.Build(profile.ImageObjectKey),
		ChainID:  h.chainID,
	}
	h.Success(w, http.StatusOK, "PROFILE_RETRIEVED", "Profile retrieved successfully.", response)
}
