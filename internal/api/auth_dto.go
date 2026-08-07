package api

import (
	"strings"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
)

type createAuthMessageRequest struct {
	Address string `json:"address"`
}

func (r *createAuthMessageRequest) normalize() {
	r.Address = strings.TrimSpace(r.Address)
}

func (r createAuthMessageRequest) validate() httpx.FieldErrors {
	fieldErrors := make(httpx.FieldErrors)
	if r.Address == "" {
		fieldErrors.Add("address", "address is required!")
	}

	if len(fieldErrors) == 0 {
		return nil
	}
	return fieldErrors
}

type createAuthMessageResponse struct {
	Message string `json:"message"`
}

type verifyAuthRequest struct {
	Message   string `json:"message"`
	Signature string `json:"signature"`
}

// normalize only trims the signature. The message is left untouched because
// it must match, byte-for-byte, the SIWE message that was signed and cached.
func (r *verifyAuthRequest) normalize() {
	r.Signature = strings.TrimSpace(r.Signature)
}

func (r verifyAuthRequest) validate() httpx.FieldErrors {
	fieldErrors := make(httpx.FieldErrors)
	if strings.TrimSpace(r.Message) == "" {
		fieldErrors.Add("message", "message is required!")
	}
	if r.Signature == "" {
		fieldErrors.Add("signature", "signature is required!")
	}

	if len(fieldErrors) == 0 {
		return nil
	}
	return fieldErrors
}

type verifyAuthResponse struct {
	AccessToken     string           `json:"accessToken"`
	IsNotRegistered bool             `json:"isNotRegistered"`
	Address         string           `json:"address"`
	Name            *string          `json:"name"`
	Role            *domain.UserRole `json:"role"`
	ImageURL        *string          `json:"imageUrl"`
}
