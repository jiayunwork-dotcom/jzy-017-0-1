package api

import (
	"net/http"
	"strconv"
	"time"

	"ionosphere/internal/persistence"
	"ionosphere/internal/validate"
)

// handleProfile computes the full electron density profile plus TEC for one
// layer.
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	var req singleRequest
	if !readBody(w, r, &req) {
		return
	}
	raw, _ := marshalRaw(req.Layer)
	res := s.computeLayer(req.Layer, raw, true)
	rec := s.toRecord(persistence.ModeSingle, "", 0, res)
	if err := s.store.Save(r.Context(), []persistence.Result{rec}); err != nil {
		writeError(w, http.StatusInternalServerError, "persistence", err.Error())
		return
	}
	if res.verr != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"ok": false, "error": res.verr})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": s.itemResponse(res, nil)})
}

// handleTEC is the lightweight TEC-only endpoint.
func (s *Server) handleTEC(w http.ResponseWriter, r *http.Request) {
	var req singleRequest
	if !readBody(w, r, &req) {
		return
	}
	raw, _ := marshalRaw(req.Layer)
	res := s.computeLayer(req.Layer, raw, false)
	rec := s.toRecord(persistence.ModeSingle, "", 0, res)
	if err := s.store.Save(r.Context(), []persistence.Result{rec}); err != nil {
		writeError(w, http.StatusInternalServerError, "persistence", err.Error())
		return
	}
	if res.verr != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"ok": false, "error": res.verr})
		return
	}
	item := s.itemResponse(res, nil)
	item.Profile = nil
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": item})
}

// handleBatch computes many layer groups in one call. Invalid groups are
// reported with their index and offending field; valid groups are still
// computed. Nothing is dropped and the request never fails wholesale for a
// bad group.
func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) {
	var req batchRequest
	if !readBody(w, r, &req) {
		return
	}
	if len(req.Layers) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "layers must contain at least one item")
		return
	}

	batchID := newBatchID()
	items := make([]responseItem, len(req.Layers))
	recs := make([]persistence.Result, 0, len(req.Layers))

	for i, raw := range req.Layers {
		idx := i
		layer, derr := decodeLayer(raw)
		if derr != nil {
			res := itemResult{raw: raw, verr: derr, includeProf: req.IncludeProfile}
			recs = append(recs, s.toRecord(persistence.ModeBatch, batchID, i, res))
			items[i] = s.itemResponse(res, &idx)
			continue
		}
		res := s.computeLayer(layer, raw, req.IncludeProfile)
		recs = append(recs, s.toRecord(persistence.ModeBatch, batchID, i, res))
		items[i] = s.itemResponse(res, &idx)
	}

	if err := s.store.Save(r.Context(), recs); err != nil {
		writeError(w, http.StatusInternalServerError, "persistence", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"batch_id": batchID,
		"count":    len(items),
		"items":    items,
	})
}

// handleDemo runs the built-in noon F2-layer example.
func (s *Server) handleDemo(w http.ResponseWriter, r *http.Request) {
	raw, _ := marshalRaw(DemoLayer)
	res := s.computeLayer(DemoLayer, raw, true)
	rec := s.toRecord(persistence.ModeSingle, "demo", 0, res)
	_ = s.store.Save(r.Context(), []persistence.Result{rec})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"description": "built-in noon F2-layer example (zenith angle 0)",
		"item":        s.itemResponse(res, nil),
	})
}

// handleConfig echoes the fixed and configurable numerical settings.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"config": map[string]any{
			"ground_altitude_m":   s.cfg.GroundAltitude,
			"top_altitude_m":      s.cfg.TopAltitude,
			"profile_step_m":      s.cfg.ProfileStep,
			"tec_u_conversion":    s.cfg.TECU,
			"integration_tolerance": s.cfg.Tolerance,
			"units": map[string]string{
				"altitude":     "metres above ground datum (0 m)",
				"density":      "electrons per cubic metre",
				"tec":          "TECU",
				"zenith_angle": "degrees",
			},
		},
	})
}

// handleHistory retrieves past computations by condition.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	f := persistence.Filter{Limit: 50}
	q := r.URL.Query()
	f.Status = q.Get("status")
	f.Mode = q.Get("mode")
	f.BatchID = q.Get("batch_id")
	if v := q.Get("min_tec_u"); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err == nil {
			f.MinTECU = &x
		}
	}
	if v := q.Get("max_tec_u"); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err == nil {
			f.MaxTECU = &x
		}
	}
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Since = &t
		}
	}
	if v := q.Get("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Until = &t
		}
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			f.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			f.Offset = n
		}
	}

	recs, err := s.store.Query(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "persistence", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(recs), "results": recs})
}

// handleLivez is a simple liveness probe.
func (s *Server) handleLivez(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
}

// handleReadyz verifies the compute service and its database.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 3*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unavailable", "database": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "database": "ok"})
}

func marshalRaw(in validate.Layer) ([]byte, error) {
	return jsonMarshal(map[string]any{
		"peak_density": in.PeakDensity,
		"peak_height":  in.PeakHeight,
		"scale_height": in.ScaleHeight,
		"zenith_angle": in.ZenithAngle,
	})
}
