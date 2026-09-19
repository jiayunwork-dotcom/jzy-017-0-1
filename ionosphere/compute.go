package main

import (
	"errors"
	"math"
)

// PeakInfo is the computed apex of the layer.
type PeakInfo struct {
	Height  float64 `json:"height"`  // m above ground
	Density float64 `json:"density"` // m^-3
}

// IntegrationInfo describes how the TEC integral was evaluated.
type IntegrationInfo struct {
	From      float64 `json:"from"`      // m
	To        float64 `json:"to"`        // m
	Intervals int     `json:"intervals"` // trapezoid intervals at convergence
	Converged bool    `json:"converged"` // refinement met the tolerance
}

// ProfilePoint is one (height, density) sample of the layer.
type ProfilePoint struct {
	Height  float64 `json:"height"`  // m
	Density float64 `json:"density"` // m^-3
}

// Computation is the full result of evaluating one Chapman layer.
type Computation struct {
	Input       LayerParams     `json:"input"`
	Peak        PeakInfo        `json:"peak"`
	TEC         float64         `json:"tec"`  // m^-2
	TECU        float64         `json:"tecu"` // TEC units
	Integration IntegrationInfo `json:"integration"`
	Profile     []ProfilePoint  `json:"profile,omitempty"`
}

var (
	errNotConverged   = errors.New("numerical integration did not converge within the refinement limit")
	errNonPositiveTEC = errors.New("integrated TEC is not positive")
)

// Compute evaluates the Chapman layer for validated parameters: it samples
// the profile (when profilePoints > 0) and integrates the density from the
// configured ground level to the configured top height to obtain the TEC.
func Compute(cfg Config, p LayerParams, profilePoints int) (*Computation, error) {
	n0 := InitialIntervals(cfg.GroundAltitude, cfg.TopHeight, p.ScaleHeight)
	tec, intervals, converged := IntegrateTEC(p.DensityAt, cfg.GroundAltitude, cfg.TopHeight, cfg.TECTolerance, n0)
	if !converged {
		return nil, errNotConverged
	}
	if math.IsNaN(tec) || math.IsInf(tec, 0) || tec <= 0 {
		return nil, errNonPositiveTEC
	}
	comp := &Computation{
		Input: p,
		Peak: PeakInfo{
			Height:  p.PeakHeight,
			Density: p.PeakDensityAtPeak(),
		},
		TEC:  tec,
		TECU: ToTECU(tec),
		Integration: IntegrationInfo{
			From:      cfg.GroundAltitude,
			To:        cfg.TopHeight,
			Intervals: intervals,
			Converged: converged,
		},
	}
	if profilePoints > 0 {
		comp.Profile = SampleProfile(p, cfg.GroundAltitude, cfg.TopHeight, profilePoints)
	}
	return comp, nil
}

// SampleProfile evaluates the density on an evenly spaced height grid from
// from to to (both included), n points total.
func SampleProfile(p LayerParams, from, to float64, n int) []ProfilePoint {
	if n < 2 {
		n = 2
	}
	pts := make([]ProfilePoint, n)
	step := (to - from) / float64(n-1)
	for i := 0; i < n; i++ {
		h := from + float64(i)*step
		if i == n-1 {
			h = to // avoid float drift on the last point
		}
		pts[i] = ProfilePoint{Height: h, Density: p.DensityAt(h)}
	}
	return pts
}
