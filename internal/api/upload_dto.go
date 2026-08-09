package api

import (
	"strings"

	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
)

type presignProfileImageRequest struct {
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
}

func (r *presignProfileImageRequest) normalize() {
	r.ContentType = strings.ToLower(strings.TrimSpace(r.ContentType))
}

func (r presignProfileImageRequest) validate() httpx.FieldErrors {
	fieldErrors := make(httpx.FieldErrors)
	if r.ContentType == "" {
		fieldErrors.Add("contentType", "contentType is required!")
	} else if !isSupportedProfileImageContentType(r.ContentType) {
		fieldErrors.Add("contentType", "contentType must be image/jpeg, image/png, or image/webp!")
	}
	if r.Size < service.MinProfileImageSize || r.Size > service.MaxProfileImageSize {
		fieldErrors.Add("size", "size must be between 1 and 5242880 bytes!")
	}

	if len(fieldErrors) == 0 {
		return nil
	}
	return fieldErrors
}

func isSupportedProfileImageContentType(contentType string) bool {
	return contentType == "image/jpeg" || contentType == "image/png" || contentType == "image/webp"
}

type presignProfileImageResponse struct {
	ObjectKey string `json:"objectKey"`
	URL       string `json:"url"`
}
