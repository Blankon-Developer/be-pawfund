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
	defaultStorageRegion  = "us-east-1"
	defaultStorageTTL     = 15 * time.Minute
)

type Config struct {
	HTTPAddr             string
	DatabaseURL          string
	JWTSecret            []byte
	StoragePublicBaseURL string
	StorageEndpoint      string
	StorageAccessKey     string
	StorageSecretKey     string
	StorageBucket        string
	StorageRegion        string
	StoragePresignTTL    time.Duration
	CacheURL             string
	CacheKeyPrefix       string
	QueueURL             string
	SIWEDomain           string
	SIWEURI              string
	SIWEChainID          int
	SIWEMessageTTL       time.Duration
	JWTTTL               time.Duration
	CORSAllowedOrigins   []string
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
		StorageEndpoint:      strings.TrimSpace(getenv("STORAGE_ENDPOINT")),
		StorageAccessKey:     strings.TrimSpace(getenv("STORAGE_ACCESS_KEY")),
		StorageSecretKey:     strings.TrimSpace(getenv("STORAGE_SECRET_KEY")),
		StorageBucket:        strings.TrimSpace(getenv("STORAGE_BUCKET")),
		StorageRegion:        strings.TrimSpace(getenv("STORAGE_REGION")),
		CacheURL:             strings.TrimSpace(getenv("CACHE_URL")),
		CacheKeyPrefix:       strings.TrimSpace(getenv("CACHE_KEY_PREFIX")),
		QueueURL:             strings.TrimSpace(getenv("QUEUE_URL")),
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
	if cfg.StorageEndpoint == "" {
		return Config{}, fmt.Errorf("config: STORAGE_ENDPOINT is required")
	}
	if err := validateStorageEndpoint(cfg.StorageEndpoint); err != nil {
		return Config{}, err
	}
	if cfg.StorageAccessKey == "" {
		return Config{}, fmt.Errorf("config: STORAGE_ACCESS_KEY is required")
	}
	if cfg.StorageSecretKey == "" {
		return Config{}, fmt.Errorf("config: STORAGE_SECRET_KEY is required")
	}
	if cfg.StorageBucket == "" {
		return Config{}, fmt.Errorf("config: STORAGE_BUCKET is required")
	}
	if cfg.StorageRegion == "" {
		cfg.StorageRegion = defaultStorageRegion
	}
	if cfg.CacheURL == "" {
		return Config{}, fmt.Errorf("config: CACHE_URL is required")
	}
	if cfg.CacheKeyPrefix == "" {
		return Config{}, fmt.Errorf("config: CACHE_KEY_PREFIX is required")
	}
	if cfg.QueueURL == "" {
		return Config{}, fmt.Errorf("config: QUEUE_URL is required")
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
	cfg.StoragePresignTTL, err = positiveDuration(
		getenv("STORAGE_PRESIGN_TTL"),
		defaultStorageTTL,
		"STORAGE_PRESIGN_TTL",
	)
	if err != nil {
		return Config{}, err
	}
	cfg.CORSAllowedOrigins, err = parseCORSOrigins(getenv("CORS_ALLOWED_ORIGINS"))
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func parseCORSOrigins(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		origin, err := normalizeHTTPOrigin(part)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[origin]; exists {
			continue
		}
		seen[origin] = struct{}{}
		origins = append(origins, origin)
	}
	return origins, nil
}

func normalizeHTTPOrigin(raw string) (string, error) {
	if raw == "*" {
		return "", fmt.Errorf("config: CORS_ALLOWED_ORIGINS must not include *")
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("config: CORS_ALLOWED_ORIGINS must contain absolute HTTP(S) origins")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("config: CORS_ALLOWED_ORIGINS must contain HTTP(S) origins without credentials, query, or fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("config: CORS_ALLOWED_ORIGINS must contain HTTP(S) origins without a path")
	}

	return parsed.Scheme + "://" + parsed.Host, nil
}

func validateStorageEndpoint(rawEndpoint string) error {
	parsed, err := url.Parse(rawEndpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("config: STORAGE_ENDPOINT must be an absolute HTTP(S) URL")
	}
	if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("config: STORAGE_ENDPOINT must be an HTTP(S) origin without credentials, path, query, or fragment")
	}
	return nil
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
