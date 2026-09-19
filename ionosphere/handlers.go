package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// version is reported by the config and status endpoints.
const version = "1.0.0"

// Server wires the HTTP endpoints to the physics and the store. It holds no
// per-request state, so concurrent requests never interfere with each other.
type Server struct {
	cfg          Config
	store        Store
	started      time.Time
	requests     atomic.Int64
	calculations atomic.Int64
}

// NewServer builds a Server.
func NewServer(cfg Config, store Store) *Server {
	return &Server{cfg: cfg, store: store, started: time.Now()}
}

// Handler returns the root HTTP handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/profile", s.handleProfile)
	mux.HandleFunc("POST /v1/tec", s.handleTEC)
	mux.HandleFunc("POST /v1/batch", s.handleBatch)
	mux.HandleFunc("GET /v1/demo", s.handleDemo)
	mux.HandleFunc("GET /v1/config", s.handleConfig)
	mux.HandleFunc("GET /v1/status", s.handleStatus)
	mux.HandleFunc("GET /v1/history", s.handleHistory)
	mux.HandleFunc("GET /v1/history/{id}", s.handleHistoryByID)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		mux.ServeHTTP(w, r)
	})
}

// computeResponse is a Computation plus its persistence ID.
type computeResponse struct {
	ID int64 `json:"id"`
	*Computation
}

// apiError couples an HTTP status with a ValidationError body.
type apiError struct {
	status int
	err    *ValidationError
}

// execute runs one validated computation and persists input and result.
func (s *Server) execute(ctx context.Context, params LayerParams, points int, kind, batchID string) (*computeResponse, *apiError) {
	comp, err := Compute(s.cfg, params, points)
	if err != nil {
		log.Printf("computation failed: %v", err)
		return nil, &apiError{http.StatusInternalServerError, &ValidationError{Message: err.Error()}}
	}
	rec := &Record{
		Kind:        kind,
		BatchID:     batchID,
		Input:       params,
		PeakHeight:  comp.Peak.Height,
		PeakDensity: comp.Peak.Density,
		TEC:         comp.TEC,
		TECU:        comp.TECU,
		Result:      mustJSON(comp),
	}
	if err := s.store.Save(ctx, rec); err != nil {
		log.Printf("persisting calculation failed: %v", err)
		return nil, &apiError{http.StatusInternalServerError, &ValidationError{Message: "failed to persist result"}}
	}
	s.calculations.Add(1)
	return &computeResponse{ID: rec.ID, Computation: comp}, nil
}

// handleProfile computes the full density profile and the TEC.
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	params, verr := ParseLayerParams(body, s.cfg)
	if verr != nil {
		writeValidationError(w, verr)
		return
	}
	points, verr := parsePoints(body, s.cfg.ProfilePoints)
	if verr != nil {
		writeValidationError(w, verr)
		return
	}
	resp, aerr := s.execute(r.Context(), params, points, "profile", "")
	if aerr != nil {
		writeAPIError(w, aerr)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleTEC is the lightweight endpoint: integrate only, return only the
// TEC (no profile is sampled or returned).
func (s *Server) handleTEC(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	params, verr := ParseLayerParams(body, s.cfg)
	if verr != nil {
		writeValidationError(w, verr)
		return
	}
	resp, aerr := s.execute(r.Context(), params, 0, "tec", "")
	if aerr != nil {
		writeAPIError(w, aerr)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// batchItem is one entry of a batch response: either a result or an error
// naming the offending parameter.
type batchItem struct {
	Index  int              `json:"index"`
	OK     bool             `json:"ok"`
	Result *computeResponse `json:"result,omitempty"`
	Error  *ValidationError `json:"error,omitempty"`
}

type batchResponse struct {
	BatchID string      `json:"batchId"`
	Results []batchItem `json:"results"`
}

// handleBatch evaluates many parameter sets in one request. Invalid items
// are reported individually (index + parameter); the rest are computed and
// persisted normally.
func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	var req struct {
		Requests       []json.RawMessage `json:"requests"`
		IncludeProfile bool              `json:"includeProfile"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "", "request body must be a JSON object with a 'requests' array")
		return
	}
	if len(req.Requests) == 0 {
		writeError(w, http.StatusBadRequest, "requests", "must contain at least one item")
		return
	}
	if len(req.Requests) > s.cfg.MaxBatchSize {
		writeError(w, http.StatusBadRequest, "requests",
			fmt.Sprintf("must contain at most %d items", s.cfg.MaxBatchSize))
		return
	}
	points := 0
	if req.IncludeProfile {
		points = s.cfg.ProfilePoints
	}
	batchID := newBatchID()
	items := make([]batchItem, len(req.Requests))
	for i, raw := range req.Requests {
		items[i].Index = i
		params, verr := ParseLayerParams(raw, s.cfg)
		if verr != nil {
			items[i].Error = verr
			continue
		}
		resp, aerr := s.execute(r.Context(), params, points, "batch", batchID)
		if aerr != nil {
			items[i].Error = aerr.err
			continue
		}
		items[i].OK = true
		items[i].Result = resp
	}
	writeJSON(w, http.StatusOK, batchResponse{BatchID: batchID, Results: items})
}

// handleDemo runs the built-in noon F2-layer example.
func (s *Server) handleDemo(w http.ResponseWriter, r *http.Request) {
	resp, aerr := s.execute(r.Context(), DemoParams(), s.cfg.ProfilePoints, "demo", "")
	if aerr != nil {
		writeAPIError(w, aerr)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// configResponse echoes the pinned physical and numerical configuration.
type configResponse struct {
	GroundAltitude float64 `json:"groundAltitude"` // m
	TopHeight      float64 `json:"topHeight"`      // m
	TECU           float64 `json:"tecu"`           // electrons per m^2 in one TECU
	TECTolerance   float64 `json:"tecTolerance"`   // relative refinement tolerance
	ProfilePoints  int     `json:"profilePoints"`
	MaxBatchSize   int     `json:"maxBatchSize"`
	Version        string  `json:"version"`
}

// handleConfig echoes the ground reference, integration top height, TECU
// conversion and integration tolerance.
func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, configResponse{
		GroundAltitude: s.cfg.GroundAltitude,
		TopHeight:      s.cfg.TopHeight,
		TECU:           TECU,
		TECTolerance:   s.cfg.TECTolerance,
		ProfilePoints:  s.cfg.ProfilePoints,
		MaxBatchSize:   s.cfg.MaxBatchSize,
		Version:        version,
	})
}

type statusResponse struct {
	Status        string  `json:"status"` // ok | degraded
	DB            string  `json:"db"`     // up | down
	UptimeSeconds float64 `json:"uptimeSeconds"`
	Requests      int64   `json:"requests"`
	Calculations  int64   `json:"calculations"`
	Time          string  `json:"time"`
	Version       string  `json:"version"`
}

// handleStatus is the monitoring endpoint: process liveness plus store
// reachability.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	db := "up"
	status := "ok"
	code := http.StatusOK
	if err := s.store.Ping(ctx); err != nil {
		db = "down"
		status = "degraded"
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, statusResponse{
		Status:        status,
		DB:            db,
		UptimeSeconds: time.Since(s.started).Seconds(),
		Requests:      s.requests.Load(),
		Calculations:  s.calculations.Load(),
		Time:          time.Now().UTC().Format(time.RFC3339),
		Version:       version,
	})
}

type historyResponse struct {
	Total   int       `json:"total"`
	Records []*Record `json:"records"`
}

// handleHistory searches persisted calculations by kind, time window and
// TECU range, with pagination.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := HistoryFilter{Limit: 50}
	f.Kind = q.Get("kind")
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "since", "must be an RFC3339 timestamp")
			return
		}
		f.Since = &t
	}
	if v := q.Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "until", "must be an RFC3339 timestamp")
			return
		}
		f.Until = &t
	}
	if v := q.Get("minTecu"); v != "" {
		x, err := strconv.ParseFloat(v, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "minTecu", "must be a number")
			return
		}
		f.MinTECU = &x
	}
	if v := q.Get("maxTecu"); v != "" {
		x, err := strconv.ParseFloat(v, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "maxTecu", "must be a number")
			return
		}
		f.MaxTECU = &x
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			writeError(w, http.StatusBadRequest, "limit", "must be an integer between 1 and 1000")
			return
		}
		f.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "offset", "must be a non-negative integer")
			return
		}
		f.Offset = n
	}
	records, total, err := s.store.List(r.Context(), f)
	if err != nil {
		log.Printf("history query failed: %v", err)
		writeError(w, http.StatusInternalServerError, "", "failed to query history")
		return
	}
	if records == nil {
		records = []*Record{}
	}
	writeJSON(w, http.StatusOK, historyResponse{Total: total, Records: records})
}

// handleHistoryByID returns one full persisted record.
func (s *Server) handleHistoryByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id", "must be an integer")
		return
	}
	rec, err := s.store.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "", "record not found")
		return
	}
	if err != nil {
		log.Printf("loading record %d failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "", "failed to load record")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// parsePoints reads the optional "points" field of a profile request.
func parsePoints(body []byte, def int) (int, *ValidationError) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return def, nil // malformed JSON is reported by ParseLayerParams
	}
	r, ok := raw["points"]
	if !ok || string(r) == "null" {
		return def, nil
	}
	var num json.Number
	if err := json.Unmarshal(r, &num); err != nil {
		return 0, &ValidationError{Parameter: "points", Message: "must be an integer"}
	}
	n, err := num.Int64()
	if err != nil {
		return 0, &ValidationError{Parameter: "points", Message: "must be an integer"}
	}
	if n < 2 || n > 10000 {
		return 0, &ValidationError{Parameter: "points", Message: "must be between 2 and 10000"}
	}
	return int(n), nil
}

// newBatchID returns a random hex identifier grouping one batch request.
func newBatchID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "", "failed to read request body")
		return nil, false
	}
	return data, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, parameter, message string) {
	writeJSON(w, status, map[string]any{"error": ValidationError{Parameter: parameter, Message: message}})
}

func writeValidationError(w http.ResponseWriter, verr *ValidationError) {
	writeError(w, http.StatusBadRequest, verr.Parameter, verr.Message)
}

func writeAPIError(w http.ResponseWriter, aerr *apiError) {
	writeError(w, aerr.status, aerr.err.Parameter, aerr.err.Message)
}

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
