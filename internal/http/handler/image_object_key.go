package handler

import (
	"github.com/Blankon-Developer/be-pawfund/internal/http/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
)

func validateStagedImageObjectKey(fieldErrors httpx.FieldErrors, key, directory string) {
	if key == "" {
		return
	}
	if len(key) > maxImageObjectKeyBytes {
		fieldErrors.Add("imageObjectKey", "imageObjectKey must not exceed 1024 bytes!")
		return
	}
	if _, err := service.CanonicalImageObjectKey(key, directory); err != nil {
		fieldErrors.Add("imageObjectKey", stagedImageObjectKeyMessage(directory))
	}
}

func stagedImageObjectKeyMessage(directory string) string {
	if directory == service.CampaignImageDirectory {
		return "imageObjectKey must be a staged campaign image key!"
	}
	return "imageObjectKey must be a staged profile image key!"
}

func stagedImageObjectNotFoundMessage() string {
	return "imageObjectKey does not reference an uploaded image!"
}
