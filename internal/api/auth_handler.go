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
	logger     *slog.Logger
}

func NewAuthHandler(
	service AuthService,
	urlBuilder *storage.PublicURLBuilder,
	logger *slog.Logger,
) *AuthHandler {
	return &AuthHandler{service: service, urlBuilder: urlBuilder, logger: logger}
}

func (h *AuthHandler) HandleCreateMessage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var request createAuthMessageRequest
	if err := httpx.ReadJSON(w, r, &request, maxAuthBodyBytes); err != nil {
		h.handleReadError(w, err)
		return
	}
	request.Address = strings.TrimSpace(request.Address)
	if request.Address == "" {
		h.writeValidationError(w, httpx.FieldErrors{"address": {"address is required!"}})
		return
	}

	msg, err := h.service.CreateMessage(r.Context(), request.Address)
	if err != nil {
		if errors.Is(err, service.ErrInvalidWalletAddress) {
			h.writeValidationError(w, httpx.FieldErrors{"address": {"address must be a valid Ethereum address!"}})
			return
		}
		h.logger.Error("create auth message", "error", err)
		h.writeInternalError(w)
		return
	}

	if err := httpx.WriteSuccess(
		w,
		http.StatusOK,
		"AUTH_MESSAGE_CREATED",
		"Authentication message created successfully.",
		createAuthMessageResponse{Message: msg},
	); err != nil {
		h.logger.Error("write create auth message response", "error", err)
	}
}

func (h *AuthHandler) HandleVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var request verifyAuthRequest
	if err := httpx.ReadJSON(w, r, &request, maxAuthBodyBytes); err != nil {
		h.handleReadError(w, err)
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
		h.writeValidationError(w, fieldErrors)
		return
	}

	result, err := h.service.Verify(r.Context(), request.Message, request.Signature)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidMessage):
			h.writeValidationError(w, httpx.FieldErrors{"message": {"message must be a valid SIWE message!"}})
		case errors.Is(err, service.ErrInvalidSignature):
			h.writeValidationError(w, httpx.FieldErrors{"signature": {"signature must be a valid 65-byte Ethereum signature!"}})
		case errors.Is(err, service.ErrSIWEVerification):
			h.writeError(
				w,
				http.StatusUnauthorized,
				"SIWE_VERIFICATION_FAILED",
				"The SIWE message or signature is invalid or expired.",
				nil,
			)
		default:
			h.logger.Error("verify auth message", "error", err)
			h.writeInternalError(w)
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

	if err := httpx.WriteSuccess(
		w,
		http.StatusOK,
		"AUTH_VERIFIED",
		"Wallet signature verified successfully.",
		response,
	); err != nil {
		h.logger.Error("write verify auth response", "error", err)
	}
}

func (h *AuthHandler) handleReadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, httpx.ErrUnsupportedMediaType):
		h.writeError(
			w,
			http.StatusUnsupportedMediaType,
			"UNSUPPORTED_MEDIA_TYPE",
			"Content-Type must be application/json.",
			nil,
		)
	case errors.Is(err, httpx.ErrBodyTooLarge):
		h.writeError(
			w,
			http.StatusRequestEntityTooLarge,
			"REQUEST_TOO_LARGE",
			"Request body exceeds the 16 KiB limit.",
			nil,
		)
	default:
		h.logger.Debug("invalid auth request", "error", err)
		h.writeError(
			w,
			http.StatusBadRequest,
			"INVALID_REQUEST",
			"Request body must contain one valid JSON object.",
			nil,
		)
	}
}

func (h *AuthHandler) writeValidationError(w http.ResponseWriter, fieldErrors httpx.FieldErrors) {
	h.writeError(
		w,
		http.StatusUnprocessableEntity,
		"VALIDATION_ERROR",
		"One or more fields are invalid.",
		fieldErrors,
	)
}

func (h *AuthHandler) writeInternalError(w http.ResponseWriter) {
	h.writeError(
		w,
		http.StatusInternalServerError,
		"INTERNAL_SERVER_ERROR",
		"An internal server error occurred.",
		nil,
	)
}

func (h *AuthHandler) writeError(
	w http.ResponseWriter,
	status int,
	code string,
	message string,
	fieldErrors httpx.FieldErrors,
) {
	if err := httpx.WriteError(w, status, code, message, fieldErrors); err != nil {
		h.logger.Error("write auth error response", "code", code, "error", err)
	}
}
