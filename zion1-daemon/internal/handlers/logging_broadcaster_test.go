package handlers

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestLoggingBroadcaster_LogsWithStructuredFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	b := NewLoggingBroadcaster(logger, nil)

	err := b.BroadcastUnlockCapital(
		context.Background(),
		"0xabc1234567890abcdef1234567890abcdef12345",
		"123456789",
		"0xdeadbeef",
	)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}
	out := buf.String()
	// 필수 필드들이 로그에 등장하는지
	required := []string{
		"handler=evm_ragequit",
		"action=unlock_pending",
		"citizen_evm=0xabc",
		"kwr_amount=123456789",
		"evm_tx=0xdeadbeef",
		"LOGGING-ONLY",
	}
	for _, want := range required {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q\nfull log: %s", want, out)
		}
	}
}

func TestLoggingBroadcaster_NilMetricsSafe(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	b := NewLoggingBroadcaster(logger, nil) // nil metrics
	if err := b.BroadcastUnlockCapital(context.Background(), "0xabc", "1", "0xdef"); err != nil {
		t.Fatal(err)
	}
}
