package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"ionosphere/internal/config"
	"ionosphere/internal/persistence"
)

func testServer() (*Server, persistence.Store) {
	cfg := config.Default()
	store := persistence.NewMemoryStore()
	return NewServer(cfg, store), store
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response %q: %v", rec.Body.String(), err)
		}
	}
	return rec.Code, out
}

func validBody(chi float64) map[string]any {
	return map[string]any{
		"peak_density": 1.2e12,
		"peak_height":  300e3,
		"scale_height": 60e3,
		"zenith_angle": chi,
	}
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected object, got %T: %v", v, v)
	}
	return m
}

// 正午 F2 示范算例：峰落在给定峰值高度，TEC 为正，chi=0 时峰点密度等于输入峰值密度。
func TestDemoExample(t *testing.T) {
	srv, _ := testServer()
	code, body := doJSON(t, srv.Handler(), "GET", "/api/v1/demo", nil)
	if code != http.StatusOK {
		t.Fatalf("demo status %d: %v", code, body)
	}
	item := asMap(t, body["item"])
	prof := asMap(t, item["profile"])
	if prof["peak_height"].(float64) != 300e3 {
		t.Fatalf("demo peak height = %v", prof["peak_height"])
	}
	peak := prof["peak_density"].(float64)
	if math.Abs(peak-1.2e12)/1.2e12 > 1e-9 {
		t.Fatalf("demo peak density %.6e != Nm 1.2e12", peak)
	}
	tecU := item["tec_u"].(float64)
	if !(tecU > 0) {
		t.Fatalf("demo TEC %v not positive", tecU)
	}
	samples := prof["samples"].([]any)
	arg := 0
	max := -1.0
	for i, sm := range samples {
		d := asMap(t, sm)["density"].(float64)
		if d > max {
			max, arg = d, i
		}
	}
	maxAlt := asMap(t, samples[arg])["altitude"].(float64)
	if math.Abs(maxAlt-300e3) > 1e-6 {
		t.Fatalf("demo profile argmax altitude %.1f != 300km", maxAlt)
	}
}

// 完整剖面接口返回剖面与 TEC；轻量接口只返回 TEC，不带剖面。
func TestProfileAndTECEndpoints(t *testing.T) {
	srv, _ := testServer()

	code, body := doJSON(t, srv.Handler(), "POST", "/api/v1/profile", validBody(0))
	if code != http.StatusOK {
		t.Fatalf("profile status %d: %v", code, body)
	}
	item := asMap(t, body["item"])
	if item["profile"] == nil {
		t.Fatal("profile endpoint must return a profile")
	}
	tecFull := asMap(t, item)["tec_u"].(float64)

	code, body = doJSON(t, srv.Handler(), "POST", "/api/v1/tec", validBody(0))
	if code != http.StatusOK {
		t.Fatalf("tec status %d: %v", code, body)
	}
	item = asMap(t, body["item"])
	if _, has := item["profile"]; has {
		t.Fatal("TEC-only endpoint must not include a profile")
	}
	if math.Abs(item["tec_u"].(float64)-tecFull)/tecFull > 1e-9 {
		t.Fatal("TEC endpoints disagree")
	}
}

// 天顶角 90 度、非法参数通过 HTTP 被明确拒绝，且不得静默返回白天剖面。
func TestIllegalZenithRejectedOverHTTP(t *testing.T) {
	srv, store := testServer()
	code, body := doJSON(t, srv.Handler(), "POST", "/api/v1/tec", validBody(90))
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d: %v", code, body)
	}
	errObj := asMap(t, body["error"])
	if errObj["field"] != "zenith_angle" {
		t.Fatalf("want zenith_angle error, got %v", errObj)
	}
	recs, _ := store.Query(context.Background(), persistence.Filter{Status: persistence.StatusError})
	if len(recs) != 1 || recs[0].TECU != nil {
		t.Fatalf("rejected computation must be persisted without TEC, got %+v", recs)
	}
}

// 批量：某组非法时点明第几组、哪个参数，其余组照常返回。
func TestBatchPartialFailure(t *testing.T) {
	srv, store := testServer()
	badNum := json.RawMessage(`{"peak_density":"oops","peak_height":300000,"scale_height":60000,"zenith_angle":0}`)
	good := validBody(0)
	good["zenith_angle"] = 60.0
	badZenith := validBody(90)
	missing := map[string]any{"peak_density": 1e12, "peak_height": 300e3}

	layers := []any{good, badNum, badZenith, missing}
	code, body := doJSON(t, srv.Handler(), "POST", "/api/v1/batch", map[string]any{
		"layers": layers, "include_profile": false,
	})
	if code != http.StatusOK {
		t.Fatalf("batch status %d: %v", code, body)
	}
	if body["count"].(float64) != 4 {
		t.Fatalf("count = %v", body["count"])
	}
	items := body["items"].([]any)

	// 第 0 组成功
	if asMap(t, items[0])["ok"] != true {
		t.Fatalf("item 0 should succeed: %v", items[0])
	}
	// 第 1 组：非数值
	e1 := asMap(t, asMap(t, items[1])["error"])
	if asMap(t, items[1])["index"].(float64) != 1 {
		t.Fatalf("error index not 1: %v", items[1])
	}
	if e1["field"] != "layer" {
		t.Fatalf("item 1 field = %v", e1)
	}
	// 第 2 组：天顶角越界
	e2 := asMap(t, asMap(t, items[2])["error"])
	if asMap(t, items[2])["index"].(float64) != 2 || e2["field"] != "zenith_angle" {
		t.Fatalf("item 2 wrong: %v", items[2])
	}
	// 第 3 组：缺字段，且必须点名缺哪个
	e3 := asMap(t, asMap(t, items[3])["error"])
	if asMap(t, items[3])["index"].(float64) != 3 || e3["field"] != "scale_height" {
		t.Fatalf("item 3 wrong: %v", items[3])
	}

	recs, _ := store.Query(context.Background(), persistence.Filter{Mode: persistence.ModeBatch})
	if len(recs) != 4 {
		t.Fatalf("want 4 persisted batch rows, got %d", len(recs))
	}
	okN, errN := 0, 0
	for _, r := range recs {
		if r.Status == persistence.StatusOK {
			okN++
		} else {
			errN++
			if r.ErrorField == "" || r.ErrorMessage == "" {
				t.Fatalf("failed row missing field/message: %+v", r)
			}
		}
	}
	if okN != 1 || errN != 3 {
		t.Fatalf("want 1 ok / 3 failed, got %d/%d", okN, errN)
	}
}

// 历史持久化：成功结果可按状态、模式与 TEC 区间检索，输入被完整保留。
func TestHistoryPersistence(t *testing.T) {
	srv, store := testServer()
	h := srv.Handler()

	doJSON(t, h, "POST", "/api/v1/tec", validBody(0))
	doJSON(t, h, "POST", "/api/v1/tec", validBody(90))
	doJSON(t, h, "POST", "/api/v1/batch", map[string]any{"layers": []any{validBody(10)}})

	all, _ := store.Query(context.Background(), persistence.Filter{})
	// 2 次单算（1 成功 1 拒绝）+ 批量 1 组 = 3 条记录
	if len(all) != 3 {
		t.Fatalf("want 3 records, got %d", len(all))
	}
	ok, _ := store.Query(context.Background(), persistence.Filter{Status: persistence.StatusOK})
	if len(ok) != 2 {
		t.Fatalf("want 2 ok records, got %d", len(ok))
	}
	batch, _ := store.Query(context.Background(), persistence.Filter{Mode: persistence.ModeBatch})
	if len(batch) != 1 || batch[0].TECU == nil || *batch[0].TECU <= 0 {
		t.Fatalf("batch row wrong: %+v", batch)
	}
	// 保留原始输入
	if !bytes.Contains(batch[0].Raw, []byte("scale_height")) {
		t.Fatalf("raw input not persisted: %s", batch[0].Raw)
	}
	// TEC 区间过滤
	low := 0.0
	high := 1e-9
	none, _ := store.Query(context.Background(), persistence.Filter{MinTECU: &low, MaxTECU: &high})
	if len(none) != 0 {
		t.Fatalf("range filter should return 0, got %d", len(none))
	}
	big := 1e-9
	some, _ := store.Query(context.Background(), persistence.Filter{MinTECU: &big})
	if len(some) != 2 {
		t.Fatalf("min-tec filter want 2, got %d", len(some))
	}

	// HTTP 检索
	code, body := doJSON(t, h, "GET", "/api/v1/history?status=ok", nil)
	if code != http.StatusOK || body["count"].(float64) != 2 {
		t.Fatalf("history endpoint: %d %v", code, body)
	}
}

// 配置回显：地面基准、积分顶高、TECU 换算与容差均可读到。
func TestConfigEcho(t *testing.T) {
	srv, _ := testServer()
	code, body := doJSON(t, srv.Handler(), "GET", "/api/v1/config", nil)
	if code != http.StatusOK {
		t.Fatalf("config %d", code)
	}
	c := asMap(t, body["config"])
	for _, k := range []string{"ground_altitude_m", "top_altitude_m", "tec_u_conversion", "integration_tolerance"} {
		if _, ok := c[k]; !ok {
			t.Fatalf("config missing %s", k)
		}
	}
	if c["ground_altitude_m"].(float64) != 0 {
		t.Fatal("ground datum must be 0")
	}
	if c["tec_u_conversion"].(float64) != 1e16 {
		t.Fatal("TECU must be 1e16")
	}
}

// 运行状态端点：存活与就绪。
func TestHealthEndpoints(t *testing.T) {
	srv, _ := testServer()
	for _, p := range []string{"/livez", "/readyz"} {
		code, body := doJSON(t, srv.Handler(), "GET", p, nil)
		if code != http.StatusOK {
			t.Fatalf("%s -> %d %v", p, code, body)
		}
	}
}

// 并发多路请求：结果互不干扰、历史不错乱。
func TestConcurrentRequestsIsolation(t *testing.T) {
	srv, store := testServer()
	h := srv.Handler()

	const goroutines = 40
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			nm := 1e12 + float64(g)*1e10
			chi := float64(g % 80) // 0..79 均合法
			body := map[string]any{
				"peak_density": nm,
				"peak_height":  300e3 + float64(g)*1e3,
				"scale_height": 50e3 + float64(g)*100,
				"zenith_angle": chi,
			}
			code, resp := doJSON(t, h, "POST", "/api/v1/profile", body)
			if code != http.StatusOK {
				errs <- fmt.Errorf("g=%d status %d: %v", g, code, resp)
				return
			}
			item := asMap(t, resp["item"])
			// 返回的输入峰值必须就是本 goroutine 提交的值
			if got := item["peak_density"].(float64); math.Abs(got-nm)/nm > 1e-12 {
				errs <- fmt.Errorf("g=%d cross-talk peak %.6e vs %.6e", g, got, nm)
				return
			}
			wantPeakH := 300e3 + float64(g)*1e3
			if got := item["peak_height"].(float64); got != wantPeakH {
				errs <- fmt.Errorf("g=%d peak height cross-talk %.1f vs %.1f", g, got, wantPeakH)
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}

	recs, _ := store.Query(context.Background(), persistence.Filter{Limit: 1000})
	if len(recs) != goroutines {
		t.Fatalf("history count = %d, want %d (no loss/duplication)", len(recs), goroutines)
	}
	seen := map[float64]bool{}
	for _, r := range recs {
		if r.Status != persistence.StatusOK || r.PeakDensity == nil || r.TECU == nil {
			t.Fatalf("unexpected bad record: %+v", r)
		}
		if seen[*r.PeakDensity] {
			t.Fatalf("duplicate peak_density %.4e: history mixed up", *r.PeakDensity)
		}
		seen[*r.PeakDensity] = true
	}
}
