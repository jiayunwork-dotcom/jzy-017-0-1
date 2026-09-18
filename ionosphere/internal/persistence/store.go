// Package persistence stores every computation (inputs and results, including
// rejected items) and supports condition-based retrieval.
package persistence

import (
	"context"
	"encoding/json"
	"time"
)

// Status values for a computation record.
const (
	StatusOK    = "ok"
	StatusError = "error"
)

// Mode values for a computation record.
const (
	ModeSingle = "single"
	ModeBatch  = "batch"
)

// Result is one persisted computation.
//
// Pointer fields are nil for rejected items whose corresponding input was
// missing, non-numeric or non-finite; Raw always retains the submitted JSON.
type Result struct {
	ID        int64          `json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	Mode      string         `json:"mode"`
	BatchID   string         `json:"batch_id,omitempty"`
	ItemIndex int            `json:"item_index"`
	Status    string         `json:"status"`

	PeakDensity *float64 `json:"peak_density,omitempty"`
	PeakHeight  *float64 `json:"peak_height,omitempty"`
	ScaleHeight *float64 `json:"scale_height,omitempty"`
	ZenithAngle *float64 `json:"zenith_angle,omitempty"`

	TECU               *float64 `json:"tec_u,omitempty"`
	ResultPeakDensity  *float64 `json:"result_peak_density,omitempty"`
	Panels             *int     `json:"panels,omitempty"`

	ErrorField   string `json:"error_field,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`

	// Raw is the original JSON object submitted for this computation.
	Raw json.RawMessage `json:"raw"`

	// ProfileJSON holds the returned profile when the caller requested one;
	// it is nil for TEC-only and default batch computations.
	ProfileJSON json.RawMessage `json:"profile,omitempty"`
}

// Filter restricts history queries. Zero/nil fields impose no restriction.
type Filter struct {
	Status     string
	Mode       string
	BatchID    string
	MinTECU    *float64
	MaxTECU    *float64
	Since      *time.Time
	Until      *time.Time
	Limit      int
	Offset     int
}

// Store is the persistence abstraction shared by the HTTP layer and tests.
type Store interface {
	// Save persists all results in one atomic write.
	Save(ctx context.Context, results []Result) error
	// Query retrieves past computations in reverse chronological order.
	Query(ctx context.Context, f Filter) ([]Result, error)
	// Ping verifies connectivity.
	Ping(ctx context.Context) error
	Close() error
}
