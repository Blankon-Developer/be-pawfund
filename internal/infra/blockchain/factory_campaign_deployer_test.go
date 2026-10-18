package blockchain

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/google/uuid"
)

func TestParseCampaignFactoryABI(t *testing.T) {
	parsed, err := ParseCampaignFactoryABI(campaignFactoryABIJSON)
	if err != nil {
		t.Fatalf("ParseCampaignFactoryABI() error = %v", err)
	}
	if _, ok := parsed.Methods[factoryCreateMethodName]; !ok {
		t.Fatalf("missing method %q", factoryCreateMethodName)
	}
	if _, ok := parsed.Events[factoryCreatedEventName]; !ok {
		t.Fatalf("missing event %q", factoryCreatedEventName)
	}
}

func TestFactoryCreateCampaignCalldataPacks(t *testing.T) {
	parsed, err := ParseCampaignFactoryABI(campaignFactoryABIJSON)
	if err != nil {
		t.Fatalf("ParseCampaignFactoryABI() error = %v", err)
	}

	campaignID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdef")
	endAt := time.Date(2026, 10, 25, 4, 0, 0, 0, time.UTC)
	endAtUnix, err := campaignEndAt(endAt)
	if err != nil {
		t.Fatalf("campaignEndAt() error = %v", err)
	}

	calldata, err := parsed.Pack(
		factoryCreateMethodName,
		uuidToBytes16(campaignID),
		common.HexToAddress("0x0000000000000000000000000000000000000001"),
		big.NewInt(10_000_000_000),
		endAtUnix,
	)
	if err != nil {
		t.Fatalf("Pack() error = %v", err)
	}
	if len(calldata) < 4 {
		t.Fatalf("calldata = %x", calldata)
	}
}

func TestParseCampaignCreatedLog(t *testing.T) {
	parsed, err := ParseCampaignFactoryABI(campaignFactoryABIJSON)
	if err != nil {
		t.Fatalf("ParseCampaignFactoryABI() error = %v", err)
	}

	factory := common.HexToAddress("0x64bD5192cB735CB25C082Db33730691E428fb9AB")
	campaign := common.HexToAddress("0x00000000000000000000000000000000000000c1")
	fundraiser := common.HexToAddress("0x00000000000000000000000000000000000000f1")
	campaignID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdef")
	endAt := uint64(1_761_369_600)

	data, err := parsed.Events[factoryCreatedEventName].Inputs.NonIndexed().Pack(
		big.NewInt(10_000_000_000),
		new(big.Int).SetUint64(endAt),
	)
	if err != nil {
		t.Fatalf("pack event data: %v", err)
	}

	campaignIDBytes := uuidToBytes16(campaignID)
	var campaignIDTopic common.Hash
	copy(campaignIDTopic[16:], campaignIDBytes[:])

	log := &types.Log{
		Address: factory,
		Index:   2,
		Topics: []common.Hash{
			parsed.Events[factoryCreatedEventName].ID,
			campaignIDTopic,
			indexedAddressTopic(campaign),
			indexedAddressTopic(fundraiser),
		},
		Data: data,
	}

	found, logIndex, err := parseCampaignCreatedLog(parsed, factory, []*types.Log{log})
	if err != nil {
		t.Fatalf("parseCampaignCreatedLog() error = %v", err)
	}
	if found != campaign || logIndex != 2 {
		t.Fatalf("parsed = %s/%d, want %s/2", found.Hex(), logIndex, campaign.Hex())
	}
}

func indexedAddressTopic(address common.Address) common.Hash {
	var topic common.Hash
	copy(topic[12:], address.Bytes())
	return topic
}
