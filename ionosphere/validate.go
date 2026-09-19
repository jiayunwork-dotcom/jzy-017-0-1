package main

import (
	"encoding/json"
	"fmt"
	"math"
)

// ValidationError pinpoints a single bad input parameter.
type ValidationError struct {
	Parameter string `json:"parameter,omitempty"`
	Message   string `json:"message"`
}

func (e *ValidationError) Error() string {
	if e.Parameter != "" {
		return fmt.Sprintf("%s: %s", e.Parameter, e.Message)
	}
	return e.Message
}

// ParseLayerParams decodes a JSON object into LayerParams and validates it
// against the service configuration. Every failure names the offending
// parameter so callers (and batch clients) learn exactly what is wrong.
func ParseLayerParams(data []byte, cfg Config) (LayerParams, *ValidationError) {
	var p LayerParams
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return p, &ValidationError{Message: "request body must be a JSON object"}
	}
	read := func(name string) (float64, *ValidationError) {
		r, ok := raw[name]
		if !ok || string(r) == "null" {
			return 0, &ValidationError{Parameter: name, Message: "missing required field"}
		}
		var num json.Number
		if err := json.Unmarshal(r, &num); err != nil {
			return 0, &ValidationError{Parameter: name, Message: "must be a number"}
		}
		f, err := num.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, &ValidationError{Parameter: name, Message: "must be a finite number"}
		}
		return f, nil
	}
	var verr *ValidationError
	if p.PeakDensity, verr = read("peakDensity"); verr != nil {
		return p, verr
	}
	if p.PeakHeight, verr = read("peakHeight"); verr != nil {
		return p, verr
	}
	if p.ScaleHeight, verr = read("scaleHeight"); verr != nil {
		return p, verr
	}
	if p.SolarZenithAngle, verr = read("solarZenithAngle"); verr != nil {
		return p, verr
	}
	return p, ValidateLayerParams(p, cfg)
}

// ValidateLayerParams enforces the physical constraints of a daylight
// Chapman layer:
//
//	peakDensity > 0
//	scaleHeight > 0
//	peakHeight  > ground altitude (the peak must sit above the surface)
//	peakHeight  < integration top height (the integral must cover the peak)
//	0 <= solarZenithAngle < 90 degrees (at/above 90 it is no longer a
//	daylight layer and the request is rejected outright)
func ValidateLayerParams(p LayerParams, cfg Config) *ValidationError {
	if p.PeakDensity <= 0 {
		return &ValidationError{Parameter: "peakDensity", Message: "must be positive"}
	}
	if p.ScaleHeight <= 0 {
		return &ValidationError{Parameter: "scaleHeight", Message: "must be positive"}
	}
	if p.PeakHeight <= cfg.GroundAltitude {
		return &ValidationError{Parameter: "peakHeight",
			Message: fmt.Sprintf("must be above ground altitude (%g m)", cfg.GroundAltitude)}
	}
	if p.PeakHeight >= cfg.TopHeight {
		return &ValidationError{Parameter: "peakHeight",
			Message: fmt.Sprintf("integration top height (%g m) does not cover the peak height", cfg.TopHeight)}
	}
	if p.SolarZenithAngle < 0 {
		return &ValidationError{Parameter: "solarZenithAngle", Message: "must be >= 0 degrees"}
	}
	if p.SolarZenithAngle >= 90 {
		return &ValidationError{Parameter: "solarZenithAngle",
			Message: "must be < 90 degrees; at or beyond 90 the layer is not a daylight layer"}
	}
	return nil
}
