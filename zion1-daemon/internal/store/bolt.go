// bolt.go — BoltDB (bbolt) 영구 store.
// 단일 파일 embedded DB. consumed events 추적 — daemon 재시작 후에도 보존.
//
// 스키마:
//
//	bucket "consumed" — key=event_key (string), value="1"
package store

import (
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

var consumedBucket = []byte("consumed")

// BoltStore는 bbolt 기반 Store. concurrent-safe — bbolt가 자체 락 보유.
type BoltStore struct {
	db *bolt.DB
}

// NewBoltStore는 파일을 열고 bucket 보장. 디렉토리는 호출자가 사전 생성.
func NewBoltStore(path string) (*BoltStore, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("bbolt open %s: %w", path, err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, e := tx.CreateBucketIfNotExists(consumedBucket)
		return e
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create bucket: %w", err)
	}
	return &BoltStore{db: db}, nil
}

// Close는 DB 종료. process 종료 시 호출 필수 (flush + lock 해제).
func (s *BoltStore) Close() error {
	return s.db.Close()
}

func (s *BoltStore) IsConsumed(key string) bool {
	var found bool
	_ = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(consumedBucket)
		if b == nil {
			return nil
		}
		found = b.Get([]byte(key)) != nil
		return nil
	})
	return found
}

func (s *BoltStore) MarkConsumed(key string) {
	_ = s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(consumedBucket).Put([]byte(key), []byte{1})
	})
}

func (s *BoltStore) Snapshot() map[string]bool {
	out := make(map[string]bool)
	_ = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(consumedBucket)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, _ []byte) error {
			out[string(k)] = true
			return nil
		})
	})
	return out
}
