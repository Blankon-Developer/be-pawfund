package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Blankon-Developer/be-pawfund/internal/auth"
	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/Blankon-Developer/be-pawfund/internal/storage"
)

const maxRegisterSupporterBodyBytes = 1 << 20

type SupporterRegistrar interface {
	Register(ctx context.Context, input service.RegisterSupporterInput) (domain.Supporter, error)
}

type SupporterHandler struct {
	service    SupporterRegistrar
	urlBuilder *storage.PublicURLBuilder
	logger     *slog.Logger
}

func NewSupporterHandler(
	service SupporterRegistrar,
	urlBuilder *storage.PublicURLBuilder,
	logger *slog.Logger,
) *SupporterHandler {
	return &SupporterHandler{
		service:    service,
		urlBuilder: urlBuilder,
		logger:     logger,
	}
}

func (h *SupporterHandler) HandleRegisterSupporter(w http.ResponseWriter, r *http.Request) {
	var request registerSupporterRequest
	if err := httpx.ReadJSON(w, r, &request, maxRegisterSupporterBodyBytes); err != nil {
		h.handleReadError(w, err)
		return
	}

	request.normalize()
	if fieldErrors := request.validate(); fieldErrors != nil {
		h.writeError(
			w,
			http.StatusUnprocessableEntity,
			"VALIDATION_ERROR",
			"One or more fields are invalid.",
			fieldErrors,
		)
		return
	}

	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.WalletAddress == "" {
		h.writeError(
			w,
			http.StatusUnauthorized,
			"INVALID_ACCESS_TOKEN",
			"The access token is invalid or expired.",
			nil,
		)
		return
	}

	created, err := h.service.Register(r.Context(), service.RegisterSupporterInput{
		Name:           request.Name,
		Email:          request.Email,
		WalletAddress:  principal.WalletAddress,
		ImageObjectKey: request.ImageObjectKey,
	})
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	response := registerSupporterResponse{
		Name:          created.Name,
		Email:         created.Email,
		WalletAddress: created.WalletAddress,
		ImageURL:      h.urlBuilder.Build(created.ImageObjectKey),
		Role:          created.Role,
	}
	if err := httpx.WriteSuccess(
		w,
		http.StatusCreated,
		"SUPPORTER_REGISTERED",
		"Supporter account created successfully.",
		response,
	); err != nil {
		h.logger.Error("write register supporter response", "error", err)
	}
}

func (h *SupporterHandler) handleReadError(w http.ResponseWriter, err error) {
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
			"Request body exceeds the 1 MiB limit.",
			nil,
		)
	default:
		h.logger.Debug("invalid register supporter request", "error", err)
		h.writeError(
			w,
			http.StatusBadRequest,
			"INVALID_REQUEST",
			"Request body must contain one valid JSON object.",
			nil,
		)
	}
}

func (h *SupporterHandler) handleServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrEmailAlreadyRegistered):
		h.writeError(
			w,
			http.StatusConflict,
			"EMAIL_ALREADY_REGISTERED",
			"Email is already registered.",
			httpx.FieldErrors{"email": {"email is already registered!"}},
		)
	case errors.Is(err, service.ErrWalletAlreadyRegistered):
		h.writeError(
			w,
			http.StatusConflict,
			"WALLET_ALREADY_REGISTERED",
			"Wallet address is already registered.",
			httpx.FieldErrors{"walletAddress": {"wallet address is already registered!"}},
		)
	default:
		h.logger.Error("register supporter", "error", err)
		h.writeError(
			w,
			http.StatusInternalServerError,
			"INTERNAL_SERVER_ERROR",
			"An internal server error occurred.",
			nil,
		)
	}
}

func (h *SupporterHandler) writeError(
	w http.ResponseWriter,
	status int,
	code string,
	message string,
	fieldErrors httpx.FieldErrors,
) {
	if err := httpx.WriteError(w, status, code, message, fieldErrors); err != nil {
		h.logger.Error("write error response", "code", code, "error", err)
	}
}
