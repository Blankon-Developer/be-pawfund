package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/Blankon-Developer/be-pawfund/internal/storage"
)

const maxAuthBodyBytes = 16 << 10

type AuthService interface {
	CreateMessage(ctx context.Context, walletAddress string) (string, error)
	Verify(ctx context.Context, message, signature string) (service.VerifyAuthResult, error)
}

type AuthHandler struct {
	service    AuthService
	urlBuilder *storage.PublicURLBuilder
	httpx.Responder
}

func NewAuthHandler(
	service AuthService,
	urlBuilder *storage.PublicURLBuilder,
	logger *slog.Logger,
) *AuthHandler {
	return &AuthHandler{service: service, urlBuilder: urlBuilder, Responder: httpx.NewResponder(logger)}
}

func (h *AuthHandler) HandleCreateMessage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var request createAuthMessageRequest
	if err := httpx.ReadJSON(w, r, &request, maxAuthBodyBytes); err != nil {
		h.ReadError(w, err, "Request body exceeds the 16 KiB limit.")
		return
	}
	request.Address = strings.TrimSpace(request.Address)
	if request.Address == "" {
		h.ValidationError(w, httpx.FieldErrors{"address": {"address is required!"}})
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

	fieldErrors := make(httpx.FieldErrors)
	if strings.TrimSpace(request.Message) == "" {
		fieldErrors.Add("message", "message is required!")
	}
	request.Signature = strings.TrimSpace(request.Signature)
	if request.Signature == "" {
		fieldErrors.Add("signature", "signature is required!")
	}
	if len(fieldErrors) != 0 {
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
	}
	if result.Profile != nil {
		response.Name = &result.Profile.Name
		response.Role = &result.Profile.Role
		response.ImageURL = h.urlBuilder.Build(result.Profile.ImageObjectKey)
	}

	h.Success(w, http.StatusOK, "AUTH_VERIFIED", "Wallet signature verified successfully.", response)
}
