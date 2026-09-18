// Package validate parses and validates Chapman layer input.
//
// Pointer fields let the validator distinguish "field missing" from "field
// present with a zero value". Unknown JSON fields and non-numeric values are
// rejected at decoding time by the HTTP layer; here every supplied field must
// be a finite number, peak density and scale height must be positive, the
// peak must lie strictly above the ground datum, the solar zenith angle must
// be a daytime value in [0, 90) degrees, and the fixed integration ceiling
// must clear the peak.
package validate

import (
	"fmt"
	"math"

	"ionosphere/internal/model"
)

// Layer is the wire representation of one layer specification. All fields are
// optional at the type level so Validate can report exactly which one is
// missing or invalid.
type Layer struct {
	PeakDensity *float64 `json:"peak_density"`
	PeakHeight  *float64 `json:"peak_height"`
	ScaleHeight *float64 `json:"scale_height"`
	ZenithAngle *float64 `json:"zenith_angle"`
}

// Error identifies a single invalid input field.
type Error struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func finite(v *float64, field string) (float64, *Error) {
	if v == nil {
		return 0, &Error{Field: field, Message: "field is required"}
	}
	if math.IsNaN(*v) || math.IsInf(*v, 0) {
		return 0, &Error{Field: field, Message: "must be a finite number"}
	}
	return *v, nil
}

// Validate checks one layer against the fixed geometry and converts it to a
// model.Params. It returns the first offending field, if any.
func Validate(in Layer, groundAltitude, topAltitude float64) (model.Params, *Error) {
	nm, err := finite(in.PeakDensity, "peak_density")
	if err != nil {
		return model.Params{}, err
	}
	if nm <= 0 {
		return model.Params{}, &Error{Field: "peak_density", Message: "must be positive"}
	}

	hm, err := finite(in.PeakHeight, "peak_height")
	if err != nil {
		return model.Params{}, err
	}
	if hm <= groundAltitude {
		return model.Params{}, &Error{
			Field:   "peak_height",
			Message: fmt.Sprintf("must be strictly above the ground datum (%.0f m)", groundAltitude),
		}
	}

	h, err := finite(in.ScaleHeight, "scale_height")
	if err != nil {
		return model.Params{}, err
	}
	if h <= 0 {
		return model.Params{}, &Error{Field: "scale_height", Message: "must be positive"}
	}

	chi, err := finite(in.ZenithAngle, "zenith_angle")
	if err != nil {
		return model.Params{}, err
	}
	if chi < 0 || chi >= 90 {
		return model.Params{}, &Error{
			Field:   "zenith_angle",
			Message: "must be in [0, 90) degrees; 90 degrees and beyond is night side and is rejected",
		}
	}

	if topAltitude <= hm {
		return model.Params{}, &Error{
			Field:   "peak_height",
			Message: fmt.Sprintf("integration top %.0f m is insufficient to cover peak height %.0f m", topAltitude, hm),
		}
	}

	p := model.Params{Nm: nm, Hm: hm, H: h, ChiDeg: chi}
	// A chi nominally inside [0,90) but so close to 90 that the peak
	// underflows is not a computable daytime layer either; refuse loudly.
	if !(p.PeakDensity() > 0) {
		return model.Params{}, &Error{
			Field:   "zenith_angle",
			Message: "too close to 90 degrees: peak density underflows, this is not a daytime layer",
		}
	}
	return p, nil
}
