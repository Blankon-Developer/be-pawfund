package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultDeployerHTTPAddr      = ":8081"
	defaultDeploymentMode        = "stub"
	defaultDeploymentConfirmWait = 2 * time.Minute
)

// DeployerConfig holds settings for the campaign deployment worker process.
type DeployerConfig struct {
	HTTPAddr               string
	DatabaseURL            string
	QueueURL               string
	ChainID                int
	DeploymentMode         string
	ChainRPCURL            string
	CampaignFactoryAddress string
	DeployerPrivateKey     string
	ConfirmTimeout         time.Duration
}

// LoadDeployer reads deployer environment variables. It intentionally does not
// reuse Load() so the worker does not require API-only secrets such as JWT or storage.
func LoadDeployer() (DeployerConfig, error) {
	return loadDeployer(os.Getenv)
}

func loadDeployer(getenv func(string) string) (DeployerConfig, error) {
	cfg := DeployerConfig{
		HTTPAddr:               strings.TrimSpace(getenv("DEPLOYER_HTTP_ADDR")),
		DatabaseURL:            strings.TrimSpace(getenv("DATABASE_URL")),
		QueueURL:               strings.TrimSpace(getenv("QUEUE_URL")),
		DeploymentMode:         strings.ToLower(strings.TrimSpace(getenv("DEPLOYMENT_MODE"))),
		ChainRPCURL:            strings.TrimSpace(getenv("CHAIN_RPC_URL")),
		CampaignFactoryAddress: strings.TrimSpace(getenv("CAMPAIGN_FACTORY_ADDRESS")),
		DeployerPrivateKey:     strings.TrimSpace(getenv("DEPLOYER_PRIVATE_KEY")),
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
	if cfg.DeploymentMode == "" {
		cfg.DeploymentMode = defaultDeploymentMode
	}

	chainID, err := strconv.Atoi(strings.TrimSpace(getenv("SIWE_CHAIN_ID")))
	if err != nil || chainID <= 0 {
		return DeployerConfig{}, fmt.Errorf("config: SIWE_CHAIN_ID must be a positive integer")
	}
	cfg.ChainID = chainID

	cfg.ConfirmTimeout, err = positiveDuration(
		getenv("DEPLOYMENT_CONFIRM_TIMEOUT"),
		defaultDeploymentConfirmWait,
		"DEPLOYMENT_CONFIRM_TIMEOUT",
	)
	if err != nil {
		return DeployerConfig{}, err
	}

	switch cfg.DeploymentMode {
	case "stub":
	case "live":
		if cfg.ChainRPCURL == "" {
			return DeployerConfig{}, fmt.Errorf("config: CHAIN_RPC_URL is required when DEPLOYMENT_MODE=live")
		}
		if cfg.CampaignFactoryAddress == "" {
			return DeployerConfig{}, fmt.Errorf("config: CAMPAIGN_FACTORY_ADDRESS is required when DEPLOYMENT_MODE=live")
		}
		if cfg.DeployerPrivateKey == "" {
			return DeployerConfig{}, fmt.Errorf("config: DEPLOYER_PRIVATE_KEY is required when DEPLOYMENT_MODE=live")
		}
	default:
		return DeployerConfig{}, fmt.Errorf("config: DEPLOYMENT_MODE must be stub or live")
	}

	return cfg, nil
}
