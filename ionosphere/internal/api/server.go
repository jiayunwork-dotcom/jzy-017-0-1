// Package api wires the Chapman model, numerical integrator, validator and
// persistence store into the HTTP service.
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"ionosphere/internal/config"
	"ionosphere/internal/model"
	"ionosphere/internal/persistence"
	"ionosphere/internal/tec"
	"ionosphere/internal/validate"
)

// DemoLayer is the built-in noon F2-layer example (chi = 0).
var DemoLayer = validate.Layer{
	PeakDensity: fp(1.2e12), // peak density 1.2e12 m^-3
	PeakHeight:  fp(300e3),  // peak height 300 km
	ScaleHeight: fp(60e3),   // scale height 60 km
	ZenithAngle: fp(0),      // solar zenith angle
}

// Server holds the service dependencies. It is safe for concurrent use:
// the integrator is read-only and the store is concurrency safe, so no
// per-request state is shared.
type Server struct {
	cfg    config.Config
	integ  *tec.Integrator
	store  persistence.Store
	mux    *http.ServeMux
}

// NewServer builds a Server and registers routes.
func NewServer(cfg config.Config, store persistence.Store) *Server {
	s := &Server{
		cfg:   cfg,
		integ: tec.NewIntegrator(cfg.GroundAltitude, cfg.TopAltitude, cfg.Tolerance, 18),
		store: store,
	}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("POST /api/v1/profile", s.handleProfile)
	s.mux.HandleFunc("POST /api/v1/tec", s.handleTEC)
	s.mux.HandleFunc("POST /api/v1/batch", s.handleBatch)
	s.mux.HandleFunc("GET /api/v1/demo", s.handleDemo)
	s.mux.HandleFunc("GET /api/v1/history", s.handleHistory)
	s.mux.HandleFunc("GET /api/v1/config", s.handleConfig)
	s.mux.HandleFunc("GET /livez", s.handleLivez)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)
	return s
}

// Handler exposes the routed HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

func fp(v float64) *float64 { return &v }

// itemResult is the outcome of processing one layer, before persistence.
type itemResult struct {
	params       *model.Params
	verr         *validate.Error
	tecU         float64
	resultPeak   float64
	panels       int
	profile      *model.Profile
	includeProf  bool
	raw          json.RawMessage
}

// computeLayer validates, integrates and (optionally) profiles one layer.
// Persistence is handled by the caller.
func (s *Server) computeLayer(in validate.Layer, raw json.RawMessage, includeProfile bool) itemResult {
	out := itemResult{raw: raw, includeProf: includeProfile}
	p, verr := validate.Validate(in, s.cfg.GroundAltitude, s.cfg.TopAltitude)
	if verr != nil {
		out.verr = verr
		return out
	}
	t, panels, err := s.integ.Integrate(p)
	if err != nil {
		out.verr = &validate.Error{Field: "integration", Message: err.Error()}
		return out
	}
	out.params = &p
	out.tecU = t
	out.resultPeak = p.PeakDensity()
	out.panels = panels
	if includeProfile {
		prof := p.BuildProfile(model.Geometry{
			GroundAltitude: s.cfg.GroundAltitude,
			TopAltitude:    s.cfg.TopAltitude,
		}, s.cfg.ProfileStep)
		out.profile = &prof
	}
	return out
}

func (s *Server) toRecord(mode, batchID string, idx int, r itemResult) persistence.Result {
	rec := persistence.Result{
		CreatedAt: time.Now().UTC(),
		Mode:      mode,
		BatchID:   batchID,
		ItemIndex: idx,
		Raw:       r.raw,
	}
	if r.verr != nil {
		rec.Status = persistence.StatusError
		rec.ErrorField = r.verr.Field
		rec.ErrorMessage = r.verr.Message
	} else {
		rec.Status = persistence.StatusOK
		nm, hm, h, chi := r.params.Nm, r.params.Hm, r.params.H, r.params.ChiDeg
		rec.PeakDensity, rec.PeakHeight, rec.ScaleHeight, rec.ZenithAngle = &nm, &hm, &h, &chi
		rec.TECU = &r.tecU
		rec.ResultPeakDensity = &r.resultPeak
		panels := r.panels
		rec.Panels = &panels
	}
	if r.includeProf && r.profile != nil {
		if b, err := json.Marshal(r.profile); err == nil {
			rec.ProfileJSON = b
		}
	}
	return rec
}

// responseItem is the JSON view of one computed layer.
type responseItem struct {
	Index       *int            `json:"index,omitempty"`
	OK          bool            `json:"ok"`
	PeakDensity *float64        `json:"peak_density,omitempty"`
	PeakHeight  *float64        `json:"peak_height,omitempty"`
	TECU        *float64        `json:"tec_u,omitempty"`
	Panels      *int            `json:"panels,omitempty"`
	Profile     *model.Profile  `json:"profile,omitempty"`
	Error       *validate.Error `json:"error,omitempty"`
}

func (s *Server) itemResponse(r itemResult, idx *int) responseItem {
	item := responseItem{Index: idx}
	if r.verr != nil {
		item.Error = r.verr
		return item
	}
	item.OK = true
	nm := r.params.Nm
	item.PeakDensity = &nm
	hm := r.params.Hm
	item.PeakHeight = &hm
	t := r.tecU
	item.TECU = &t
	p := r.panels
	item.Panels = &p
	item.Profile = r.profile
	return item
}

func newBatchID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// --- request decoding ------------------------------------------------------

type singleRequest struct {
	validate.Layer
}

type batchRequest struct {
	Layers         []json.RawMessage `json:"layers"`
	IncludeProfile bool              `json:"include_profile"`
}

// decodeLayer unmarshals one layer object strictly into validate.Layer.
// On failure it returns a synthetic field error while preserving the raw JSON.
func decodeLayer(raw json.RawMessage) (validate.Layer, *validate.Error) {
	var in validate.Layer
	if len(raw) == 0 {
		return in, &validate.Error{Field: "layer", Message: "missing layer object"}
	}
	if err := strictJSONUnmarshal(raw, &in); err != nil {
		return validate.Layer{}, &validate.Error{Field: "layer", Message: cleanJSONError(err)}
	}
	return in, nil
}

func strictJSONUnmarshal(data []byte, v any) error {
	dec := json.NewDecoder(newBytesReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("unexpected trailing JSON tokens")
	}
	return nil
}

func newBytesReader(b []byte) io.Reader { return &bytesReader{b: b} }

type bytesReader struct {
	b []byte
	i int
}

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

func cleanJSONError(err error) string {
	var ute *json.UnmarshalTypeError
	if errors.As(err, &ute) {
		return fmt.Sprintf("%s must be a number", ute.Field)
	}
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

// --- small JSON helpers ----------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": map[string]string{
		"code":    code,
		"message": message,
	}})
}

func readBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", cleanJSONError(err))
		return false
	}
	if dec.More() {
		writeError(w, http.StatusBadRequest, "bad_request", "unexpected trailing JSON tokens")
		return false
	}
	return true
}
