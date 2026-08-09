package service

import "github.com/google/uuid"

type IDGenerator func() (uuid.UUID, error)
