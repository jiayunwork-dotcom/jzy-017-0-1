// Package tec performs the vertical integration of an electron density
// profile over geometric height and converts the result to TECU.
//
// The vertical total electron content is
//
//	VTEC = integral N(h) dh  [electrons / m^2]
//
// and one TECU is exactly 1e16 electrons / m^2.
//
// The integral is really computed numerically (composite Simpson's rule with
// successive grid refinement until a relative tolerance is met); there is no
// "peak density times scale height times a fixed constant" shortcut, so a
// wrong profile shape can never be hidden behind a tuned multiplier.
package tec

import (
	"fmt"
	"math"

	"ionosphere/internal/model"
)

// TECU is one total-electron-content unit: 1e16 electrons per square metre.
const TECU = 1.0e16

// Integrator numerically integrates Chapman layers over the service-wide
// altitude interval. An Integrator is read-only after creation and safe for
// concurrent use.
type Integrator struct {
	top        float64
	ground     float64
	tolerance  float64
	maxDivides int
}

// NewIntegrator builds an Integrator. ground/top are altitudes in metres,
// tolerance is the relative convergence threshold on TEC below which grid
// refinement stops, and maxDivides bounds the number of refinements.
func NewIntegrator(ground, top, tolerance float64, maxDivides int) *Integrator {
	if maxDivides <= 0 {
		maxDivides = 16
	}
	return &Integrator{
		top:        top,
		ground:     ground,
		tolerance:  tolerance,
		maxDivides: maxDivides,
	}
}

// Integrate computes the vertical TEC of the layer in TECU together with the
// number of Simpson panels used at convergence. It returns an error if the
// integrated density is not strictly positive (for example an underflowed
// night-side layer), instead of silently emitting a zero day profile.
func (in *Integrator) Integrate(p model.Params) (tec float64, panels int, err error) {
	f := func(h float64) float64 { return p.DensityAt(h) }

	// Start with 16 panels and repeatedly double the resolution. Composite
	// Simpson needs an even number of panels.
	n := 16
	prev := math.NaN()
	var value float64
	for i := 0; i <= in.maxDivides; i++ {
		value = compositeSimpson(f, in.ground, in.top, n)
		panels = n
		if i > 0 {
			change := math.Abs(value-prev) / math.Max(math.Abs(prev), 1e-300)
			if change < in.tolerance {
				break
			}
		}
		prev = value
		n *= 2
	}

	if !(value > 0) {
		return 0, panels, fmt.Errorf("integrated TEC is not positive (%.3e); layer is too weak for a daytime profile", value)
	}
	return value / TECU, panels, nil
}

// compositeSimpson applies composite Simpson's 1/3 rule to f on [a,b] with n
// (even) equally spaced panels.
func compositeSimpson(f func(float64) float64, a, b float64, n int) float64 {
	if n%2 != 0 {
		n++
	}
	dx := (b - a) / float64(n)
	sum := f(a) + f(b)
	for i := 1; i < n; i++ {
		x := a + float64(i)*dx
		if i%2 == 0 {
			sum += 2 * f(x)
		} else {
			sum += 4 * f(x)
		}
	}
	return sum * dx / 3.0
}
