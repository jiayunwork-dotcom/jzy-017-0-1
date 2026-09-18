package persistence_test

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"ionosphere/internal/persistence"
)

// TestPostgresPersistence runs only when CHAPMAN_TEST_DATABASE_URL points at a
// reachable PostgreSQL instance (the compose database provides this).
func TestPostgresPersistence(t *testing.T) {
	url := os.Getenv("CHAPMAN_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("CHAPMAN_TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := persistence.NewPostgresStore(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer store.Close()

	// 每次运行使用唯一 batch id，保证在共享数据库上可重复执行。
	batchID := "batch-int-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	nm, hm, h, chi := 1.2e12, 300e3, 60e3, 0.0
	tecU, peak := 24.5, 1.2e12
	panels := 2048
	raw, _ := json.Marshal(map[string]any{"peak_density": nm})
	prof, _ := json.Marshal(map[string]any{"peak_height": hm})

	okRow := persistence.Result{
		Mode: persistence.ModeSingle, Status: persistence.StatusOK,
		PeakDensity: &nm, PeakHeight: &hm, ScaleHeight: &h, ZenithAngle: &chi,
		TECU: &tecU, ResultPeakDensity: &peak, Panels: &panels,
		Raw: raw, ProfileJSON: prof,
	}
	badRow := persistence.Result{
		Mode: persistence.ModeBatch, BatchID: batchID, ItemIndex: 1,
		Status: persistence.StatusError,
		ErrorField: "zenith_angle", ErrorMessage: "must be in [0, 90)",
		Raw: json.RawMessage(`{"zenith_angle":90}`),
	}
	rows := []persistence.Result{okRow, badRow}
	if err := store.Save(ctx, rows); err != nil {
		t.Fatalf("save: %v", err)
	}
	okRow, badRow = rows[0], rows[1]
	if okRow.ID == 0 || badRow.ID == 0 || okRow.CreatedAt.IsZero() {
		t.Fatalf("ids/timestamps not populated: %d %d %v", okRow.ID, badRow.ID, okRow.CreatedAt)
	}

	// 批量按 batch_id 检索并点名失败组
	recs, err := store.Query(ctx, persistence.Filter{BatchID: batchID})
	if err != nil || len(recs) != 1 {
		t.Fatalf("batch query: %v %+v", err, recs)
	}
	if recs[0].ItemIndex != 1 || recs[0].ErrorField != "zenith_angle" {
		t.Fatalf("failed item wrong: %+v", recs[0])
	}
	// JSONB 会规范化空白，按语义比较原始输入
	var gotRaw map[string]any
	if err := json.Unmarshal(recs[0].Raw, &gotRaw); err != nil {
		t.Fatalf("raw is not json: %v", err)
	}
	if gotRaw["zenith_angle"].(float64) != 90 {
		t.Fatalf("raw input not preserved: %s", recs[0].Raw)
	}

	// 按 TEC 区间检索，且剖面 JSONB 原样可取
	min := 10.0
	recs, err = store.Query(ctx, persistence.Filter{MinTECU: &min, Status: persistence.StatusOK})
	if err != nil || len(recs) < 1 {
		t.Fatalf("tec filter: %v %d", err, len(recs))
	}
	if len(recs[0].ProfileJSON) == 0 {
		t.Fatal("profile json not retrieved")
	}

	if err := store.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
