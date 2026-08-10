package api

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
)

type createCampaignRequest struct {
	Title            string `json:"title"`
	ShortDescription string `json:"shortDescription"`
	Story            string `json:"story"`
	GoalAmount       int64  `json:"goalAmount"`
	EndAt            string `json:"endAt"`
	ImageObjectKey   string `json:"imageObjectKey"`
	Country          string `json:"country"`
	ZipCode          string `json:"zipCode"`
}

func (r *createCampaignRequest) normalize() {
	r.Title = strings.TrimSpace(r.Title)
	r.ShortDescription = strings.TrimSpace(r.ShortDescription)
	r.Story = strings.TrimSpace(r.Story)
	r.EndAt = strings.TrimSpace(r.EndAt)
	r.ImageObjectKey = strings.TrimSpace(r.ImageObjectKey)
	r.Country = strings.TrimSpace(r.Country)
	r.ZipCode = strings.TrimSpace(r.ZipCode)
}

func (r createCampaignRequest) validate() (httpx.FieldErrors, time.Time) {
	fieldErrors := make(httpx.FieldErrors)
	validateRequiredLength(fieldErrors, "title", r.Title, maxNameCharacters)
	validateRequiredLength(fieldErrors, "shortDescription", r.ShortDescription, maxNameCharacters)
	if r.Story == "" {
		fieldErrors.Add("story", "story is required!")
	}
	if r.GoalAmount <= 0 {
		fieldErrors.Add("goalAmount", "goalAmount must be greater than 0!")
	}

	var endAt time.Time
	if r.EndAt == "" {
		fieldErrors.Add("endAt", "endAt is required!")
	} else {
		parsed, err := time.Parse(time.RFC3339, r.EndAt)
		if err != nil {
			fieldErrors.Add("endAt", "endAt must be a valid RFC3339 timestamp!")
		} else {
			endAt = parsed.UTC()
		}
	}

	if r.ImageObjectKey == "" {
		fieldErrors.Add("imageObjectKey", "imageObjectKey is required!")
	} else if len(r.ImageObjectKey) > maxImageObjectKeyBytes {
		fieldErrors.Add("imageObjectKey", "imageObjectKey must not exceed 1024 bytes!")
	}
	validateRequiredLength(fieldErrors, "country", r.Country, maxCountryCharacters)
	validateRequiredLength(fieldErrors, "zipCode", r.ZipCode, maxZipCodeCharacters)

	if !utf8.ValidString(r.Story) {
		fieldErrors.Add("story", "story must contain valid UTF-8 text!")
	}
	if len(fieldErrors) == 0 {
		return nil, endAt
	}
	return fieldErrors, time.Time{}
}

type createCampaignResponse struct {
	ID               string                          `json:"id"`
	Title            string                          `json:"title"`
	ShortDescription string                          `json:"shortDescription"`
	Story            string                          `json:"story"`
	GoalAmount       int64                           `json:"goalAmount"`
	RaisedAmount     int64                           `json:"raisedAmount"`
	DonorCount       int64                           `json:"donorCount"`
	EndAt            string                          `json:"endAt"`
	ImageURL         *string                         `json:"imageUrl"`
	Country          string                          `json:"country"`
	ZipCode          string                          `json:"zipCode"`
	Status           domain.CampaignStatus           `json:"status"`
	DeploymentStatus domain.CampaignDeploymentStatus `json:"deploymentStatus"`
	ContractAddress  *string                         `json:"contractAddress"`
	CreatedAt        string                          `json:"createdAt"`
}
