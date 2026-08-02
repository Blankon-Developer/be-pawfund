package config

import (
	"fmt"
	"os"
	"strings"
)

const defaultHTTPAddr = ":8080"

type Config struct {
	HTTPAddr             string
	DatabaseURL          string
	JWTSecret            []byte
	StoragePublicBaseURL string
}

func Load() (Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:             strings.TrimSpace(getenv("HTTP_ADDR")),
		DatabaseURL:          strings.TrimSpace(getenv("DATABASE_URL")),
		JWTSecret:            []byte(getenv("JWT_SECRET")),
		StoragePublicBaseURL: strings.TrimSpace(getenv("STORAGE_PUBLIC_BASE_URL")),
	}

	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = defaultHTTPAddr
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: DATABASE_URL is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("config: JWT_SECRET must contain at least 32 bytes")
	}
	if cfg.StoragePublicBaseURL == "" {
		return Config{}, fmt.Errorf("config: STORAGE_PUBLIC_BASE_URL is required")
	}

	return cfg, nil
}
