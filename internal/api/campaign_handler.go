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
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	maxCreateCampaignBodyBytes = 1 << 20
	maxIdempotencyKeyBytes     = 255
)

type CampaignService interface {
	Create(ctx context.Context, input service.CreateCampaignInput) (domain.Campaign, error)
	ListPublicCampaigns(ctx context.Context, options domain.CampaignListOptions) ([]domain.PublicCampaignListItem, int64, error)
	GetPublicCampaignDetail(ctx context.Context, contractAddress string) (domain.PublicCampaignDetail, error)
	ListPublicCampaignDonors(
		ctx context.Context,
		contractAddress string,
		options domain.CampaignDonorListOptions,
	) ([]domain.PublicCampaignDonor, int64, error)
	ListMyCampaigns(ctx context.Context, walletAddress string, options domain.CampaignListOptions) ([]domain.Campaign, int64, error)
	GetMyCampaignDetail(ctx context.Context, walletAddress string, campaignID uuid.UUID) (domain.Campaign, error)
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

func (h *CampaignHandler) HandleGetPublicCampaignList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	options, fieldErrors := campaignListOptionsFromQuery(r.URL.Query())
	if fieldErrors != nil {
		h.ValidationError(w, fieldErrors)
		return
	}
	rawSort, sortSpecified := r.URL.Query()["sortBy"]
	if !sortSpecified || strings.TrimSpace(firstQueryValue(rawSort)) == "" {
		options.Sort = domain.CampaignListSortRandom
	}

	campaigns, totalItems, err := h.service.ListPublicCampaigns(r.Context(), options)
	if err != nil {
		h.Logger.Error("list public campaigns", "error", err)
		h.InternalError(w)
		return
	}

	response := make([]publicCampaignListItemResponse, 0, len(campaigns))
	for _, campaign := range campaigns {
		campaignImageObjectKey := campaign.ImageObjectKey
		response = append(response, publicCampaignListItemResponse{
			ID:                 campaign.ID,
			Title:              campaign.Title,
			ShortDescription:   campaign.ShortDescription,
			GoalAmount:         campaign.GoalAmount,
			RaisedAmount:       campaign.RaisedAmount,
			DonorCount:         campaign.DonorCount,
			CampaignImageURL:   h.urlBuilder.Build(&campaignImageObjectKey),
			FundraiserImageURL: h.urlBuilder.Build(campaign.FundraiserImageObjectKey),
			EndAt:              campaign.EndAt.UTC().Format(time.RFC3339),
			CreatedAt:          campaign.CreatedAt.UTC().Format(time.RFC3339),
			ContractAddress:    campaign.ContractAddress,
			Status:             campaign.Status,
		})
	}
	h.SuccessWithPagination(
		w,
		http.StatusOK,
		"CAMPAIGNS_RETRIEVED",
		"Campaigns retrieved successfully.",
		response,
		httpx.NewPagination(options.Page, options.PageSize, totalItems),
	)
}

func (h *CampaignHandler) HandleGetPublicCampaignDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	campaign, err := h.service.GetPublicCampaignDetail(r.Context(), strings.TrimSpace(chi.URLParam(r, "address")))
	if err != nil {
		if errors.Is(err, service.ErrCampaignNotFound) {
			h.Error(
				w,
				http.StatusNotFound,
				"CAMPAIGN_NOT_FOUND",
				"No public campaign was found for the requested contract address.",
				nil,
			)
			return
		}

		h.Logger.Error("get public campaign detail", "error", err)
		h.InternalError(w)
		return
	}

	campaignImageObjectKey := campaign.ImageObjectKey
	response := publicCampaignDetailResponse{
		ID:               campaign.ID,
		Title:            campaign.Title,
		ShortDescription: campaign.ShortDescription,
		Story:            campaign.Story,
		Fundraiser: campaignFundraiser{
			ID:       campaign.FundraiserID,
			Name:     campaign.FundraiserName,
			ImageURL: h.urlBuilder.Build(campaign.FundraiserImageObjectKey),
			Address:  campaign.FundraiserWalletAddress,
		},
		GoalAmount:      campaign.GoalAmount,
		RaisedAmount:    campaign.RaisedAmount,
		DonorCount:      campaign.DonorCount,
		ContractAddress: valueOrEmpty(campaign.ContractAddress),
		EndAt:           campaign.EndAt.UTC().Format(time.RFC3339),
		CreatedAt:       campaign.CreatedAt.UTC().Format(time.RFC3339),
		ImageURL:        valueOrEmpty(h.urlBuilder.Build(&campaignImageObjectKey)),
		Country:         campaign.Country,
		ZipCode:         campaign.ZipCode,
		Status:          campaign.Status,
	}
	h.Success(w, http.StatusOK, "CAMPAIGN_RETRIEVED", "Campaign retrieved successfully.", response)
}

func (h *CampaignHandler) HandleGetPublicCampaignDonors(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	options, fieldErrors := campaignDonorListOptionsFromQuery(r.URL.Query())
	if fieldErrors != nil {
		h.ValidationError(w, fieldErrors)
		return
	}

	donors, totalItems, err := h.service.ListPublicCampaignDonors(
		r.Context(),
		strings.TrimSpace(chi.URLParam(r, "address")),
		options,
	)
	if err != nil {
		if errors.Is(err, service.ErrCampaignNotFound) {
			h.Error(
				w,
				http.StatusNotFound,
				"CAMPAIGN_NOT_FOUND",
				"No public campaign was found for the requested contract address.",
				nil,
			)
			return
		}

		h.Logger.Error("list public campaign donors", "error", err)
		h.InternalError(w)
		return
	}

	response := make([]publicCampaignDonorsItemResponse, 0, len(donors))
	for _, donor := range donors {
		response = append(response, publicCampaignDonorsItemResponse{
			Name:      donor.Name,
			Address:   donor.WalletAddress,
			ImageURL:  h.urlBuilder.Build(donor.ImageObjectKey),
			Amount:    donor.Amount,
			DonatedOn: donor.DonatedAt.UTC().Format(time.RFC3339),
		})
	}
	h.SuccessWithPagination(
		w,
		http.StatusOK,
		"CAMPAIGN_DONORS_RETRIEVED",
		"Campaign donors retrieved successfully.",
		response,
		httpx.NewPagination(options.Page, options.PageSize, totalItems),
	)
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

func (h *CampaignHandler) HandleGetMyCampaignList(w http.ResponseWriter, r *http.Request) {
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

	options, fieldErrors := campaignListOptionsFromQuery(r.URL.Query())
	if fieldErrors != nil {
		h.ValidationError(w, fieldErrors)
		return
	}

	campaigns, totalItems, err := h.service.ListMyCampaigns(r.Context(), walletAddress, options)
	if err != nil {
		h.Logger.Error("list fundraiser campaigns", "error", err)
		h.InternalError(w)
		return
	}

	response := make([]myCampaignListItemResponse, 0, len(campaigns))
	for _, campaign := range campaigns {
		imageObjectKey := campaign.ImageObjectKey
		response = append(response, myCampaignListItemResponse{
			ID:               campaign.ID,
			Title:            campaign.Title,
			ShortDescription: campaign.ShortDescription,
			GoalAmount:       campaign.GoalAmount,
			RaisedAmount:     campaign.RaisedAmount,
			DonorCount:       campaign.DonorCount,
			ImageURL:         h.urlBuilder.Build(&imageObjectKey),
			EndAt:            campaign.EndAt.UTC().Format(time.RFC3339),
			CreatedAt:        campaign.CreatedAt.UTC().Format(time.RFC3339),
			ContractAddress:  campaign.ContractAddress,
			Status:           campaign.Status,
		})
	}
	h.SuccessWithPagination(
		w,
		http.StatusOK,
		"CAMPAIGNS_RETRIEVED",
		"Campaigns retrieved successfully.",
		response,
		httpx.NewPagination(options.Page, options.PageSize, totalItems),
	)
}

func (h *CampaignHandler) HandleGetMyCampaignDetail(w http.ResponseWriter, r *http.Request) {
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

	campaignID, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		h.ValidationError(w, httpx.FieldErrors{
			"id": {"id must be a valid UUID!"},
		})
		return
	}

	campaign, err := h.service.GetMyCampaignDetail(r.Context(), walletAddress, campaignID)
	if err != nil {
		if errors.Is(err, service.ErrCampaignNotFound) {
			h.Error(
				w,
				http.StatusNotFound,
				"CAMPAIGN_NOT_FOUND",
				"No campaign was found for the authenticated fundraiser.",
				nil,
			)
			return
		}

		h.Logger.Error("get fundraiser campaign detail", "error", err)
		h.InternalError(w)
		return
	}

	imageObjectKey := campaign.ImageObjectKey
	response := myCampaignResponse{
		Title:            campaign.Title,
		ShortDescription: campaign.ShortDescription,
		Story:            campaign.Story,
		GoalAmount:       campaign.GoalAmount,
		RaisedAmount:     campaign.RaisedAmount,
		DonorCount:       campaign.DonorCount,
		EndAt:            campaign.EndAt.UTC().Format(time.RFC3339),
		ImageURL:         h.urlBuilder.Build(&imageObjectKey),
		Country:          campaign.Country,
		ZipCode:          campaign.ZipCode,
		Status:           campaign.Status,
		DeploymentStatus: campaign.DeploymentStatus,
		ContractAddress:  campaign.ContractAddress,
		CreatedAt:        campaign.CreatedAt.UTC().Format(time.RFC3339),
	}
	h.Success(w, http.StatusOK, "CAMPAIGN_RETRIEVED", "Campaign retrieved successfully.", response)
}
