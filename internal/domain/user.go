package domain

import (
	"time"

	"github.com/google/uuid"
)

type UserRole string

const (
	UserRoleFundraiser UserRole = "fundraiser"
	UserRoleSupporter  UserRole = "supporter"
)

type User struct {
	ID            uuid.UUID
	Role          UserRole
	Email         string
	WalletAddress string
	CreatedAt     time.Time
}
