package handler

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/http/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
)

const (
	maxSocialURLCharacters = 2048
	maxCountryCharacters   = 255
	maxZipCodeCharacters   = 20
)

type fundraiserContactPerson struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

type registerFundraiserRequest struct {
	Name           string                  `json:"name"`
	Email          string                  `json:"email"`
	ContactPerson  fundraiserContactPerson `json:"contactPerson"`
	SocialURL      string                  `json:"socialUrl"`
	Country        string                  `json:"country"`
	ZipCode        string                  `json:"zipCode"`
	ImageObjectKey *string                 `json:"imageObjectKey"`
}

func (r *registerFundraiserRequest) normalize() {
	r.Name = strings.TrimSpace(r.Name)
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))
	r.ContactPerson.Name = strings.TrimSpace(r.ContactPerson.Name)
	r.ContactPerson.Phone = strings.TrimSpace(r.ContactPerson.Phone)
	r.SocialURL = strings.TrimSpace(r.SocialURL)
	r.Country = strings.TrimSpace(r.Country)
	r.ZipCode = strings.TrimSpace(r.ZipCode)

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

func (r registerFundraiserRequest) validate() httpx.FieldErrors {
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

	validateRequiredLength(fieldErrors, "contactPerson.name", r.ContactPerson.Name, maxNameCharacters)
	validateRequiredLength(fieldErrors, "contactPerson.phone", r.ContactPerson.Phone, maxNameCharacters)

	if r.SocialURL == "" {
		fieldErrors.Add("socialUrl", "socialUrl is required!")
	} else {
		if utf8.RuneCountInString(r.SocialURL) > maxSocialURLCharacters {
			fieldErrors.Add("socialUrl", "socialUrl must not exceed 2048 characters!")
		}
		if !isAbsoluteHTTPURL(r.SocialURL) {
			fieldErrors.Add("socialUrl", "socialUrl must be an absolute HTTP or HTTPS URL!")
		}
	}

	validateRequiredLength(fieldErrors, "country", r.Country, maxCountryCharacters)
	validateRequiredLength(fieldErrors, "zipCode", r.ZipCode, maxZipCodeCharacters)

	if r.ImageObjectKey != nil {
		validateStagedImageObjectKey(fieldErrors, *r.ImageObjectKey, service.ProfileImageDirectory)
	}

	if len(fieldErrors) == 0 {
		return nil
	}
	return fieldErrors
}

func validateRequiredLength(fieldErrors httpx.FieldErrors, field, value string, maxCharacters int) {
	if value == "" {
		fieldErrors.Add(field, field+" is required!")
		return
	}
	if utf8.RuneCountInString(value) > maxCharacters {
		fieldErrors.Add(field, field+" must not exceed "+strconv.Itoa(maxCharacters)+" characters!")
	}
}

func isAbsoluteHTTPURL(rawURL string) bool {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

type registerFundraiserResponse struct {
	Name          string                  `json:"name"`
	Email         string                  `json:"email"`
	ContactPerson fundraiserContactPerson `json:"contactPerson"`
	SocialURL     string                  `json:"socialUrl"`
	Country       string                  `json:"country"`
	ZipCode       string                  `json:"zipCode"`
	ImageURL      *string                 `json:"imageUrl"`
	WalletAddress string                  `json:"walletAddress"`
	Role          domain.UserRole         `json:"role"`
}

type myFundraiserProfileResponse struct {
	Name          string                  `json:"name"`
	Email         string                  `json:"email"`
	ContactPerson fundraiserContactPerson `json:"contactPerson"`
	SocialURL     string                  `json:"socialUrl"`
	Country       string                  `json:"country"`
	ZipCode       string                  `json:"zipCode"`
	ImageURL      *string                 `json:"imageUrl"`
	WalletAddress string                  `json:"walletAddress"`
}

type publicFundraiserProfileResponse struct {
	Name          string                  `json:"name"`
	Email         string                  `json:"email"`
	ContactPerson fundraiserContactPerson `json:"contactPerson"`
	SocialURL     string                  `json:"socialUrl"`
	Country       string                  `json:"country"`
	ZipCode       string                  `json:"zipCode"`
	ImageURL      *string                 `json:"imageUrl"`
	CreatedAt     string                  `json:"createdAt"`
}

type replaceFundraiserProfileRequest struct {
	Name           string                  `json:"name"`
	ImageObjectKey optionalImageObjectKey  `json:"imageObjectKey"`
	Email          string                  `json:"email"`
	ContactPerson  fundraiserContactPerson `json:"contactPerson"`
	SocialURL      string                  `json:"socialUrl"`
	Country        string                  `json:"country"`
	ZipCode        string                  `json:"zipCode"`
}

// optionalImageObjectKey distinguishes an omitted image update from an
// explicit null, which clears the current image.
type optionalImageObjectKey struct {
	set   bool
	value *string
}

func (k *optionalImageObjectKey) UnmarshalJSON(data []byte) error {
	k.set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		k.value = nil
		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	k.value = &value
	return nil
}

func (k *optionalImageObjectKey) normalize() {
	if !k.set || k.value == nil {
		return
	}
	value := strings.TrimSpace(*k.value)
	k.value = &value
}

func (r *replaceFundraiserProfileRequest) normalize() {
	r.Name = strings.TrimSpace(r.Name)
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))
	r.ContactPerson.Name = strings.TrimSpace(r.ContactPerson.Name)
	r.ContactPerson.Phone = strings.TrimSpace(r.ContactPerson.Phone)
	r.SocialURL = strings.TrimSpace(r.SocialURL)
	r.Country = strings.TrimSpace(r.Country)
	r.ZipCode = strings.TrimSpace(r.ZipCode)
	r.ImageObjectKey.normalize()
}

func (r replaceFundraiserProfileRequest) validate() httpx.FieldErrors {
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

	validateRequiredLength(fieldErrors, "contactPerson.name", r.ContactPerson.Name, maxNameCharacters)
	validateRequiredLength(fieldErrors, "contactPerson.phone", r.ContactPerson.Phone, maxNameCharacters)

	if r.SocialURL == "" {
		fieldErrors.Add("socialUrl", "socialUrl is required!")
	} else {
		if utf8.RuneCountInString(r.SocialURL) > maxSocialURLCharacters {
			fieldErrors.Add("socialUrl", "socialUrl must not exceed 2048 characters!")
		}
		if !isAbsoluteHTTPURL(r.SocialURL) {
			fieldErrors.Add("socialUrl", "socialUrl must be an absolute HTTP or HTTPS URL!")
		}
	}

	validateRequiredLength(fieldErrors, "country", r.Country, maxCountryCharacters)
	validateRequiredLength(fieldErrors, "zipCode", r.ZipCode, maxZipCodeCharacters)

	if r.ImageObjectKey.set && r.ImageObjectKey.value != nil {
		if *r.ImageObjectKey.value == "" {
			fieldErrors.Add("imageObjectKey", "imageObjectKey must not be empty!")
		} else {
			validateStagedImageObjectKey(fieldErrors, *r.ImageObjectKey.value, service.ProfileImageDirectory)
		}
	}

	if len(fieldErrors) == 0 {
		return nil
	}
	return fieldErrors
}

func (r replaceFundraiserProfileRequest) toProfileReplacement() domain.FundraiserProfileReplacement {
	return domain.FundraiserProfileReplacement{
		Name:  r.Name,
		Email: r.Email,
		ImageObjectKey: domain.ImageObjectKeyUpdate{
			Set:   r.ImageObjectKey.set,
			Value: r.ImageObjectKey.value,
		},
		ContactName:  r.ContactPerson.Name,
		ContactPhone: r.ContactPerson.Phone,
		SocialURL:    r.SocialURL,
		Country:      r.Country,
		ZipCode:      r.ZipCode,
	}
}
