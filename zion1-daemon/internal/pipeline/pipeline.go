// Package pipeline는 데몬의 메인 이벤트 루프.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/okneo31/zion1-daemon/internal/handlers"
	"github.com/okneo31/zion1-daemon/internal/zionclient"
)

// Pipeline은 Zion 이벤트 소스 → handler dispatch → 에러/재시도.
type Pipeline struct {
	source     <-chan zionclient.TypedEvent
	dispatcher *handlers.Dispatcher
	maxRetries int
	backoff    time.Duration
	logger     *slog.Logger
}

// New는 파이프라인 생성.
func New(source <-chan zionclient.TypedEvent, dispatcher *handlers.Dispatcher, maxRetries, backoffSeconds int, logger *slog.Logger) *Pipeline {
	return &Pipeline{
		source:     source,
		dispatcher: dispatcher,
		maxRetries: maxRetries,
		backoff:    time.Duration(backoffSeconds) * time.Second,
		logger:     logger,
	}
}

// Run은 ctx가 cancel되거나 source 닫힐 때까지 이벤트 처리.
func (p *Pipeline) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-p.source:
			if !ok {
				return fmt.Errorf("source channel closed")
			}
			p.processWithRetry(ctx, ev)
		}
	}
}

func (p *Pipeline) processWithRetry(ctx context.Context, ev zionclient.TypedEvent) {
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		if attempt > 0 {
			p.logger.Warn("retry event",
				"type", ev.Type,
				"tx_hash", ev.TxHash,
				"attempt", attempt,
			)
			select {
			case <-ctx.Done():
				return
			case <-time.After(p.backoff):
			}
		}
		err := p.dispatcher.Dispatch(ctx, ev)
		if err == nil {
			p.logger.Info("processed event",
				"type", ev.Type,
				"tx_hash", ev.TxHash,
			)
			return
		}
		// 영구 에러는 retry 무의미 — dispatcher가 이미 markConsumed 안 한 상태.
		// 영구 표시 + dispatch에서 다시 처리하지 않도록 별도 dead-letter 메커니즘이 store에 있어야 함.
		// 현재는 abandoned 로깅 후 종료 (영구 에러 메트릭 분리).
		if IsPermanent(err) {
			p.logger.Error("dispatch permanent error — abandoning event (will NOT mark consumed)",
				"type", ev.Type,
				"tx_hash", ev.TxHash,
				"error", err.Error(),
			)
			p.dispatcher.MarkPermanentlyFailed(ev)
			return
		}
		p.logger.Error("dispatch error (transient — will retry)",
			"type", ev.Type,
			"tx_hash", ev.TxHash,
			"error", err.Error(),
		)
	}
	p.logger.Error("event abandoned after retries (transient retries exhausted)",
		"type", ev.Type,
		"tx_hash", ev.TxHash,
	)
}
