// Package handlers는 Zion 이벤트 → EVM action 변환.
package handlers

import (
	"context"
	"fmt"

	"github.com/okneo31/zion1-daemon/internal/evmclient"
	"github.com/okneo31/zion1-daemon/internal/signer"
	"github.com/okneo31/zion1-daemon/internal/store"
	"github.com/okneo31/zion1-daemon/internal/zionclient"
)

// Handler는 단일 이벤트 type을 처리.
type Handler interface {
	// EventTypes는 이 핸들러가 처리할 Zion 이벤트 type 목록.
	EventTypes() []string

	// Handle은 이벤트 한 건 처리. idempotent해야 함 (consumed 체크 이미 store에서).
	Handle(ctx context.Context, ev zionclient.TypedEvent) error
}

// Dispatcher는 이벤트를 적합한 handler로 라우팅.
type Dispatcher struct {
	handlers map[string]Handler
	store    store.Store
}

// NewDispatcher는 빈 dispatcher 생성.
func NewDispatcher(store store.Store) *Dispatcher {
	return &Dispatcher{
		handlers: make(map[string]Handler),
		store:    store,
	}
}

// Register는 handler 등록. 같은 event type에 multi-handler는 미지원 (v1).
func (d *Dispatcher) Register(h Handler) {
	for _, t := range h.EventTypes() {
		d.handlers[t] = h
	}
}

// Dispatch는 이벤트 1건 처리. nonce 기반 dedup + 에러 전파.
func (d *Dispatcher) Dispatch(ctx context.Context, ev zionclient.TypedEvent) error {
	h, ok := d.handlers[ev.Type]
	if !ok {
		return nil // unregistered event — silently skip
	}
	// dedup: same TxHash + event type 조합으로 1회용
	key := ev.TxHash + ":" + ev.Type
	if d.store.IsConsumed(key) {
		return nil
	}
	if err := h.Handle(ctx, ev); err != nil {
		return fmt.Errorf("handler %s: %w", ev.Type, err)
	}
	d.store.MarkConsumed(key)
	return nil
}

// HandlerContext는 모든 핸들러가 공유하는 의존성 묶음.
type HandlerContext struct {
	Signer    *signer.Signer
	EVMClient *evmclient.Client
	Store     store.Store
	SignatureTTLSeconds int64
}
