// logging_broadcaster.go — ZionTxBroadcaster의 명시적 logging-only 구현.
//
// nil broadcaster와 다른 점:
//
//	(1) 명시적 타입 — 의도가 코드로 드러남 (nil은 사고로 보임)
//	(2) structured 로그 — 운영팀이 grep 가능 (handler="evm_ragequit" action="unlock_pending" ...)
//	(3) 메트릭 카운터 — Prometheus에 "logged_only" 상태로 누적
//
// Phase 2 (cosmos-sdk client 통합) 까지의 운영 안전망.
// 이 broadcaster를 쓰면 EVM ragequit 이벤트는 데몬 로그에 100% 캡처되지만
// *Zion 측 자금 unlock은 실행 안 됨* — 운영자가 수동 대응 필요.
package handlers

import (
	"context"
	"log/slog"
)

// LoggingBroadcaster는 unlock 요청을 로그·메트릭으로만 캡처.
// 실제 Zion tx broadcast는 안 함 — Phase 2 placeholder.
type LoggingBroadcaster struct {
	logger  *slog.Logger
	metrics Metrics
}

// NewLoggingBroadcaster는 명시적 logging-only broadcaster 생성.
func NewLoggingBroadcaster(logger *slog.Logger, metrics Metrics) *LoggingBroadcaster {
	return &LoggingBroadcaster{
		logger:  logger,
		metrics: MetricsOrNoop(metrics),
	}
}

// BroadcastUnlockCapital은 ZionTxBroadcaster 인터페이스 구현.
// 항상 nil 반환 (= 성공) — 호출자는 mark_consumed 진행. 실제 Zion 액션은 발생 안 함.
func (b *LoggingBroadcaster) BroadcastUnlockCapital(ctx context.Context, citizen string, kwrAmount string, evmTxHash string) error {
	b.logger.Warn("zion unlock requested (LOGGING-ONLY — Phase 2 cosmos-sdk client 미구현)",
		"handler", "evm_ragequit",
		"action", "unlock_pending",
		"citizen_evm", citizen,
		"kwr_amount", kwrAmount,
		"evm_tx", evmTxHash,
		"note", "운영자가 Zion 측 수동 unlock 필요",
	)
	b.metrics.IncEventProcessed("evm_ragequit", "logged_only")
	return nil
}
