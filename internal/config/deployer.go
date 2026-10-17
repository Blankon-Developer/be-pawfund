package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const defaultDeployerHTTPAddr = ":8081"

// DeployerConfig holds settings for the campaign deployment worker process.
type DeployerConfig struct {
	HTTPAddr    string
	DatabaseURL string
	QueueURL    string
	ChainID     int
}

// LoadDeployer reads deployer environment variables. It intentionally does not
// reuse Load() so the worker does not require API-only secrets such as JWT or storage.
func LoadDeployer() (DeployerConfig, error) {
	return loadDeployer(os.Getenv)
}

func loadDeployer(getenv func(string) string) (DeployerConfig, error) {
	cfg := DeployerConfig{
		HTTPAddr:    strings.TrimSpace(getenv("DEPLOYER_HTTP_ADDR")),
		DatabaseURL: strings.TrimSpace(getenv("DATABASE_URL")),
		QueueURL:    strings.TrimSpace(getenv("QUEUE_URL")),
	}

	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = defaultDeployerHTTPAddr
	}
	if cfg.DatabaseURL == "" {
		return DeployerConfig{}, fmt.Errorf("config: DATABASE_URL is required")
	}
	if cfg.QueueURL == "" {
		return DeployerConfig{}, fmt.Errorf("config: QUEUE_URL is required")
	}

	chainID, err := strconv.Atoi(strings.TrimSpace(getenv("SIWE_CHAIN_ID")))
	if err != nil || chainID <= 0 {
		return DeployerConfig{}, fmt.Errorf("config: SIWE_CHAIN_ID must be a positive integer")
	}
	cfg.ChainID = chainID

	return cfg, nil
}
