package store

import (
	"path/filepath"
	"testing"
)

func TestBoltStore_PersistsAndQueries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	s1, err := NewBoltStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if s1.IsConsumed("key1") {
		t.Fatal("key1 should not be consumed yet")
	}
	s1.MarkConsumed("key1")
	s1.MarkConsumed("key2")
	if !s1.IsConsumed("key1") {
		t.Fatal("key1 should be consumed")
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	// 새 인스턴스에서 영속성 확인
	s2, err := NewBoltStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if !s2.IsConsumed("key1") {
		t.Fatal("key1 lost across restart")
	}
	if !s2.IsConsumed("key2") {
		t.Fatal("key2 lost across restart")
	}
	if s2.IsConsumed("key3") {
		t.Fatal("key3 should not be consumed")
	}

	snap := s2.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
}

func TestBoltStore_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := NewBoltStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.MarkConsumed("k")
	s.MarkConsumed("k") // double mark — no error
	if !s.IsConsumed("k") {
		t.Fatal()
	}
}
