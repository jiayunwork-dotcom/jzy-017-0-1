package main

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

func newTestServer() (*Server, *MemoryStore) {
	st := NewMemoryStore()
	return NewServer(testConfig(), st), st
}

func doRequest(t *testing.T, srv *Server, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("invalid JSON response: %v\n%s", err, rec.Body.String())
	}
}

const validBody = `{"peakDensity":1.2e12,"peakHeight":300000,"scaleHeight":60000,"solarZenithAngle":0}`

// The profile endpoint returns the full profile plus TEC, pins the peak to
// the requested height, and persists the calculation.
func TestProfileEndpoint(t *testing.T) {
	srv, st := newTestServer()
	rec := doRequest(t, srv, "POST", "/v1/profile", []byte(validBody))
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID      int64          `json:"id"`
		Peak    PeakInfo       `json:"peak"`
		TECU    float64        `json:"tecu"`
		Profile []ProfilePoint `json:"profile"`
	}
	decodeBody(t, rec, &resp)
	if resp.ID != 1 {
		t.Fatalf("id = %d, want 1", resp.ID)
	}
	if resp.Peak.Density != 1.2e12 {
		t.Fatalf("peak density = %v, want exactly 1.2e12 at zenith 0", resp.Peak.Density)
	}
	if resp.Peak.Height != 300000 {
		t.Fatalf("peak height = %v, want 300000", resp.Peak.Height)
	}
	if resp.TECU <= 0 {
		t.Fatalf("tecu = %v, want positive", resp.TECU)
	}
	if len(resp.Profile) != 400 {
		t.Fatalf("profile points = %d, want 400", len(resp.Profile))
	}
	// The sampled profile must reach its maximum at the peak height.
	best := 0
	for i := range resp.Profile {
		if resp.Profile[i].Density > resp.Profile[best].Density {
			best = i
		}
	}
	step := 1e6 / 399
	if math.Abs(resp.Profile[best].Height-300000) > 2*step {
		t.Fatalf("profile maximum at %v m, want near 300000 m", resp.Profile[best].Height)
	}
	// The calculation must be persisted with its inputs and results.
	if len(st.records) != 1 {
		t.Fatalf("stored %d records, want 1", len(st.records))
	}
	stored := st.records[0]
	if stored.Kind != "profile" || stored.Input.PeakDensity != 1.2e12 || stored.TECU != resp.TECU {
		t.Fatalf("stored record wrong: %+v", stored)
	}
}

// The TEC endpoint integrates but returns no profile.
func TestTECEndpointIsLightweight(t *testing.T) {
	srv, _ := newTestServer()
	rec := doRequest(t, srv, "POST", "/v1/tec", []byte(validBody))
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	decodeBody(t, rec, &raw)
	if _, ok := raw["profile"]; ok {
		t.Fatalf("TEC endpoint must not return a profile")
	}
	var resp struct {
		TECU float64 `json:"tecu"`
	}
	decodeBody(t, rec, &resp)
	if resp.TECU <= 0 {
		t.Fatalf("tecu = %v, want positive", resp.TECU)
	}
}

// Doubling only the peak density doubles the whole profile and the TEC.
func TestDoublingPeakDensityDoublesProfileAndTEC(t *testing.T) {
	srv, _ := newTestServer()
	post := func(density float64) struct {
		TECU    float64        `json:"tecu"`
		Profile []ProfilePoint `json:"profile"`
	} {
		body := `{"peakDensity":` + strconv.FormatFloat(density, 'g', -1, 64) +
			`,"peakHeight":300000,"scaleHeight":60000,"solarZenithAngle":10,"points":200}`
		rec := doRequest(t, srv, "POST", "/v1/profile", []byte(body))
		if rec.Code != 200 {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			TECU    float64        `json:"tecu"`
			Profile []ProfilePoint `json:"profile"`
		}
		decodeBody(t, rec, &resp)
		return resp
	}
	r1 := post(1.2e12)
	r2 := post(2.4e12)
	if math.Abs(r2.TECU/r1.TECU-2) > 1e-9 {
		t.Fatalf("TECU ratio = %v, want 2", r2.TECU/r1.TECU)
	}
	for i := range r1.Profile {
		if math.Abs(r2.Profile[i].Density/r1.Profile[i].Density-2) > 1e-9 {
			t.Fatalf("profile density ratio at point %d = %v, want 2",
				i, r2.Profile[i].Density/r1.Profile[i].Density)
		}
	}
}

// Raising the peak height moves the profile maximum up with it (the
// integration top is high enough not to clip it).
func TestRaisedPeakHeightFollowed(t *testing.T) {
	srv, _ := newTestServer()
	body := `{"peakDensity":1.2e12,"peakHeight":420000,"scaleHeight":60000,"solarZenithAngle":0}`
	rec := doRequest(t, srv, "POST", "/v1/profile", []byte(body))
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Peak    PeakInfo       `json:"peak"`
		Profile []ProfilePoint `json:"profile"`
	}
	decodeBody(t, rec, &resp)
	if resp.Peak.Height != 420000 {
		t.Fatalf("peak height = %v, want 420000", resp.Peak.Height)
	}
	best := 0
	for i := range resp.Profile {
		if resp.Profile[i].Density > resp.Profile[best].Density {
			best = i
		}
	}
	step := 1e6 / 399
	if math.Abs(resp.Profile[best].Height-420000) > 2*step {
		t.Fatalf("profile maximum at %v m, want near 420000 m", resp.Profile[best].Height)
	}
}

// Zenith angle 0 -> 60 degrees lowers the peak density; 90 degrees is
// rejected outright.
func TestZenithAngleBehaviour(t *testing.T) {
	srv, _ := newTestServer()
	peakAt := func(chi string) float64 {
		body := `{"peakDensity":1.2e12,"peakHeight":300000,"scaleHeight":60000,"solarZenithAngle":` + chi + `}`
		rec := doRequest(t, srv, "POST", "/v1/tec", []byte(body))
		if rec.Code != 200 {
			t.Fatalf("chi=%s: status = %d, body %s", chi, rec.Code, rec.Body.String())
		}
		var resp struct {
			Peak PeakInfo `json:"peak"`
		}
		decodeBody(t, rec, &resp)
		return resp.Peak.Density
	}
	n0 := peakAt("0")
	n60 := peakAt("60")
	if !(n60 < n0) {
		t.Fatalf("peak density at 60° (%v) not below 0° (%v)", n60, n0)
	}

	rec := doRequest(t, srv, "POST", "/v1/tec",
		[]byte(`{"peakDensity":1.2e12,"peakHeight":300000,"scaleHeight":60000,"solarZenithAngle":90}`))
	if rec.Code != 400 {
		t.Fatalf("zenith 90: status = %d, want 400", rec.Code)
	}
	var errResp struct {
		Error ValidationError `json:"error"`
	}
	decodeBody(t, rec, &errResp)
	if errResp.Error.Parameter != "solarZenithAngle" {
		t.Fatalf("error parameter = %q, want solarZenithAngle", errResp.Error.Parameter)
	}
}

// Invalid inputs are rejected with 400 and a named parameter.
func TestInvalidParamsRejected(t *testing.T) {
	srv, _ := newTestServer()
	cases := []struct {
		body string
		want string
	}{
		{`{"peakDensity":-1,"peakHeight":3e5,"scaleHeight":6e4,"solarZenithAngle":0}`, "peakDensity"},
		{`{"peakDensity":1e12,"peakHeight":3e5,"scaleHeight":0,"solarZenithAngle":0}`, "scaleHeight"},
		{`{"peakDensity":1e12,"peakHeight":0,"scaleHeight":6e4,"solarZenithAngle":0}`, "peakHeight"},
		{`{"peakDensity":1e12,"peakHeight":3e5,"scaleHeight":6e4,"solarZenithAngle":95}`, "solarZenithAngle"},
		{`{"peakDensity":1e12,"peakHeight":3e5,"solarZenithAngle":0}`, "scaleHeight"},
		{`{"peakDensity":"big","peakHeight":3e5,"scaleHeight":6e4,"solarZenithAngle":0}`, "peakDensity"},
	}
	for _, tc := range cases {
		rec := doRequest(t, srv, "POST", "/v1/profile", []byte(tc.body))
		if rec.Code != 400 {
			t.Fatalf("body %s: status = %d, want 400", tc.body, rec.Code)
		}
		var errResp struct {
			Error ValidationError `json:"error"`
		}
		decodeBody(t, rec, &errResp)
		if errResp.Error.Parameter != tc.want {
			t.Fatalf("body %s: parameter = %q, want %q", tc.body, errResp.Error.Parameter, tc.want)
		}
	}
}

// A batch with some invalid items reports those items (index + parameter)
// while the valid ones are computed and persisted.
func TestBatchPartialFailure(t *testing.T) {
	srv, st := newTestServer()
	body := `{"requests":[
		{"peakDensity":1.2e12,"peakHeight":300000,"scaleHeight":60000,"solarZenithAngle":0},
		{"peakDensity":1.2e12,"peakHeight":300000,"scaleHeight":-1,"solarZenithAngle":0},
		{"peakDensity":2e12,"peakHeight":250000,"scaleHeight":50000,"solarZenithAngle":30},
		{"peakDensity":1e12,"peakHeight":300000,"solarZenithAngle":0}
	]}`
	rec := doRequest(t, srv, "POST", "/v1/batch", []byte(body))
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		BatchID string `json:"batchId"`
		Results []struct {
			Index  int              `json:"index"`
			OK     bool             `json:"ok"`
			Result *computeResponse `json:"result"`
			Error  *ValidationError `json:"error"`
		} `json:"results"`
	}
	decodeBody(t, rec, &resp)
	if resp.BatchID == "" {
		t.Fatalf("batch id missing")
	}
	if len(resp.Results) != 4 {
		t.Fatalf("results = %d, want 4", len(resp.Results))
	}
	if !resp.Results[0].OK || resp.Results[0].Result.TECU <= 0 {
		t.Fatalf("item 0 should have succeeded: %+v", resp.Results[0])
	}
	if resp.Results[1].OK || resp.Results[1].Error == nil ||
		resp.Results[1].Error.Parameter != "scaleHeight" || resp.Results[1].Index != 1 {
		t.Fatalf("item 1 should name scaleHeight: %+v", resp.Results[1])
	}
	if !resp.Results[2].OK || resp.Results[2].Result.TECU <= 0 {
		t.Fatalf("item 2 should have succeeded: %+v", resp.Results[2])
	}
	if resp.Results[3].OK || resp.Results[3].Error == nil ||
		resp.Results[3].Error.Parameter != "scaleHeight" || resp.Results[3].Index != 3 {
		t.Fatalf("item 3 should name the missing scaleHeight: %+v", resp.Results[3])
	}
	// Only the two valid items are persisted, grouped under the batch id.
	if len(st.records) != 2 {
		t.Fatalf("stored %d records, want 2", len(st.records))
	}
	for _, r := range st.records {
		if r.Kind != "batch" || r.BatchID != resp.BatchID {
			t.Fatalf("stored record not linked to the batch: %+v", r)
		}
	}
}

// Persisted calculations are retrievable through the history endpoints,
// with filters.
func TestHistoryPersistence(t *testing.T) {
	srv, _ := newTestServer()
	doRequest(t, srv, "POST", "/v1/tec", []byte(validBody))
	doRequest(t, srv, "POST", "/v1/tec",
		[]byte(`{"peakDensity":2.4e12,"peakHeight":300000,"scaleHeight":60000,"solarZenithAngle":0}`))
	doRequest(t, srv, "POST", "/v1/profile", []byte(validBody))

	rec := doRequest(t, srv, "GET", "/v1/history", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var list struct {
		Total   int       `json:"total"`
		Records []*Record `json:"records"`
	}
	decodeBody(t, rec, &list)
	if list.Total != 3 || len(list.Records) != 3 {
		t.Fatalf("history total = %d (%d records), want 3", list.Total, len(list.Records))
	}

	// Filter by kind.
	rec = doRequest(t, srv, "GET", "/v1/history?kind=tec", nil)
	decodeBody(t, rec, &list)
	if list.Total != 2 {
		t.Fatalf("kind=tec total = %d, want 2", list.Total)
	}

	// Filter by TECU range: the doubled-density run has twice the TECU.
	rec = doRequest(t, srv, "GET", "/v1/history?kind=tec&minTecu=40", nil)
	decodeBody(t, rec, &list)
	if list.Total != 1 || list.Records[0].Input.PeakDensity != 2.4e12 {
		t.Fatalf("minTecu filter returned %+v, want only the 2.4e12 run", list.Records)
	}

	// Fetch one record in full, including the stored result document.
	rec = doRequest(t, srv, "GET", "/v1/history/1", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var full Record
	decodeBody(t, rec, &full)
	if full.Input.PeakDensity != 1.2e12 || full.TECU <= 0 || len(full.Result) == 0 {
		t.Fatalf("record 1 wrong: %+v", full)
	}

	// Unknown id -> 404.
	rec = doRequest(t, srv, "GET", "/v1/history/9999", nil)
	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// The config endpoint echoes ground reference, top height, TECU conversion
// and tolerance.
func TestConfigEcho(t *testing.T) {
	srv, _ := newTestServer()
	rec := doRequest(t, srv, "GET", "/v1/config", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var cfg configResponse
	decodeBody(t, rec, &cfg)
	if cfg.GroundAltitude != 0 || cfg.TopHeight != 1e6 || cfg.TECU != 1e16 || cfg.TECTolerance != 1e-9 {
		t.Fatalf("config echo wrong: %+v", cfg)
	}
}

// The status endpoint reports liveness for monitoring.
func TestStatusEndpoint(t *testing.T) {
	srv, _ := newTestServer()
	rec := doRequest(t, srv, "GET", "/v1/status", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var st statusResponse
	decodeBody(t, rec, &st)
	if st.Status != "ok" || st.DB != "up" {
		t.Fatalf("status wrong: %+v", st)
	}
}

// The built-in demo case: noon F2 layer, peak at the given height, peak
// density equal to the input at zero zenith angle, positive TEC.
func TestDemoEndpoint(t *testing.T) {
	srv, _ := newTestServer()
	rec := doRequest(t, srv, "GET", "/v1/demo", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp computeResponse
	decodeBody(t, rec, &resp)
	if resp.Peak.Height != 300000 {
		t.Fatalf("demo peak height = %v, want 300000", resp.Peak.Height)
	}
	if resp.Peak.Density != resp.Input.PeakDensity {
		t.Fatalf("demo peak density %v != input %v at zenith 0", resp.Peak.Density, resp.Input.PeakDensity)
	}
	if resp.TECU <= 0 {
		t.Fatalf("demo TECU = %v, want positive", resp.TECU)
	}
}

// Concurrent requests must not interfere: every response must correspond to
// its own input, and the persisted history must be complete and untangled.
func TestConcurrentRequests(t *testing.T) {
	srv, st := newTestServer()
	const n = 32
	tecu := make([]float64, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d := 1.2e12 * (1 + float64(i)*0.01)
			body := `{"peakDensity":` + strconv.FormatFloat(d, 'g', -1, 64) +
				`,"peakHeight":300000,"scaleHeight":60000,"solarZenithAngle":10}`
			req := httptest.NewRequest("POST", "/v1/tec", bytes.NewReader([]byte(body)))
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Errorf("request %d: status = %d, body %s", i, rec.Code, rec.Body.String())
				return
			}
			var resp struct {
				Input LayerParams `json:"input"`
				TECU  float64     `json:"tecu"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Errorf("request %d: bad JSON: %v", i, err)
				return
			}
			if resp.Input.PeakDensity != d {
				t.Errorf("cross-talk: asked density %v, got %v", d, resp.Input.PeakDensity)
				return
			}
			tecu[i] = resp.TECU
		}(i)
	}
	wg.Wait()

	// TEC is linear in peak density: each response must match its own input.
	for i := 1; i < n; i++ {
		want := tecu[0] * (1 + float64(i)*0.01)
		if math.Abs(tecu[i]/want-1) > 1e-9 {
			t.Fatalf("request %d: tecu = %v, want %v", i, tecu[i], want)
		}
	}

	// All n calculations persisted exactly once, with distinct IDs, and each
	// stored record's TECU matches its own stored input.
	if len(st.records) != n {
		t.Fatalf("stored %d records, want %d", len(st.records), n)
	}
	seen := make(map[int64]bool)
	for _, r := range st.records {
		if seen[r.ID] {
			t.Fatalf("duplicate record id %d", r.ID)
		}
		seen[r.ID] = true
		want := tecu[0] * r.Input.PeakDensity / 1.2e12
		if math.Abs(r.TECU/want-1) > 1e-9 {
			t.Fatalf("record %d: stored tecu %v does not match stored input %v", r.ID, r.TECU, r.Input.PeakDensity)
		}
	}
}
