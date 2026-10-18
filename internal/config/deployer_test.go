package config

import (
	"testing"
)

func TestLoadDeployer(t *testing.T) {
	validEnvironment := map[string]string{
		"DATABASE_URL":  "postgres://test",
		"QUEUE_URL":     "amqp://guest:guest@localhost:5672/",
		"SIWE_CHAIN_ID": "84532",
	}

	tests := []struct {
		name        string
		environment map[string]string
		wantHTTP    string
		wantChainID int
		wantError   string
	}{
		{
			name:        "uses defaults",
			environment: validEnvironment,
			wantHTTP:    defaultDeployerHTTPAddr,
			wantChainID: 84532,
		},
		{
			name: "uses configured HTTP address",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"DEPLOYER_HTTP_ADDR": "127.0.0.1:9091",
			}),
			wantHTTP:    "127.0.0.1:9091",
			wantChainID: 84532,
		},
		{
			name: "requires database URL",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"DATABASE_URL": "",
			}),
			wantError: "DATABASE_URL is required",
		},
		{
			name: "requires queue URL",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"QUEUE_URL": "",
			}),
			wantError: "QUEUE_URL is required",
		},
		{
			name: "requires chain ID",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"SIWE_CHAIN_ID": "",
			}),
			wantError: "SIWE_CHAIN_ID must be a positive integer",
		},
		{
			name: "rejects invalid chain ID",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"SIWE_CHAIN_ID": "0",
			}),
			wantError: "SIWE_CHAIN_ID must be a positive integer",
		},
		{
			name: "defaults deployment mode to stub",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"DEPLOYMENT_MODE": "",
			}),
			wantHTTP:    defaultDeployerHTTPAddr,
			wantChainID: 84532,
		},
		{
			name: "requires live chain settings",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"DEPLOYMENT_MODE": "live",
			}),
			wantError: "CHAIN_RPC_URL is required when DEPLOYMENT_MODE=live",
		},
		{
			name: "accepts live deployment settings",
			environment: mergeEnvironment(validEnvironment, map[string]string{
				"DEPLOYMENT_MODE":          "live",
				"CHAIN_RPC_URL":            "https://sepolia.base.org",
				"CAMPAIGN_FACTORY_ADDRESS": "0x0000000000000000000000000000000000000001",
				"DEPLOYER_PRIVATE_KEY":     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			}),
			wantHTTP:    defaultDeployerHTTPAddr,
			wantChainID: 84532,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadDeployer(func(key string) string {
				return tt.environment[key]
			})
			if tt.wantError != "" {
				if err == nil || err.Error() != "config: "+tt.wantError {
					t.Fatalf("LoadDeployer() error = %v, want %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadDeployer() unexpected error: %v", err)
			}
			if cfg.HTTPAddr != tt.wantHTTP || cfg.ChainID != tt.wantChainID {
				t.Fatalf("config = %#v, want HTTP %q chainID %d", cfg, tt.wantHTTP, tt.wantChainID)
			}
			if tt.name == "defaults deployment mode to stub" && cfg.DeploymentMode != "stub" {
				t.Fatalf("DeploymentMode = %q, want stub", cfg.DeploymentMode)
			}
			if tt.name == "accepts live deployment settings" && cfg.DeploymentMode != "live" {
				t.Fatalf("DeploymentMode = %q, want live", cfg.DeploymentMode)
			}
		})
	}
}
