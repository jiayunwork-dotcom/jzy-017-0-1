package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestPGStoreIntegration exercises the PostgreSQL store end to end. It is
// skipped unless IONO_TEST_DATABASE_URL points at a test database, e.g.:
//
//	IONO_TEST_DATABASE_URL="postgres://iono:iono@localhost:5432/ionosphere?sslmode=disable" go test -run Integration ./...
func TestPGStoreIntegration(t *testing.T) {
	url := os.Getenv("IONO_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("IONO_TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.ExecContext(ctx, `TRUNCATE calculations`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	st := NewPGStore(db)

	rec := &Record{
		Kind:        "tec",
		Input:       testLayer(),
		PeakHeight:  300e3,
		PeakDensity: 1.2e12,
		TEC:         2.9e17,
		TECU:        29,
		Result:      json.RawMessage(`{"tecu":29,"profile":[{"height":0,"density":1}]}`),
	}
	if err := st.Save(ctx, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	if rec.ID == 0 {
		t.Fatalf("save did not assign an id")
	}

	got, err := st.Get(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Input.PeakDensity != 1.2e12 || got.TECU != 29 || got.Kind != "tec" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	// The full record keeps the stored profile.
	var full map[string]json.RawMessage
	if err := json.Unmarshal(got.Result, &full); err != nil {
		t.Fatalf("stored result is not JSON: %v", err)
	}
	if _, ok := full["profile"]; !ok {
		t.Fatalf("full record lost the stored profile")
	}

	// List strips the profile but keeps the rest.
	list, total, err := st.List(ctx, HistoryFilter{Kind: "tec", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("list total = %d, want 1", total)
	}
	var stripped map[string]json.RawMessage
	if err := json.Unmarshal(list[0].Result, &stripped); err != nil {
		t.Fatalf("listed result is not JSON: %v", err)
	}
	if _, ok := stripped["profile"]; ok {
		t.Fatalf("listed record should have the profile stripped")
	}

	// TECU range filter excludes the record.
	hi := 1000.0
	_, total, err = st.List(ctx, HistoryFilter{MinTECU: &hi, Limit: 10})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if total != 0 {
		t.Fatalf("filtered total = %d, want 0", total)
	}

	// Unknown id -> ErrNotFound.
	if _, err := st.Get(ctx, 424242); err != ErrNotFound {
		t.Fatalf("get unknown id: err = %v, want ErrNotFound", err)
	}
}
