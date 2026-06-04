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
	"log/slog"

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
	logger      *slog.Logger
}

// NewEVMRagequitHandler는 핸들러 생성. broadcaster가 nil이면 logging-only 모드.
// logger nil이면 slog.Default() 사용.
func NewEVMRagequitHandler(store store.Store, broadcaster ZionTxBroadcaster, metrics Metrics, logger *slog.Logger) *EVMRagequitHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &EVMRagequitHandler{
		store:       store,
		broadcaster: broadcaster,
		metrics:     MetricsOrNoop(metrics),
		logger:      logger,
	}
}

// Run은 EVM 이벤트 채널을 소비. ctx cancel될 때까지.
// 처리 실패는 dead-letter 큐 (store의 별도 prefix) 에 영구 기록 + structured log + 메트릭.
// EVM ragequit은 사용자 인생의 일회성 이벤트 — silent drop 절대 금지.
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
				// dead-letter 영구 기록 — 재시작 시 dispatcher가 이 키로 dedup 가능.
				failedKey := fmt.Sprintf("failed:evm_ragequit:%s:%d", ev.TxHash.Hex(), ev.LogIndex)
				h.store.MarkConsumed(failedKey)
				h.logger.Error("EVM RagequitRequest 처리 실패 — DEAD-LETTER 큐 기록 (수동 대응 필요)",
					"citizen_evm", ev.Citizen.Hex(),
					"kwr_amount", ev.KWRAmount.String(),
					"evm_tx", ev.TxHash.Hex(),
					"log_index", ev.LogIndex,
					"block_number", ev.BlockNumber,
					"error", err.Error(),
					"failed_key", failedKey,
				)
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
	// dead-letter도 dedup 대상 — 이전 영구 실패 이벤트가 재시작 시 재시도 안 되도록.
	failedKey := fmt.Sprintf("failed:evm_ragequit:%s:%d", ev.TxHash.Hex(), ev.LogIndex)
	if h.store.IsConsumed(failedKey) {
		h.logger.Warn("evm ragequit 이벤트 이전에 영구 실패 — skip (dead-letter)",
			"failed_key", failedKey,
			"citizen", ev.Citizen.Hex(),
		)
		return nil
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
