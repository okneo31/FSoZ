// Package zionclient는 Zion (Cosmos SDK / CometBFT) 체인 이벤트 구독.
package zionclient

import (
	"context"
	"fmt"
	"time"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cometbft/cometbft/types"
)

// Client는 CometBFT WebSocket subscribe + state query 래퍼.
type Client struct {
	rpc      *rpchttp.HTTP
	chainID  string
}

// New는 WebSocket URL과 chain ID로 클라이언트 생성.
//   rpcURL 예: "ws://localhost:26657/websocket" 또는 "http://localhost:26657"
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
//   예: "tm.event='Tx' AND zion.pop.v1.EventPoPVerified.evm_addr EXISTS"
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
			case ev := <-eventsCh:
				if ev.Data == nil {
					continue
				}
				te, decErr := decodeEvent(ev)
				if decErr != nil {
					// TODO: log instead of swallow
					continue
				}
				select {
				case out <- te:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// TypedEvent는 디코드된 Zion 이벤트.
type TypedEvent struct {
	Type        string                  // 예: "zion.pop.v1.EventPoPVerified"
	Attributes  map[string]string       // raw key=value pairs
	TxHash      string
	BlockHeight int64
	Timestamp   time.Time
}

func decodeEvent(ev interface{}) (TypedEvent, error) {
	// CometBFT 이벤트는 abci.Event{Type, Attributes []EventAttribute}
	// rpc.Subscribe는 rpctypes.ResultEvent를 반환 — .Data가 EventDataTx 등
	// TODO: 실제 디코딩 — types.EventDataTx에서 abci.Event 추출
	_ = ev
	return TypedEvent{}, fmt.Errorf("TODO: implement CometBFT event decoding")
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
