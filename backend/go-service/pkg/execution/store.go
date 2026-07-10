package execution

import (
	"context"
	"sync"
)

type IntentStore interface {
	GetIntent(ctx context.Context, intentID string) (*IntentRecord, error)
	SaveIntent(ctx context.Context, record IntentRecord) error
}

type MemoryIntentStore struct {
	mu      sync.RWMutex
	records map[string]IntentRecord
}

func NewMemoryIntentStore() *MemoryIntentStore {
	return &MemoryIntentStore{records: map[string]IntentRecord{}}
}

func (s *MemoryIntentStore) GetIntent(ctx context.Context, intentID string) (*IntentRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	record, ok := s.records[intentID]
	s.mu.RUnlock()
	if !ok {
		return nil, nil
	}
	return &record, nil
}

func (s *MemoryIntentStore) SaveIntent(ctx context.Context, record IntentRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.records[record.Intent.IntentID] = record
	s.mu.Unlock()
	return nil
}
