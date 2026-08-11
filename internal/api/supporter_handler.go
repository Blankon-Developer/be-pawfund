package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Blankon-Developer/be-pawfund/internal/auth"
	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/Blankon-Developer/be-pawfund/internal/storage"
)

const (
	maxRegisterSupporterBodyBytes = 1 << 20
	maxReplaceSupporterBodyBytes  = 1 << 20
)

type SupporterService interface {
	Register(ctx context.Context, input service.RegisterSupporterInput) (domain.Supporter, error)
	GetProfile(ctx context.Context, walletAddress string) (domain.Supporter, error)
	ReplaceProfile(ctx context.Context, input service.ReplaceSupporterProfileInput) error
	DeleteProfile(ctx context.Context, walletAddress string) error
}

type SupporterHandler struct {
	service    SupporterService
	urlBuilder *storage.PublicURLBuilder
	httpx.Responder
}

func NewSupporterHandler(
	service SupporterService,
	urlBuilder *storage.PublicURLBuilder,
	logger *slog.Logger,
) *SupporterHandler {
	return &SupporterHandler{
		service:    service,
		urlBuilder: urlBuilder,
		Responder:  httpx.NewResponder(logger),
	}
}

func (h *SupporterHandler) HandleRegisterSupporter(w http.ResponseWriter, r *http.Request) {
	var request registerSupporterRequest
	if err := httpx.ReadJSON(w, r, &request, maxRegisterSupporterBodyBytes); err != nil {
		h.ReadError(w, err, "Request body exceeds the 1 MiB limit.")
		return
	}

	request.normalize()
	if fieldErrors := request.validate(); fieldErrors != nil {
		h.ValidationError(w, fieldErrors)
		return
	}

	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.WalletAddress == "" {
		h.Error(
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
	h.Success(w, http.StatusCreated, "SUPPORTER_REGISTERED", "Supporter account created successfully.", response)
}

func (h *SupporterHandler) HandleGetProfile(w http.ResponseWriter, r *http.Request) {
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

	supporter, err := h.service.GetProfile(r.Context(), walletAddress)
	if err != nil {
		if errors.Is(err, service.ErrProfileNotFound) {
			h.Error(
				w,
				http.StatusNotFound,
				"PROFILE_NOT_FOUND",
				"No supporter profile is registered for the authenticated wallet.",
				nil,
			)
			return
		}

		h.Logger.Error("get supporter profile", "error", err)
		h.InternalError(w)
		return
	}

	response := mySupporterProfileResponse{
		Name:          supporter.Name,
		Email:         supporter.Email,
		WalletAddress: supporter.WalletAddress,
		ImageURL:      h.urlBuilder.Build(supporter.ImageObjectKey),
	}
	h.Success(w, http.StatusOK, "PROFILE_RETRIEVED", "Profile retrieved successfully.", response)
}

func (h *SupporterHandler) HandleReplaceProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var request replaceSupporterProfileRequest
	if err := httpx.ReadJSON(w, r, &request, maxReplaceSupporterBodyBytes); err != nil {
		h.ReadError(w, err, "Request body exceeds the 1 MiB limit.")
		return
	}

	request.normalize()
	if fieldErrors := request.validate(); fieldErrors != nil {
		h.ValidationError(w, fieldErrors)
		return
	}

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

	err := h.service.ReplaceProfile(r.Context(), service.ReplaceSupporterProfileInput{
		WalletAddress: walletAddress,
		Profile:       request.toProfileReplacement(),
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrProfileNotFound):
			h.Error(
				w,
				http.StatusNotFound,
				"PROFILE_NOT_FOUND",
				"No supporter profile is registered for the authenticated wallet.",
				nil,
			)
		default:
			h.handleServiceError(w, err)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *SupporterHandler) HandleDeleteProfile(w http.ResponseWriter, r *http.Request) {
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

	err := h.service.DeleteProfile(r.Context(), walletAddress)
	if err != nil {
		if errors.Is(err, service.ErrProfileNotFound) {
			h.Error(
				w,
				http.StatusNotFound,
				"PROFILE_NOT_FOUND",
				"No supporter profile is registered for the authenticated wallet.",
				nil,
			)
			return
		}

		h.Logger.Error("delete supporter profile", "error", err)
		h.InternalError(w)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *SupporterHandler) handleServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrEmailAlreadyRegistered):
		h.Error(
			w,
			http.StatusConflict,
			"EMAIL_ALREADY_REGISTERED",
			"Email is already registered.",
			httpx.FieldErrors{"email": {"email is already registered!"}},
		)
	case errors.Is(err, service.ErrWalletAlreadyRegistered):
		h.Error(
			w,
			http.StatusConflict,
			"WALLET_ALREADY_REGISTERED",
			"Wallet address is already registered.",
			httpx.FieldErrors{"walletAddress": {"wallet address is already registered!"}},
		)
	default:
		h.Logger.Error("register supporter", "error", err)
		h.InternalError(w)
	}
}
