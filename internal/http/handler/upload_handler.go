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
	"github.com/Blankon-Developer/be-pawfund/internal/service"
)

const maxPresignProfileImageBodyBytes = 16 << 10

type UploadService interface {
	PresignProfileImage(
		ctx context.Context,
		input service.PresignProfileImageInput,
	) (service.PresignProfileImageResult, error)
	PresignCampaignImage(
		ctx context.Context,
		input service.PresignCampaignImageInput,
	) (service.PresignCampaignImageResult, error)
}

type UploadHandler struct {
	service UploadService
	httpx.Responder
}

func NewUploadHandler(service UploadService, logger *slog.Logger) *UploadHandler {
	return &UploadHandler{service: service, Responder: httpx.NewResponder(logger)}
}

func (h *UploadHandler) HandlePresignProfileImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var request presignProfileImageRequest
	if err := httpx.ReadJSON(w, r, &request, maxPresignProfileImageBodyBytes); err != nil {
		h.ReadError(w, err, "Request body exceeds the 16 KiB limit.")
		return
	}

	request.normalize()
	if fieldErrors := request.validate(); fieldErrors != nil {
		h.ValidationError(w, fieldErrors)
		return
	}

	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.WalletAddress) == "" {
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

	result, err := h.service.PresignProfileImage(r.Context(), service.PresignProfileImageInput{
		ContentType: request.ContentType,
		Size:        request.Size,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUnsupportedProfileImageType):
			h.ValidationError(w, httpx.FieldErrors{
				"contentType": {"contentType must be image/jpeg, image/png, or image/webp!"},
			})
		case errors.Is(err, service.ErrInvalidProfileImageSize):
			h.ValidationError(w, httpx.FieldErrors{
				"size": {"size must be between 1 and 5242880 bytes!"},
			})
		default:
			h.Logger.Error("presign profile image upload", "error", err)
			h.InternalError(w)
		}
		return
	}

	h.Success(
		w,
		http.StatusOK,
		"PROFILE_IMAGE_UPLOAD_PRESIGNED",
		"Profile image upload presigned successfully.",
		presignProfileImageResponse{ObjectKey: result.ObjectKey, URL: result.URL},
	)
}

func (h *UploadHandler) HandlePresignCampaignImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.WalletAddress) == "" {
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

	var request presignProfileImageRequest
	if err := httpx.ReadJSON(w, r, &request, maxPresignProfileImageBodyBytes); err != nil {
		h.ReadError(w, err, "Request body exceeds the 16 KiB limit.")
		return
	}

	request.normalize()
	if fieldErrors := request.validate(); fieldErrors != nil {
		h.ValidationError(w, fieldErrors)
		return
	}

	result, err := h.service.PresignCampaignImage(r.Context(), service.PresignCampaignImageInput{
		ContentType: request.ContentType,
		Size:        request.Size,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUnsupportedProfileImageType):
			h.ValidationError(w, httpx.FieldErrors{
				"contentType": {"contentType must be image/jpeg, image/png, or image/webp!"},
			})
		case errors.Is(err, service.ErrInvalidProfileImageSize):
			h.ValidationError(w, httpx.FieldErrors{
				"size": {"size must be between 1 and 5242880 bytes!"},
			})
		default:
			h.Logger.Error("presign campaign image upload", "error", err)
			h.InternalError(w)
		}
		return
	}

	h.Success(
		w,
		http.StatusOK,
		"CAMPAIGN_IMAGE_UPLOAD_PRESIGNED",
		"Campaign image upload presigned successfully.",
		presignProfileImageResponse{ObjectKey: result.ObjectKey, URL: result.URL},
	)
}
