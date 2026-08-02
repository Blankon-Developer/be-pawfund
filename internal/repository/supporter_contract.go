package repository

import (
	"context"
	"errors"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
)

var (
	ErrEmailAlreadyExists  = errors.New("email already exists")
	ErrWalletAlreadyExists = errors.New("wallet address already exists")
)

type SupporterRepository interface {
	Create(ctx context.Context, supporter domain.Supporter) (domain.Supporter, error)
}
