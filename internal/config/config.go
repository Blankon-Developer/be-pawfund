package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddr       = ":8080"
	defaultSIWEMessageTTL = 5 * time.Minute
	defaultJWTTTL         = 24 * time.Hour
)

type Config struct {
	HTTPAddr             string
	DatabaseURL          string
	JWTSecret            []byte
	StoragePublicBaseURL string
	CacheURL             string
	CacheKeyPrefix       string
	SIWEDomain           string
	SIWEURI              string
	SIWEChainID          int
	SIWEMessageTTL       time.Duration
	JWTTTL               time.Duration
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
		CacheURL:             strings.TrimSpace(getenv("CACHE_URL")),
		CacheKeyPrefix:       strings.TrimSpace(getenv("CACHE_KEY_PREFIX")),
		SIWEDomain:           strings.TrimSpace(getenv("SIWE_DOMAIN")),
		SIWEURI:              strings.TrimSpace(getenv("SIWE_URI")),
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
	if cfg.CacheURL == "" {
		return Config{}, fmt.Errorf("config: CACHE_URL is required")
	}
	if cfg.CacheKeyPrefix == "" {
		return Config{}, fmt.Errorf("config: CACHE_KEY_PREFIX is required")
	}
	if cfg.SIWEDomain == "" {
		return Config{}, fmt.Errorf("config: SIWE_DOMAIN is required")
	}
	if cfg.SIWEURI == "" {
		return Config{}, fmt.Errorf("config: SIWE_URI is required")
	}

	parsedURI, err := url.Parse(cfg.SIWEURI)
	if err != nil || parsedURI.Host == "" || (parsedURI.Scheme != "http" && parsedURI.Scheme != "https") {
		return Config{}, fmt.Errorf("config: SIWE_URI must be an absolute HTTP(S) URL")
	}
	if parsedURI.Host != cfg.SIWEDomain {
		return Config{}, fmt.Errorf("config: SIWE_DOMAIN must match the host in SIWE_URI")
	}

	chainID, err := strconv.Atoi(strings.TrimSpace(getenv("SIWE_CHAIN_ID")))
	if err != nil || chainID <= 0 {
		return Config{}, fmt.Errorf("config: SIWE_CHAIN_ID must be a positive integer")
	}
	cfg.SIWEChainID = chainID

	cfg.SIWEMessageTTL, err = positiveDuration(
		getenv("SIWE_MESSAGE_TTL"),
		defaultSIWEMessageTTL,
		"SIWE_MESSAGE_TTL",
	)
	if err != nil {
		return Config{}, err
	}
	cfg.JWTTTL, err = positiveDuration(getenv("JWT_TTL"), defaultJWTTTL, "JWT_TTL")
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func positiveDuration(raw string, defaultValue time.Duration, name string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("config: %s must be a positive duration", name)
	}
	return value, nil
}
