// Package zionclient는 Zion (Cosmos SDK / CometBFT) 체인 이벤트 구독 + 디코딩.
//
// Subscribe는 CometBFT WebSocket query로 EventDataTx를 받아, 그 안의 abci.Event 들을
// TypedEvent (Type + Attributes map)로 평탄화. 한 tx에 여러 이벤트가 있으면 각각 emit.
package zionclient

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto/tmhash"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
)

// Client는 CometBFT WebSocket subscribe + state query 래퍼.
type Client struct {
	rpc     *rpchttp.HTTP
	chainID string
}

// New는 WebSocket URL과 chain ID로 클라이언트 생성.
//
//	rpcURL 예: "http://localhost:26657" — CometBFT v0.38는 HTTP URL을 받고 내부적으로 ws 사용.
func New(rpcURL, chainID string) (*Client, error) {
	c, err := rpchttp.New(rpcURL, "/websocket")
	if err != nil {
		return nil, fmt.Errorf("rpchttp.New: %w", err)
	}
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("rpc client start: %w", err)
	}
	return &Client{rpc: c, chainID: chainID}, nil
}

// Close는 WebSocket 종료.
func (c *Client) Close() error {
	return c.rpc.Stop()
}

// Subscribe는 query string으로 이벤트 구독. CometBFT query 구문 사용.
//
//	예: "tm.event='Tx' AND zion.seum.v1.EventEvmMappingRegistered.evm_addr EXISTS"
//
// 한 tx에 여러 abci.Event가 있으면 각각 별개 TypedEvent로 채널에 emit.
// 디코딩 실패 시 그 이벤트 한 건만 drop (로그는 caller 책임).
func (c *Client) Subscribe(ctx context.Context, subscriber, query string) (<-chan TypedEvent, error) {
	out := make(chan TypedEvent, 64)
	eventsCh, err := c.rpc.Subscribe(ctx, subscriber, query)
	if err != nil {
		return nil, fmt.Errorf("rpc subscribe: %w", err)
	}
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case re, ok := <-eventsCh:
				if !ok {
					return
				}
				for _, te := range DecodeResultEvent(re) {
					select {
					case out <- te:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return out, nil
}

// TypedEvent는 디코드된 Zion 이벤트.
type TypedEvent struct {
	Type        string            // 예: "zion.seum.v1.EventEvmMappingRegistered"
	Attributes  map[string]string // raw key=value pairs (abci.EventAttribute에서 추출)
	TxHash      string            // hex sha256(raw tx bytes), 대소문자 lower
	BlockHeight int64
	Timestamp   time.Time
}

// DecodeResultEvent는 CometBFT ResultEvent → 0..N개 TypedEvent.
// EventDataTx만 처리 (블록 단위 이벤트는 v1에서 무시).
// Public — 단위 테스트가 직접 호출.
func DecodeResultEvent(re rpctypes.ResultEvent) []TypedEvent {
	var out []TypedEvent
	switch data := re.Data.(type) {
	case cmttypes.EventDataTx:
		txHash := hex.EncodeToString(tmhash.Sum(data.Tx))
		for _, ev := range data.TxResult.Result.Events {
			out = append(out, abciEventToTyped(ev, txHash, data.Height))
		}
	case cmttypes.EventDataNewBlockEvents:
		// 블록 단위 이벤트 — Phase 2에서 검토. 현재 unused.
	}
	return out
}

func abciEventToTyped(ev abci.Event, txHash string, height int64) TypedEvent {
	attrs := make(map[string]string, len(ev.Attributes))
	for _, a := range ev.Attributes {
		attrs[a.Key] = a.Value
	}
	return TypedEvent{
		Type:        ev.Type,
		Attributes:  attrs,
		TxHash:      txHash,
		BlockHeight: height,
		Timestamp:   time.Now().UTC(), // 정확한 block time은 별도 query 필요 — 대략값
	}
}

// MockSource는 테스트용 — fixture 파일에서 이벤트 inject.
type MockSource struct {
	events []TypedEvent
}

// NewMockSource는 mock 이벤트 소스 (테스트/개발용).
func NewMockSource(events []TypedEvent) *MockSource {
	return &MockSource{events: events}
}

// Stream은 이벤트를 채널로 흘림.
func (m *MockSource) Stream(ctx context.Context) <-chan TypedEvent {
	out := make(chan TypedEvent, len(m.events))
	go func() {
		defer close(out)
		for _, ev := range m.events {
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}
