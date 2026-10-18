package blockchain

import (
	"context"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"
)

//go:embed abi/campaign_factory.json
var campaignFactoryABIJSON []byte

const (
	factoryCreateMethodName = "createCampaign"
	factoryCreatedEventName = "CampaignCreated"
)

// FactoryCampaignDeployerConfig configures an on-chain factory deployment.
type FactoryCampaignDeployerConfig struct {
	RPCURL         string
	FactoryAddress string
	PrivateKey     string
	ChainID        int
	ConfirmTimeout time.Duration
}

// FactoryCampaignDeployer submits createCampaign transactions to a deployed factory.
type FactoryCampaignDeployer struct {
	client         *ethclient.Client
	contractABI    abi.ABI
	factoryAddress common.Address
	chainID        *big.Int
	privateKey     string
	confirmTimeout time.Duration
}

func NewFactoryCampaignDeployer(
	ctx context.Context,
	cfg FactoryCampaignDeployerConfig,
) (*FactoryCampaignDeployer, error) {
	rpcURL := strings.TrimSpace(cfg.RPCURL)
	if rpcURL == "" {
		return nil, fmt.Errorf("blockchain: factory deployer: rpc url is required")
	}
	factoryRaw := strings.TrimSpace(cfg.FactoryAddress)
	if !common.IsHexAddress(factoryRaw) {
		return nil, fmt.Errorf("blockchain: factory deployer: factory address is invalid")
	}
	privateKey := strings.TrimSpace(cfg.PrivateKey)
	if privateKey == "" {
		return nil, fmt.Errorf("blockchain: factory deployer: private key is required")
	}
	if cfg.ChainID <= 0 {
		return nil, fmt.Errorf("blockchain: factory deployer: chain id must be positive")
	}
	confirmTimeout := cfg.ConfirmTimeout
	if confirmTimeout <= 0 {
		confirmTimeout = 2 * time.Minute
	}

	parsedABI, err := abi.JSON(strings.NewReader(string(campaignFactoryABIJSON)))
	if err != nil {
		return nil, fmt.Errorf("blockchain: factory deployer: parse abi: %w", err)
	}

	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("blockchain: factory deployer: dial rpc: %w", err)
	}

	return &FactoryCampaignDeployer{
		client:         client,
		contractABI:    parsedABI,
		factoryAddress: common.HexToAddress(factoryRaw),
		chainID:        big.NewInt(int64(cfg.ChainID)),
		privateKey:     privateKey,
		confirmTimeout: confirmTimeout,
	}, nil
}

func (d *FactoryCampaignDeployer) DeployCampaign(
	ctx context.Context,
	params CampaignDeploymentParams,
) (CampaignDeploymentResult, error) {
	if d == nil || d.client == nil {
		return CampaignDeploymentResult{}, fmt.Errorf("blockchain: factory deploy campaign: deployer is required")
	}
	if err := ctx.Err(); err != nil {
		return CampaignDeploymentResult{}, err
	}
	if params.CampaignID == uuid.Nil {
		return CampaignDeploymentResult{}, fmt.Errorf("blockchain: factory deploy campaign: campaign id is required")
	}
	if params.GoalAmount <= 0 {
		return CampaignDeploymentResult{}, fmt.Errorf("blockchain: factory deploy campaign: goal amount must be positive")
	}
	fundraiser := common.HexToAddress(strings.TrimSpace(params.FundraiserWallet))
	if fundraiser == (common.Address{}) {
		return CampaignDeploymentResult{}, fmt.Errorf("blockchain: factory deploy campaign: fundraiser wallet is required")
	}
	endAt, err := campaignEndAt(params.EndAt)
	if err != nil {
		return CampaignDeploymentResult{}, err
	}

	calldata, err := d.contractABI.Pack(
		factoryCreateMethodName,
		uuidToBytes16(params.CampaignID),
		fundraiser,
		big.NewInt(params.GoalAmount),
		endAt,
	)
	if err != nil {
		return CampaignDeploymentResult{}, fmt.Errorf("blockchain: factory deploy campaign: pack calldata: %w", err)
	}

	signer, err := transactSigner(d.privateKey, d.chainID)
	if err != nil {
		return CampaignDeploymentResult{}, err
	}

	nonce, err := d.client.PendingNonceAt(ctx, signer.From)
	if err != nil {
		return CampaignDeploymentResult{}, fmt.Errorf("blockchain: factory deploy campaign: pending nonce: %w", err)
	}

	gasPrice, err := d.client.SuggestGasPrice(ctx)
	if err != nil {
		return CampaignDeploymentResult{}, fmt.Errorf("blockchain: factory deploy campaign: suggest gas price: %w", err)
	}

	gasLimit, err := d.client.EstimateGas(ctx, ethereum.CallMsg{
		From: signer.From,
		To:   &d.factoryAddress,
		Data: calldata,
	})
	if err != nil {
		return CampaignDeploymentResult{}, fmt.Errorf("blockchain: factory deploy campaign: estimate gas: %w", err)
	}

	tx := types.NewTransaction(nonce, d.factoryAddress, nil, gasLimit, gasPrice, calldata)
	signedTx, err := signer.Signer(signer.From, tx)
	if err != nil {
		return CampaignDeploymentResult{}, fmt.Errorf("blockchain: factory deploy campaign: sign transaction: %w", err)
	}
	if err := d.client.SendTransaction(ctx, signedTx); err != nil {
		return CampaignDeploymentResult{}, fmt.Errorf("blockchain: factory deploy campaign: send transaction: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, d.confirmTimeout)
	defer cancel()

	receipt, err := bind.WaitMined(waitCtx, d.client, signedTx)
	if err != nil {
		return CampaignDeploymentResult{}, fmt.Errorf("blockchain: factory deploy campaign: wait mined: %w", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return CampaignDeploymentResult{}, fmt.Errorf("blockchain: factory deploy campaign: transaction reverted")
	}

	campaignAddress, logIndex, err := parseCampaignCreatedLog(d.contractABI, d.factoryAddress, receipt.Logs)
	if err != nil {
		return CampaignDeploymentResult{}, err
	}

	header, err := d.client.HeaderByNumber(ctx, receipt.BlockNumber)
	if err != nil {
		return CampaignDeploymentResult{}, fmt.Errorf("blockchain: factory deploy campaign: load block header: %w", err)
	}
	blockTime := time.Unix(int64(header.Time), 0).UTC()

	return CampaignDeploymentResult{
		ContractAddress: campaignAddress.Hex(),
		TxHash:          signedTx.Hash().Hex(),
		LogIndex:        logIndex,
		BlockNumber:     int(receipt.BlockNumber.Uint64()),
		EventCreatedAt:  blockTime,
	}, nil
}

func (d *FactoryCampaignDeployer) Close() error {
	if d == nil || d.client == nil {
		return nil
	}
	d.client.Close()
	return nil
}

func transactSigner(privateKeyHex string, chainID *big.Int) (*bind.TransactOpts, error) {
	keyHex := strings.TrimPrefix(strings.TrimSpace(privateKeyHex), "0x")
	key, err := crypto.HexToECDSA(keyHex)
	if err != nil {
		return nil, fmt.Errorf("blockchain: factory deployer: parse private key: %w", err)
	}
	signer, err := bind.NewKeyedTransactorWithChainID(key, chainID)
	if err != nil {
		return nil, fmt.Errorf("blockchain: factory deployer: transact signer: %w", err)
	}
	return signer, nil
}

func campaignEndAt(endAt time.Time) (*big.Int, error) {
	if endAt.IsZero() {
		return nil, fmt.Errorf("blockchain: factory deploy campaign: end time is required")
	}
	unix := endAt.UTC().Unix()
	if unix <= 0 {
		return nil, fmt.Errorf("blockchain: factory deploy campaign: end time must be positive")
	}
	return big.NewInt(unix), nil
}

func uuidToBytes16(id uuid.UUID) [16]byte {
	var out [16]byte
	copy(out[:], id[:])
	return out
}

func parseCampaignCreatedLog(
	contractABI abi.ABI,
	factoryAddress common.Address,
	logs []*types.Log,
) (common.Address, int, error) {
	event, ok := contractABI.Events[factoryCreatedEventName]
	if !ok {
		return common.Address{}, 0, fmt.Errorf("blockchain: factory deploy campaign: event %q is not in abi", factoryCreatedEventName)
	}

	for _, log := range logs {
		if log.Address != factoryAddress {
			continue
		}
		if len(log.Topics) == 0 || log.Topics[0] != event.ID {
			continue
		}
		if len(log.Topics) < 4 {
			return common.Address{}, 0, fmt.Errorf("blockchain: factory deploy campaign: campaign created log has insufficient topics")
		}
		campaign := indexedAddressFromTopic(log.Topics[2])
		if campaign == (common.Address{}) {
			return common.Address{}, 0, fmt.Errorf("blockchain: factory deploy campaign: campaign address is empty")
		}
		return campaign, int(log.Index), nil
	}

	return common.Address{}, 0, errors.New("blockchain: factory deploy campaign: campaign created log not found")
}

func indexedAddressFromTopic(topic common.Hash) common.Address {
	return common.BytesToAddress(topic.Bytes()[12:])
}

// ParseCampaignFactoryABI exposes ABI parsing for tests and contract upgrades.
func ParseCampaignFactoryABI(raw []byte) (abi.ABI, error) {
	return abi.JSON(strings.NewReader(string(raw)))
}

// CampaignIDToBytes16 hex-encodes the campaign UUID bytes for debugging deployments.
func CampaignIDToBytes16Hex(id uuid.UUID) string {
	return "0x" + hex.EncodeToString(id[:])
}
