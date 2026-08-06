package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	validEnvironment := map[string]string{
		"DATABASE_URL":            "postgres://test",
		"JWT_SECRET":              strings.Repeat("s", 32),
		"STORAGE_PUBLIC_BASE_URL": "https://storage.example.com/bucket",
		"CACHE_URL":               "redis://localhost:6379/0",
		"CACHE_KEY_PREFIX":        "pawfund-test",
		"SIWE_DOMAIN":             "app.example.com",
		"SIWE_URI":                "https://app.example.com/login",
		"SIWE_CHAIN_ID":           "84532",
	}

	tests := []struct {
		name             string
		environment      map[string]string
		wantAddress    string
		wantError      string
		wantMessageTTL time.Duration
		wantJWTTTL     time.Duration
	}{
		{
			name:           "uses defaults",
			environment:    validEnvironment,
			wantAddress:    defaultHTTPAddr,
			wantMessageTTL: defaultSIWEMessageTTL,
			wantJWTTTL:     defaultJWTTTL,
		},
		{
			name: "uses configured HTTP address",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"HTTP_ADDR":        "127.0.0.1:9090",
				"SIWE_MESSAGE_TTL": "10m",
				"JWT_TTL":          "2h",
			}),
			wantAddress:    "127.0.0.1:9090",
			wantMessageTTL: 10 * time.Minute,
			wantJWTTTL:     2 * time.Hour,
		},
		{
			name: "requires database URL",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"DATABASE_URL": "",
			}),
			wantError: "DATABASE_URL is required",
		},
		{
			name: "requires a sufficiently long JWT secret",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"JWT_SECRET": "short",
			}),
			wantError: "JWT_SECRET must contain at least 32 bytes",
		},
		{
			name: "requires storage public base URL",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"STORAGE_PUBLIC_BASE_URL": "",
			}),
			wantError: "STORAGE_PUBLIC_BASE_URL is required",
		},
		{
			name: "requires cache URL",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"CACHE_URL": "",
			}),
			wantError: "CACHE_URL is required",
		},
		{
			name: "requires cache key prefix",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"CACHE_KEY_PREFIX": "",
			}),
			wantError: "CACHE_KEY_PREFIX is required",
		},
		{
			name: "requires SIWE domain",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"SIWE_DOMAIN": "",
			}),
			wantError: "SIWE_DOMAIN is required",
		},
		{
			name: "requires absolute SIWE URI",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"SIWE_URI": "/login",
			}),
			wantError: "SIWE_URI must be an absolute HTTP(S) URL",
		},
		{
			name: "requires SIWE domain to match URI host",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"SIWE_DOMAIN": "other.example.com",
			}),
			wantError: "SIWE_DOMAIN must match the host in SIWE_URI",
		},
		{
			name: "requires positive SIWE chain ID",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"SIWE_CHAIN_ID": "0",
			}),
			wantError: "SIWE_CHAIN_ID must be a positive integer",
		},
		{
			name: "requires positive SIWE message TTL",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"SIWE_MESSAGE_TTL": "-1m",
			}),
			wantError: "SIWE_MESSAGE_TTL must be a positive duration",
		},
		{
			name: "requires positive JWT TTL",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"JWT_TTL": "invalid",
			}),
			wantError: "JWT_TTL must be a positive duration",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := load(func(key string) string { return test.environment[key] })
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("load() error = %v, want error containing %q", err, test.wantError)
				}
				return
			}

			if err != nil {
				t.Fatalf("load() unexpected error: %v", err)
			}
			if cfg.HTTPAddr != test.wantAddress {
				t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, test.wantAddress)
			}
			if cfg.SIWEMessageTTL != test.wantMessageTTL {
				t.Errorf("SIWEMessageTTL = %v, want %v", cfg.SIWEMessageTTL, test.wantMessageTTL)
			}
			if cfg.JWTTTL != test.wantJWTTTL {
				t.Errorf("JWTTTL = %v, want %v", cfg.JWTTTL, test.wantJWTTTL)
			}
		})
	}
}

func mergeEnvironment(base, override map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(override))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range override {
		merged[key] = value
	}
	return merged
}
