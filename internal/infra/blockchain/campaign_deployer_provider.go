package blockchain

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DeploymentModeStub keeps phase-2 style deterministic addresses without RPC.
const DeploymentModeStub = "stub"

// DeploymentModeLive submits factory transactions on-chain.
const DeploymentModeLive = "live"

// ConfiguredCampaignDeployer bundles the selected deployer and optional cleanup.
type ConfiguredCampaignDeployer struct {
	Deployer CampaignDeployer
	Close    func() error
}

// NewConfiguredCampaignDeployer selects stub or live factory deployment.
func NewConfiguredCampaignDeployer(
	ctx context.Context,
	mode string,
	chainID int,
	rpcURL string,
	factoryAddress string,
	privateKey string,
	confirmTimeout time.Duration,
) (ConfiguredCampaignDeployer, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", DeploymentModeStub:
		return ConfiguredCampaignDeployer{
			Deployer: NewStubCampaignDeployer(),
			Close:    func() error { return nil },
		}, nil
	case DeploymentModeLive:
		factoryDeployer, err := NewFactoryCampaignDeployer(ctx, FactoryCampaignDeployerConfig{
			RPCURL:         rpcURL,
			FactoryAddress: factoryAddress,
			PrivateKey:     privateKey,
			ChainID:        chainID,
			ConfirmTimeout: confirmTimeout,
		})
		if err != nil {
			return ConfiguredCampaignDeployer{}, err
		}
		return ConfiguredCampaignDeployer{
			Deployer: factoryDeployer,
			Close:    factoryDeployer.Close,
		}, nil
	default:
		return ConfiguredCampaignDeployer{}, fmt.Errorf(
			"blockchain: campaign deployer: deployment mode %q is not supported",
			mode,
		)
	}
}
