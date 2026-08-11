package api

import (
	"math"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
)

const (
	maxNameCharacters           = 255
	maxEmailCharacters          = 255
	maxImageObjectKeyBytes      = 1024
	defaultDonationListPage     = 1
	defaultDonationListPageSize = 10
	maxDonationListPageSize     = 100
)

var emailPattern = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

type registerSupporterRequest struct {
	Name           string  `json:"name"`
	Email          string  `json:"email"`
	ImageObjectKey *string `json:"imageObjectKey"`
}

func (r *registerSupporterRequest) normalize() {
	r.Name = strings.TrimSpace(r.Name)
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))

	if r.ImageObjectKey == nil {
		return
	}

	key := strings.TrimSpace(*r.ImageObjectKey)
	if key == "" {
		r.ImageObjectKey = nil
		return
	}
	r.ImageObjectKey = &key
}

func (r registerSupporterRequest) validate() httpx.FieldErrors {
	fieldErrors := make(httpx.FieldErrors)

	if r.Name == "" {
		fieldErrors.Add("name", "name is required!")
	} else if utf8.RuneCountInString(r.Name) > maxNameCharacters {
		fieldErrors.Add("name", "name must not exceed 255 characters!")
	}

	if r.Email == "" {
		fieldErrors.Add("email", "email is required!")
	} else {
		if utf8.RuneCountInString(r.Email) > maxEmailCharacters {
			fieldErrors.Add("email", "email must not exceed 255 characters!")
		}
		if !emailPattern.MatchString(r.Email) {
			fieldErrors.Add("email", "email format is not valid!")
		}
	}

	if r.ImageObjectKey != nil && len(*r.ImageObjectKey) > maxImageObjectKeyBytes {
		fieldErrors.Add("imageObjectKey", "imageObjectKey must not exceed 1024 bytes!")
	}

	if len(fieldErrors) == 0 {
		return nil
	}
	return fieldErrors
}

type registerSupporterResponse struct {
	Name          string          `json:"name"`
	Email         string          `json:"email"`
	WalletAddress string          `json:"walletAddress"`
	ImageURL      *string         `json:"imageUrl"`
	Role          domain.UserRole `json:"role"`
}

type mySupporterProfileResponse struct {
	Name          string  `json:"name"`
	Email         string  `json:"email"`
	WalletAddress string  `json:"walletAddress"`
	ImageURL      *string `json:"imageUrl"`
}

type replaceSupporterProfileRequest struct {
	Name           string                 `json:"name"`
	ImageObjectKey optionalImageObjectKey `json:"imageObjectKey"`
	Email          string                 `json:"email"`
}

func (r *replaceSupporterProfileRequest) normalize() {
	r.Name = strings.TrimSpace(r.Name)
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))
	r.ImageObjectKey.normalize()
}

func (r replaceSupporterProfileRequest) validate() httpx.FieldErrors {
	fieldErrors := make(httpx.FieldErrors)

	validateRequiredLength(fieldErrors, "name", r.Name, maxNameCharacters)
	if r.Email == "" {
		fieldErrors.Add("email", "email is required!")
	} else {
		if utf8.RuneCountInString(r.Email) > maxEmailCharacters {
			fieldErrors.Add("email", "email must not exceed 255 characters!")
		}
		if !emailPattern.MatchString(r.Email) {
			fieldErrors.Add("email", "email format is not valid!")
		}
	}

	if r.ImageObjectKey.set && r.ImageObjectKey.value != nil {
		if *r.ImageObjectKey.value == "" {
			fieldErrors.Add("imageObjectKey", "imageObjectKey must not be empty!")
		} else if len(*r.ImageObjectKey.value) > maxImageObjectKeyBytes {
			fieldErrors.Add("imageObjectKey", "imageObjectKey must not exceed 1024 bytes!")
		}
	}

	if len(fieldErrors) == 0 {
		return nil
	}
	return fieldErrors
}

func (r replaceSupporterProfileRequest) toProfileReplacement() domain.SupporterProfileReplacement {
	return domain.SupporterProfileReplacement{
		Name:  r.Name,
		Email: r.Email,
		ImageObjectKey: domain.ImageObjectKeyUpdate{
			Set:   r.ImageObjectKey.set,
			Value: r.ImageObjectKey.value,
		},
	}
}

type myDonationCampaignItem struct {
	Title           string `json:"title"`
	ContractAddress string `json:"contractAddress"`
}

type myDonationItemListResponse struct {
	Amount    int64                  `json:"amount"`
	Campaign  myDonationCampaignItem `json:"campaign"`
	DonatedOn string                 `json:"donatedOn"`
	TxHash    string                 `json:"txHash"`
}

func donationListOptionsFromQuery(query url.Values) (domain.DonationListOptions, httpx.FieldErrors) {
	options := domain.DonationListOptions{
		Page:     defaultDonationListPage,
		PageSize: defaultDonationListPageSize,
	}
	fieldErrors := make(httpx.FieldErrors)

	if rawPage, ok := query["page"]; ok {
		page, valid := parsePositiveInt64(firstQueryValue(rawPage))
		if !valid {
			fieldErrors.Add("page", "page must be a positive integer!")
		} else {
			options.Page = page
		}
	}

	if rawPageSize, ok := query["pageSize"]; ok {
		pageSize, valid := parsePositiveInt64(firstQueryValue(rawPageSize))
		if !valid || pageSize > maxDonationListPageSize {
			fieldErrors.Add("pageSize", "pageSize must be an integer between 1 and 100!")
		} else {
			options.PageSize = pageSize
		}
	}

	if _, pageInvalid := fieldErrors["page"]; !pageInvalid && options.Page-1 > math.MaxInt64/options.PageSize {
		fieldErrors.Add("page", "page is too large!")
	}

	if len(fieldErrors) == 0 {
		return options, nil
	}
	return domain.DonationListOptions{}, fieldErrors
}
