package api

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
)

const (
	maxNameCharacters      = 255
	maxEmailCharacters     = 255
	maxImageObjectKeyBytes = 1024
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
