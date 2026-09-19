package main

import (
	"math"
	"testing"
)

// testConfig mirrors the pinned service defaults used in tests.
func testConfig() Config {
	return Config{
		Port:           8080,
		GroundAltitude: 0,
		TopHeight:      1e6,
		TECTolerance:   1e-9,
		ProfilePoints:  400,
		MaxBatchSize:   500,
	}
}

func mustTEC(t *testing.T, p LayerParams) float64 {
	t.Helper()
	n0 := InitialIntervals(0, 1e6, p.ScaleHeight)
	tec, _, converged := IntegrateTEC(p.DensityAt, 0, 1e6, 1e-9, n0)
	if !converged {
		t.Fatalf("integration did not converge")
	}
	return tec
}

// The integrated TEC must be positive for any valid daylight layer.
func TestTECPositive(t *testing.T) {
	if tec := mustTEC(t, testLayer()); tec <= 0 {
		t.Fatalf("TEC = %v, want positive", tec)
	}
}

// Doubling only the peak density must double the TEC (the profile is
// exactly linear in NmF2).
func TestTECDoublesWithPeakDensity(t *testing.T) {
	p1 := testLayer()
	p2 := p1
	p2.PeakDensity *= 2
	ratio := mustTEC(t, p2) / mustTEC(t, p1)
	if math.Abs(ratio-2) > 1e-9 {
		t.Fatalf("TEC ratio = %v, want 2", ratio)
	}
}

// Doubling only the scale height must approximately double the TEC (the
// layer is fully contained between ground and the top height, so the
// integral scales with H up to negligible tail corrections).
func TestTECDoublesWithScaleHeight(t *testing.T) {
	p1 := testLayer()
	p1.ScaleHeight = 30e3
	p2 := p1
	p2.ScaleHeight *= 2
	ratio := mustTEC(t, p2) / mustTEC(t, p1)
	if math.Abs(ratio-2) > 0.01 {
		t.Fatalf("TEC ratio = %v, want approximately 2 (±1%%)", ratio)
	}
}

// The numerical integral must reproduce the analytic full-column Chapman
// value sqrt(2*pi*e) * NmF2 * H when the layer is fully covered. This pins
// the TEC to a real quadrature of the profile, not a canned coefficient.
func TestTECMatchesAnalyticChapmanIntegral(t *testing.T) {
	p := testLayer()
	p.ScaleHeight = 30e3 // z spans about [-10, 23]: tails are negligible
	tec := mustTEC(t, p)
	want := p.PeakDensity * p.ScaleHeight * math.Sqrt(2*math.Pi*math.E)
	if math.Abs(tec/want-1) > 1e-3 {
		t.Fatalf("TEC = %v, analytic full-layer value = %v", tec, want)
	}
}

// The TEC must come from a genuine quadrature over [ground, top]: cutting
// the integration off at the peak height collects only the lower flank of
// the layer. For a Chapman layer the column below the peak is
// erfc(1/sqrt(2)) ~= 0.3173 of the full column, so a "NmF2 * H * constant"
// impostor cannot pass this.
func TestTECRespondsToIntegrationBounds(t *testing.T) {
	p := testLayer()
	p.ScaleHeight = 30e3 // tails below ground negligible
	n0 := InitialIntervals(0, 1e6, p.ScaleHeight)
	full, _, ok := IntegrateTEC(p.DensityAt, 0, 1e6, 1e-9, n0)
	if !ok {
		t.Fatalf("full integration did not converge")
	}
	toPeak, _, ok := IntegrateTEC(p.DensityAt, 0, p.PeakHeight, 1e-9, n0)
	if !ok {
		t.Fatalf("partial integration did not converge")
	}
	ratio := toPeak / full
	if want := 0.3173; math.Abs(ratio-want) > 0.01 {
		t.Fatalf("column below the peak = %.4f of the full column, want ~%.4f", ratio, want)
	}
}

// Pushing the peak up against the integration top must clip the TEC: the
// part of the layer above the top is simply not integrated.
func TestTECClippedWhenPeakNearTop(t *testing.T) {
	p := testLayer() // H = 50 km
	full := mustTEC(t, p)
	p.PeakHeight = 950e3 // only z = 1 of headroom below the 1000 km top
	clipped := mustTEC(t, p)
	if ratio := clipped / full; ratio > 0.75 {
		t.Fatalf("TEC not clipped by the integration top: ratio = %v, want < 0.75", ratio)
	}
}

// Raising the peak height (with the top still well above it) must leave the
// TEC essentially unchanged: the integral follows the layer, it is not
// clipped by the upper bound.
func TestTECUnaffectedByPeakHeightShift(t *testing.T) {
	p1 := testLayer()
	p2 := p1
	p2.PeakHeight = 420e3
	ratio := mustTEC(t, p2) / mustTEC(t, p1)
	if math.Abs(ratio-1) > 0.01 {
		t.Fatalf("TEC ratio = %v, want approximately 1", ratio)
	}
}

// Refinement must actually converge within the interval budget.
func TestIntegrationConverges(t *testing.T) {
	p := testLayer()
	n0 := InitialIntervals(0, 1e6, p.ScaleHeight)
	_, intervals, converged := IntegrateTEC(p.DensityAt, 0, 1e6, 1e-9, n0)
	if !converged {
		t.Fatalf("did not converge after %d intervals", intervals)
	}
}

func TestTECUConversion(t *testing.T) {
	if got := ToTECU(2.5e16); got != 2.5 {
		t.Fatalf("ToTECU(2.5e16) = %v, want 2.5", got)
	}
	if TECU != 1e16 {
		t.Fatalf("TECU = %v, want 1e16", TECU)
	}
}
