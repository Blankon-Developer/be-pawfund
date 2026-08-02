package config

import (
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	validEnvironment := map[string]string{
		"DATABASE_URL":            "postgres://test",
		"JWT_SECRET":              strings.Repeat("s", 32),
		"STORAGE_PUBLIC_BASE_URL": "https://storage.example.com/bucket",
	}

	tests := []struct {
		name        string
		environment map[string]string
		wantAddress string
		wantError   string
	}{
		{
			name:        "uses default HTTP address",
			environment: validEnvironment,
			wantAddress: defaultHTTPAddr,
		},
		{
			name: "uses configured HTTP address",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"HTTP_ADDR": "127.0.0.1:9090",
			}),
			wantAddress: "127.0.0.1:9090",
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
