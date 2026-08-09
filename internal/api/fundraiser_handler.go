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

const maxRegisterFundraiserBodyBytes = 1 << 20

type FundraiserService interface {
	Register(ctx context.Context, input service.RegisterFundraiserInput) (domain.Fundraiser, error)
	GetProfile(ctx context.Context, walletAddress string) (domain.Fundraiser, error)
}

type FundraiserHandler struct {
	service    FundraiserService
	urlBuilder *storage.PublicURLBuilder
	httpx.Responder
}

func NewFundraiserHandler(
	service FundraiserService,
	urlBuilder *storage.PublicURLBuilder,
	logger *slog.Logger,
) *FundraiserHandler {
	return &FundraiserHandler{
		service:    service,
		urlBuilder: urlBuilder,
		Responder:  httpx.NewResponder(logger),
	}
}

func (h *FundraiserHandler) HandleRegisterFundraiser(w http.ResponseWriter, r *http.Request) {
	var request registerFundraiserRequest
	if err := httpx.ReadJSON(w, r, &request, maxRegisterFundraiserBodyBytes); err != nil {
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

	created, err := h.service.Register(r.Context(), service.RegisterFundraiserInput{
		Name:          request.Name,
		Email:         request.Email,
		WalletAddress: principal.WalletAddress,
		ContactPerson: service.ContactPerson{
			Name:  request.ContactPerson.Name,
			Phone: request.ContactPerson.Phone,
		},
		SocialURL:      request.SocialURL,
		Country:        request.Country,
		ZipCode:        request.ZipCode,
		ImageObjectKey: request.ImageObjectKey,
	})
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	response := registerFundraiserResponse{
		Name:  created.Name,
		Email: created.Email,
		ContactPerson: fundraiserContactPerson{
			Name:  created.ContactName,
			Phone: created.ContactPhone,
		},
		SocialURL:     valueOrEmpty(created.SocialURL),
		Country:       created.Country,
		ZipCode:       created.ZipCode,
		ImageURL:      h.urlBuilder.Build(created.ImageObjectKey),
		WalletAddress: created.WalletAddress,
		Role:          created.Role,
	}
	h.Success(w, http.StatusCreated, "FUNDRAISER_REGISTERED", "Fundraiser account created successfully.", response)
}

func (h *FundraiserHandler) HandleGetProfile(w http.ResponseWriter, r *http.Request) {
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

	fundraiser, err := h.service.GetProfile(r.Context(), walletAddress)
	if err != nil {
		if errors.Is(err, service.ErrProfileNotFound) {
			h.Error(
				w,
				http.StatusNotFound,
				"PROFILE_NOT_FOUND",
				"No fundraiser profile is registered for the authenticated wallet.",
				nil,
			)
			return
		}

		h.Logger.Error("get fundraiser profile", "error", err)
		h.InternalError(w)
		return
	}

	response := getFundraiserProfileResponse{
		Name:  fundraiser.Name,
		Email: fundraiser.Email,
		ContactPerson: fundraiserContactPerson{
			Name:  fundraiser.ContactName,
			Phone: fundraiser.ContactPhone,
		},
		SocialURL:     valueOrEmpty(fundraiser.SocialURL),
		Country:       fundraiser.Country,
		ZipCode:       fundraiser.ZipCode,
		ImageURL:      h.urlBuilder.Build(fundraiser.ImageObjectKey),
		WalletAddress: fundraiser.WalletAddress,
	}
	h.Success(w, http.StatusOK, "PROFILE_RETRIEVED", "Profile retrieved successfully.", response)
}

func (h *FundraiserHandler) handleServiceError(w http.ResponseWriter, err error) {
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
		h.Logger.Error("register fundraiser", "error", err)
		h.InternalError(w)
	}
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
