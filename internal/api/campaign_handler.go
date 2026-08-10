package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/auth"
	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/Blankon-Developer/be-pawfund/internal/storage"
)

const (
	maxCreateCampaignBodyBytes = 1 << 20
	maxIdempotencyKeyBytes     = 255
)

type CampaignService interface {
	Create(ctx context.Context, input service.CreateCampaignInput) (domain.Campaign, error)
}

type CampaignHandler struct {
	service    CampaignService
	urlBuilder *storage.PublicURLBuilder
	httpx.Responder
}

func NewCampaignHandler(
	service CampaignService,
	urlBuilder *storage.PublicURLBuilder,
	logger *slog.Logger,
) *CampaignHandler {
	return &CampaignHandler{
		service:    service,
		urlBuilder: urlBuilder,
		Responder:  httpx.NewResponder(logger),
	}
}

func (h *CampaignHandler) HandleCreateCampaign(w http.ResponseWriter, r *http.Request) {
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
	if principal.Role != domain.UserRoleFundraiser {
		h.Error(
			w,
			http.StatusForbidden,
			"FUNDRAISER_ACCESS_REQUIRED",
			"A registered fundraiser account is required.",
			nil,
		)
		return
	}

	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		h.Error(
			w,
			http.StatusBadRequest,
			"IDEMPOTENCY_KEY_REQUIRED",
			"Idempotency-Key header is required.",
			nil,
		)
		return
	}
	if len(idempotencyKey) > maxIdempotencyKeyBytes {
		h.Error(
			w,
			http.StatusBadRequest,
			"INVALID_IDEMPOTENCY_KEY",
			"Idempotency-Key must not exceed 255 bytes.",
			nil,
		)
		return
	}

	var request createCampaignRequest
	if err := httpx.ReadJSON(w, r, &request, maxCreateCampaignBodyBytes); err != nil {
		h.ReadError(w, err, "Request body exceeds the 1 MiB limit.")
		return
	}
	request.normalize()
	fieldErrors, endAt := request.validate()
	if fieldErrors != nil {
		h.ValidationError(w, fieldErrors)
		return
	}

	created, err := h.service.Create(r.Context(), service.CreateCampaignInput{
		WalletAddress:    walletAddress,
		IdempotencyKey:   idempotencyKey,
		Title:            request.Title,
		ShortDescription: request.ShortDescription,
		Story:            request.Story,
		GoalAmount:       request.GoalAmount,
		EndAt:            endAt,
		ImageObjectKey:   request.ImageObjectKey,
		Country:          request.Country,
		ZipCode:          request.ZipCode,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrCampaignEndAtTooSoon):
			h.ValidationError(w, httpx.FieldErrors{
				"endAt": {"endAt must be at least 5 minutes in the future!"},
			})
		case errors.Is(err, service.ErrCampaignIdempotencyConflict):
			h.Error(
				w,
				http.StatusConflict,
				"IDEMPOTENCY_KEY_CONFLICT",
				"Idempotency-Key was already used with a different request payload.",
				nil,
			)
		case errors.Is(err, service.ErrProfileNotFound):
			h.Error(
				w,
				http.StatusNotFound,
				"PROFILE_NOT_FOUND",
				"No fundraiser profile is registered for the authenticated wallet.",
				nil,
			)
		default:
			h.Logger.Error("create campaign", "error", err)
			h.InternalError(w)
		}
		return
	}

	imageObjectKey := created.ImageObjectKey
	response := createCampaignResponse{
		ID:               created.ID.String(),
		Title:            created.Title,
		ShortDescription: created.ShortDescription,
		Story:            created.Story,
		GoalAmount:       created.GoalAmount,
		RaisedAmount:     created.RaisedAmount,
		DonorCount:       created.DonorCount,
		EndAt:            created.EndAt.UTC().Format(time.RFC3339),
		ImageURL:         h.urlBuilder.Build(&imageObjectKey),
		Country:          created.Country,
		ZipCode:          created.ZipCode,
		Status:           created.Status,
		DeploymentStatus: created.DeploymentStatus,
		ContractAddress:  created.ContractAddress,
		CreatedAt:        created.CreatedAt.UTC().Format(time.RFC3339),
	}
	h.Success(w, http.StatusCreated, "CAMPAIGN_CREATED", "Campaign created successfully.", response)
}
