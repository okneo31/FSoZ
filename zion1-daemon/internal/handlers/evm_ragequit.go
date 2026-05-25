// evm_ragequit는 *역방향* 핸들러 — EVM RagequitRequest → Zion x/seum.MsgUnlockCapital.
//
// 다른 핸들러 (Zion → EVM)와 달리 dispatcher.Register 안 함 — Run 메소드가 자체 루프.
// EVM 측 evmclient.WatchRagequit 채널을 받아 처리.
//
// 현재 한계 (Phase 2 작업):
//
//	Zion tx 실제 빌드/서명/브로드캐스트는 cosmos-sdk client 추가 필요.
//	여기선 이벤트 수신 + 로깅 + idempotency 체크까지만 구현 — 실제 unlock 호출은 TODO.
//	x/seum 모듈 구현이 끝나면 ZionTxBroadcaster interface로 swap.
package handlers

import (
	"context"
	"fmt"

	"github.com/okneo31/zion1-daemon/internal/evmclient"
	"github.com/okneo31/zion1-daemon/internal/store"
)

// ZionTxBroadcaster는 Zion 체인에 MsgUnlockCapitalForSeum tx를 송출.
// Phase 2에서 cosmos-sdk client 기반 구현.
type ZionTxBroadcaster interface {
	BroadcastUnlockCapital(ctx context.Context, citizen string, kwrAmount string, evmTxHash string) error
}

// EVMRagequitHandler는 EVM RagequitRequest를 처리.
type EVMRagequitHandler struct {
	store       store.Store
	broadcaster ZionTxBroadcaster
	metrics     Metrics
}

// NewEVMRagequitHandler는 핸들러 생성. broadcaster가 nil이면 logging-only 모드.
func NewEVMRagequitHandler(store store.Store, broadcaster ZionTxBroadcaster, metrics Metrics) *EVMRagequitHandler {
	return &EVMRagequitHandler{
		store:       store,
		broadcaster: broadcaster,
		metrics:     MetricsOrNoop(metrics),
	}
}

// Run은 EVM 이벤트 채널을 소비. ctx cancel될 때까지.
func (h *EVMRagequitHandler) Run(ctx context.Context, in <-chan evmclient.RagequitEvent) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-in:
			if !ok {
				return fmt.Errorf("evm ragequit channel closed")
			}
			if err := h.handle(ctx, ev); err != nil {
				h.metrics.IncEventProcessed("evm_ragequit", "err")
				// 영구 실패도 caller에 전파 — pipeline-level retry 같은 게 추후 필요
				continue
			}
			h.metrics.IncEventProcessed("evm_ragequit", "ok")
		}
	}
}

func (h *EVMRagequitHandler) handle(ctx context.Context, ev evmclient.RagequitEvent) error {
	key := fmt.Sprintf("evm_ragequit:%s:%d", ev.TxHash.Hex(), ev.LogIndex)
	if h.store.IsConsumed(key) {
		return nil // 이미 처리됨
	}

	if h.broadcaster == nil {
		// Phase 1: 로깅만, 실제 Zion tx는 미구현
		// 운영 시 broadcaster nil이면 EVM ragequit 후 Zion 측 자금 잠금 풀리지 않음 — 알람 대상
		h.store.MarkConsumed(key)
		return nil
	}

	if err := h.broadcaster.BroadcastUnlockCapital(
		ctx,
		ev.Citizen.Hex(),
		ev.KWRAmount.String(),
		ev.TxHash.Hex(),
	); err != nil {
		return fmt.Errorf("broadcast unlock: %w", err)
	}
	h.store.MarkConsumed(key)
	return nil
}
