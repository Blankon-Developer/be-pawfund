package api

import "github.com/Blankon-Developer/be-pawfund/internal/domain"

type createAuthMessageRequest struct {
	Address string `json:"address"`
}

type createAuthMessageResponse struct {
	Message string `json:"message"`
}

type verifyAuthRequest struct {
	Message   string `json:"message"`
	Signature string `json:"signature"`
}

type verifyAuthResponse struct {
	AccessToken     string           `json:"accessToken"`
	IsNotRegistered bool             `json:"isNotRegistered"`
	Address         string           `json:"address"`
	Name            *string          `json:"name"`
	Role            *domain.UserRole `json:"role"`
	ImageURL        *string          `json:"imageUrl"`
}
