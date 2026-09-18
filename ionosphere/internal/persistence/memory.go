package persistence

import (
	"context"
	"sync"
	"time"
)

// MemoryStore is an in-process Store used by tests and when PostgreSQL is
// unavailable. It is safe for concurrent use.
type MemoryStore struct {
	mu      sync.Mutex
	nextID  int64
	records []Result
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// Save implements Store.
func (m *MemoryStore) Save(_ context.Context, results []Result) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range results {
		m.nextID++
		results[i].ID = m.nextID
		if results[i].CreatedAt.IsZero() {
			results[i].CreatedAt = time.Now().UTC()
		}
		m.records = append(m.records, results[i])
	}
	return nil
}

// Query implements Store.
func (m *MemoryStore) Query(_ context.Context, f Filter) ([]Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []Result
	for i := len(m.records) - 1; i >= 0; i-- {
		r := m.records[i]
		if f.Status != "" && r.Status != f.Status {
			continue
		}
		if f.Mode != "" && r.Mode != f.Mode {
			continue
		}
		if f.BatchID != "" && r.BatchID != f.BatchID {
			continue
		}
		if (f.MinTECU != nil || f.MaxTECU != nil) && r.TECU == nil {
			continue
		}
		if f.MinTECU != nil && *r.TECU < *f.MinTECU {
			continue
		}
		if f.MaxTECU != nil && *r.TECU > *f.MaxTECU {
			continue
		}
		if f.Since != nil && r.CreatedAt.Before(*f.Since) {
			continue
		}
		if f.Until != nil && r.CreatedAt.After(*f.Until) {
			continue
		}
		out = append(out, r)
	}
	if f.Offset >= len(out) {
		return []Result{}, nil
	}
	if f.Offset > 0 {
		out = out[f.Offset:]
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	if out == nil {
		out = []Result{}
	}
	return out, nil
}

// Ping implements Store.
func (m *MemoryStore) Ping(_ context.Context) error { return nil }

// Close implements Store.
func (m *MemoryStore) Close() error { return nil }
