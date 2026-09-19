package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// ErrNotFound is returned when a history record does not exist.
var ErrNotFound = errors.New("record not found")

// Record is one persisted calculation: its inputs and its results.
type Record struct {
	ID          int64           `json:"id"`
	CreatedAt   time.Time       `json:"createdAt"`
	Kind        string          `json:"kind"` // profile | tec | batch | demo
	BatchID     string          `json:"batchId,omitempty"`
	Input       LayerParams     `json:"input"`
	PeakHeight  float64         `json:"peakHeight"`  // m
	PeakDensity float64         `json:"peakDensity"` // computed density at the peak, m^-3
	TEC         float64         `json:"tec"`         // m^-2
	TECU        float64         `json:"tecu"`
	Result      json.RawMessage `json:"result,omitempty"` // full computation JSON
}

// HistoryFilter narrows history lookups. Nil fields are ignored.
type HistoryFilter struct {
	Kind    string
	Since   *time.Time
	Until   *time.Time
	MinTECU *float64
	MaxTECU *float64
	Limit   int
	Offset  int
}

// Store persists calculation records. Save fills in rec.ID and
// rec.CreatedAt. List returns records with the bulky profile stripped from
// the stored result; Get returns the full record.
type Store interface {
	Save(ctx context.Context, rec *Record) error
	Get(ctx context.Context, id int64) (*Record, error)
	List(ctx context.Context, f HistoryFilter) (records []*Record, total int, err error)
	Ping(ctx context.Context) error
	Close() error
}

// MemoryStore is an in-process Store used for tests and for running the
// service without a database. A mutex makes it safe for concurrent use.
type MemoryStore struct {
	mu      sync.Mutex
	nextID  int64
	records []*Record
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{nextID: 1}
}

// Save appends a copy of rec and assigns its ID and timestamp.
func (m *MemoryStore) Save(_ context.Context, rec *Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *rec
	cp.ID = m.nextID
	m.nextID++
	cp.CreatedAt = time.Now().UTC()
	cp.Result = append(json.RawMessage(nil), rec.Result...)
	m.records = append(m.records, &cp)
	rec.ID = cp.ID
	rec.CreatedAt = cp.CreatedAt
	return nil
}

// Get returns a copy of the record with the given ID.
func (m *MemoryStore) Get(_ context.Context, id int64) (*Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.records {
		if r.ID == id {
			cp := *r
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

// List returns the records matching f (profile stripped from the stored
// result) plus the total number of matches before pagination.
func (m *MemoryStore) List(_ context.Context, f HistoryFilter) ([]*Record, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var matched []*Record
	for _, r := range m.records {
		if f.Kind != "" && r.Kind != f.Kind {
			continue
		}
		if f.Since != nil && r.CreatedAt.Before(*f.Since) {
			continue
		}
		if f.Until != nil && r.CreatedAt.After(*f.Until) {
			continue
		}
		if f.MinTECU != nil && r.TECU < *f.MinTECU {
			continue
		}
		if f.MaxTECU != nil && r.TECU > *f.MaxTECU {
			continue
		}
		matched = append(matched, r)
	}
	total := len(matched)
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	start := f.Offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	out := make([]*Record, 0, end-start)
	for _, r := range matched[start:end] {
		cp := *r
		cp.Result = stripProfile(cp.Result)
		out = append(out, &cp)
	}
	return out, total, nil
}

// Ping always succeeds for the in-memory store.
func (m *MemoryStore) Ping(_ context.Context) error { return nil }

// Close is a no-op for the in-memory store.
func (m *MemoryStore) Close() error { return nil }

// stripProfile removes the "profile" member from a stored result document
// so history listings stay compact.
func stripProfile(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return raw
	}
	if _, ok := doc["profile"]; !ok {
		return raw
	}
	delete(doc, "profile")
	out, err := json.Marshal(doc)
	if err != nil {
		return raw
	}
	return out
}
