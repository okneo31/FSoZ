package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/okneo31/zion1-daemon/internal/store"
	"github.com/okneo31/zion1-daemon/internal/zionclient"
)

// fakeHandler — calls counter, optional error.
type fakeHandler struct {
	types []string
	calls int
	err   error
}

func (f *fakeHandler) EventTypes() []string { return f.types }
func (f *fakeHandler) Handle(_ context.Context, _ zionclient.TypedEvent) error {
	f.calls++
	return f.err
}

func TestDispatcher_RoutesToRegistered(t *testing.T) {
	st, _ := store.NewMemoryStore("")
	d := NewDispatcher(st)
	h := &fakeHandler{types: []string{"zion.seum.v1.EventA"}}
	d.Register(h)

	ev := zionclient.TypedEvent{Type: "zion.seum.v1.EventA", TxHash: "tx1"}
	if err := d.Dispatch(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if h.calls != 1 {
		t.Fatalf("calls = %d, want 1", h.calls)
	}
}

func TestDispatcher_DedupsByTxHashAndType(t *testing.T) {
	st, _ := store.NewMemoryStore("")
	d := NewDispatcher(st)
	h := &fakeHandler{types: []string{"E"}}
	d.Register(h)

	ev := zionclient.TypedEvent{Type: "E", TxHash: "tx1"}
	_ = d.Dispatch(context.Background(), ev)
	_ = d.Dispatch(context.Background(), ev) // duplicate
	if h.calls != 1 {
		t.Fatalf("calls = %d, want 1 (dedup)", h.calls)
	}
	// 다른 TxHash → 처리
	ev2 := zionclient.TypedEvent{Type: "E", TxHash: "tx2"}
	_ = d.Dispatch(context.Background(), ev2)
	if h.calls != 2 {
		t.Fatalf("calls = %d, want 2", h.calls)
	}
}

func TestDispatcher_SkipsUnregistered(t *testing.T) {
	st, _ := store.NewMemoryStore("")
	d := NewDispatcher(st)
	// no handlers
	ev := zionclient.TypedEvent{Type: "unknown", TxHash: "tx"}
	if err := d.Dispatch(context.Background(), ev); err != nil {
		t.Fatalf("unregistered should silently skip, got %v", err)
	}
}

func TestDispatcher_PropagatesErrorNoDedup(t *testing.T) {
	st, _ := store.NewMemoryStore("")
	d := NewDispatcher(st)
	h := &fakeHandler{types: []string{"E"}, err: errors.New("boom")}
	d.Register(h)

	ev := zionclient.TypedEvent{Type: "E", TxHash: "tx"}
	if err := d.Dispatch(context.Background(), ev); err == nil {
		t.Fatal("expected error")
	}
	// 에러 시 mark consumed 안 했으니 다시 처리됨
	_ = d.Dispatch(context.Background(), ev)
	if h.calls != 2 {
		t.Fatalf("calls = %d, want 2 (no dedup on error)", h.calls)
	}
}
