package zionclient

import (
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
)

func TestDecodeResultEvent_TxEvents(t *testing.T) {
	rawTx := []byte("fake_tx_bytes_unique")
	re := rpctypes.ResultEvent{
		Query: "tm.event='Tx'",
		Data: cmttypes.EventDataTx{
			TxResult: abci.TxResult{
				Height: 42,
				Index:  0,
				Tx:     rawTx,
				Result: abci.ExecTxResult{
					Code: 0,
					Events: []abci.Event{
						{
							Type: "zion.seum.v1.EventEvmMappingRegistered",
							Attributes: []abci.EventAttribute{
								{Key: "evm_addr", Value: "0x1111111111111111111111111111111111111111", Index: true},
								{Key: "zion_addr", Value: "zion1abc...", Index: false},
							},
						},
						{
							Type: "zion.seum.v1.EventCapitalLocked",
							Attributes: []abci.EventAttribute{
								{Key: "evm_addr", Value: "0x2222222222222222222222222222222222222222", Index: true},
								{Key: "kwr_amount", Value: "10000", Index: false},
								{Key: "lock_id", Value: "0xaa", Index: false},
							},
						},
					},
				},
			},
		},
	}

	events := DecodeResultEvent(re)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}

	e1 := events[0]
	if e1.Type != "zion.seum.v1.EventEvmMappingRegistered" {
		t.Errorf("e1.Type = %s", e1.Type)
	}
	if e1.Attributes["evm_addr"] != "0x1111111111111111111111111111111111111111" {
		t.Errorf("e1 evm_addr = %s", e1.Attributes["evm_addr"])
	}
	if e1.BlockHeight != 42 {
		t.Errorf("e1.BlockHeight = %d", e1.BlockHeight)
	}
	if e1.TxHash == "" {
		t.Errorf("e1.TxHash empty")
	}
	if len(e1.TxHash) != 64 {
		t.Errorf("e1.TxHash len = %d, want 64 (sha256 hex)", len(e1.TxHash))
	}

	e2 := events[1]
	if e2.Type != "zion.seum.v1.EventCapitalLocked" {
		t.Errorf("e2.Type = %s", e2.Type)
	}
	if e2.Attributes["kwr_amount"] != "10000" {
		t.Errorf("e2 kwr_amount = %s", e2.Attributes["kwr_amount"])
	}
	if e2.TxHash != e1.TxHash {
		t.Errorf("same tx should produce same hash: %s vs %s", e1.TxHash, e2.TxHash)
	}
}

func TestDecodeResultEvent_NonTxData(t *testing.T) {
	// 비-Tx 데이터는 0 events
	re := rpctypes.ResultEvent{
		Query: "tm.event='NewBlock'",
		Data:  cmttypes.EventDataNewBlockEvents{},
	}
	events := DecodeResultEvent(re)
	if len(events) != 0 {
		t.Fatalf("want 0 events for non-Tx data, got %d", len(events))
	}
}

func TestDecodeResultEvent_EmptyEvents(t *testing.T) {
	// Tx 결과는 있는데 events가 비어 있는 경우
	re := rpctypes.ResultEvent{
		Data: cmttypes.EventDataTx{
			TxResult: abci.TxResult{
				Height: 1,
				Tx:     []byte("tx"),
				Result: abci.ExecTxResult{Events: nil},
			},
		},
	}
	events := DecodeResultEvent(re)
	if len(events) != 0 {
		t.Fatalf("want 0, got %d", len(events))
	}
}
