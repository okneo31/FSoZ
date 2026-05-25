// Package store는 데몬의 영구 상태 (consumed events, retry queue).
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// Store는 처리된 이벤트 추적.
type Store interface {
	IsConsumed(key string) bool
	MarkConsumed(key string)
	Snapshot() map[string]bool
}

// MemoryStore는 인메모리 + 파일 스냅샷 (간단한 영속).
type MemoryStore struct {
	mu       sync.RWMutex
	consumed map[string]bool
	path     string // 비어있으면 메모리만
}

// NewMemoryStore는 path가 있으면 시작 시 로드, 변경 시 저장.
func NewMemoryStore(path string) (*MemoryStore, error) {
	ms := &MemoryStore{
		consumed: make(map[string]bool),
		path:     path,
	}
	if path == "" {
		return ms, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ms, nil
		}
		return nil, fmt.Errorf("read state %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &ms.consumed); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	return ms, nil
}

func (ms *MemoryStore) IsConsumed(key string) bool {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.consumed[key]
}

func (ms *MemoryStore) MarkConsumed(key string) {
	ms.mu.Lock()
	ms.consumed[key] = true
	ms.mu.Unlock()
	if ms.path != "" {
		_ = ms.flush() // best-effort; log error in production
	}
}

func (ms *MemoryStore) Snapshot() map[string]bool {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	out := make(map[string]bool, len(ms.consumed))
	for k, v := range ms.consumed {
		out[k] = v
	}
	return out
}

func (ms *MemoryStore) flush() error {
	ms.mu.RLock()
	data, err := json.MarshalIndent(ms.consumed, "", "  ")
	ms.mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(ms.path, data, 0644)
}
