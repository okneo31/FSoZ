// Package metrics는 Prometheus 노출.
//
// Counters:
//
//	zion1_events_processed_total{handler,status} — 핸들러별 결과
//
// Histograms:
//
//	zion1_signing_duration_seconds{handler} — digest pack + EIP-191 sign 합산 시간
//
// HTTP server는 별도 옵션 — Serve(addr) 호출하면 /metrics endpoint 띄움.
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// PromMetrics는 handlers.Metrics 인터페이스 구현체.
type PromMetrics struct {
	registry        *prometheus.Registry
	eventsProcessed *prometheus.CounterVec
	signingDuration *prometheus.HistogramVec
}

// New는 새 registry + collector 생성. 데몬당 1개 인스턴스.
func New() *PromMetrics {
	reg := prometheus.NewRegistry()
	m := &PromMetrics{
		registry: reg,
		eventsProcessed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "zion1_events_processed_total",
				Help: "Total events processed by handler, partitioned by result status.",
			},
			[]string{"handler", "status"},
		),
		signingDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "zion1_signing_duration_seconds",
				Help:    "Time spent packing digest + EIP-191 signing.",
				Buckets: prometheus.ExponentialBuckets(0.0001, 2, 12), // 100us .. ~400ms
			},
			[]string{"handler"},
		),
	}
	reg.MustRegister(m.eventsProcessed, m.signingDuration)
	return m
}

func (m *PromMetrics) IncEventProcessed(handler, status string) {
	m.eventsProcessed.WithLabelValues(handler, status).Inc()
}

func (m *PromMetrics) ObserveSigningDuration(handler string, seconds float64) {
	m.signingDuration.WithLabelValues(handler).Observe(seconds)
}

// Serve는 /metrics endpoint를 addr에 띄움. context cancel 시 graceful shutdown.
// addr 예: ":9100".
func (m *PromMetrics) Serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return fmt.Errorf("metrics server: %w", err)
	}
}
