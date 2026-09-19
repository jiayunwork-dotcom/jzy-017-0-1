package main

import (
	"math"
	"testing"
)

// testLayer is a representative daytime F2 layer.
func testLayer() LayerParams {
	return LayerParams{
		PeakDensity:      1.2e12,
		PeakHeight:       300e3,
		ScaleHeight:      50e3,
		SolarZenithAngle: 0,
	}
}

// argmaxDensity scans the profile on an even grid and returns the height of
// the maximum density together with that density.
func argmaxDensity(p LayerParams, from, to, step float64) (float64, float64) {
	bestH, bestN := from, math.Inf(-1)
	for h := from; h <= to+step/2; h += step {
		if n := p.DensityAt(h); n > bestN {
			bestN, bestH = n, h
		}
	}
	return bestH, bestN
}

// At zero zenith angle the density at the peak height must equal the
// requested peak density exactly.
func TestZeroZenithPeakDensityEqualsInput(t *testing.T) {
	p := testLayer()
	if got := p.DensityAt(p.PeakHeight); got != p.PeakDensity {
		t.Fatalf("density at peak height = %v, want exactly %v", got, p.PeakDensity)
	}
	if got := p.PeakDensityAtPeak(); got != p.PeakDensity {
		t.Fatalf("peak density = %v, want exactly %v", got, p.PeakDensity)
	}
}

// The maximum of the profile must sit at the peak height, and neither flank
// may ever exceed the peak value.
func TestMaximumAtPeakHeight(t *testing.T) {
	p := testLayer()
	const step = 100.0
	hMax, _ := argmaxDensity(p, 0, 1e6, step)
	if math.Abs(hMax-p.PeakHeight) > step {
		t.Fatalf("argmax height = %v m, want within %v m of %v m", hMax, step, p.PeakHeight)
	}
	peak := p.PeakDensityAtPeak()
	for _, k := range []float64{0.05, 0.2, 0.5, 1, 2, 4} {
		for _, side := range []float64{-1, 1} {
			h := p.PeakHeight + side*k*p.ScaleHeight
			if n := p.DensityAt(h); n > peak {
				t.Fatalf("density above peak at h=%v m: %v > %v", h, n, peak)
			}
		}
	}
}

// The peak position must not drift with the solar zenith angle.
func TestPeakDoesNotDriftWithZenithAngle(t *testing.T) {
	for _, chi := range []float64{0, 15, 30, 45, 60, 75, 85} {
		p := testLayer()
		p.SolarZenithAngle = chi
		const step = 200.0
		hMax, _ := argmaxDensity(p, 0, 1e6, step)
		if math.Abs(hMax-p.PeakHeight) > step {
			t.Fatalf("chi=%v: argmax = %v m, want within %v m of %v m", chi, hMax, step, p.PeakHeight)
		}
	}
}

// Increasing the zenith angle from 0 to 60 degrees must lower the peak
// density (by exactly exp(0.5*(1-sec 60°)) = e^-0.5).
func TestZenithAngleReducesPeakDensity(t *testing.T) {
	p := testLayer()
	n0 := p.PeakDensityAtPeak()
	p.SolarZenithAngle = 60
	n60 := p.PeakDensityAtPeak()
	if !(n60 < n0) {
		t.Fatalf("peak density at 60° (%v) is not below the 0° value (%v)", n60, n0)
	}
	if want := math.Exp(-0.5); math.Abs(n60/n0-want) > 1e-12 {
		t.Fatalf("n60/n0 = %v, want %v", n60/n0, want)
	}
}

// Doubling the scale height must keep the peak where it is, thicken the
// layer and preserve the self-similar Chapman shape.
func TestScaleHeightDoublingKeepsPeakAndThickens(t *testing.T) {
	p1 := testLayer()
	p2 := p1
	p2.ScaleHeight *= 2
	const step = 200.0
	h1, _ := argmaxDensity(p1, 0, 1e6, step)
	h2, _ := argmaxDensity(p2, 0, 1e6, step)
	if math.Abs(h1-p1.PeakHeight) > step || math.Abs(h2-p1.PeakHeight) > step {
		t.Fatalf("peak moved: H gives %v m, 2H gives %v m, want %v m", h1, h2, p1.PeakHeight)
	}
	// Self-similarity: N(2H; hm+2d) == N(H; hm+d).
	for _, d := range []float64{10e3, 40e3, 100e3} {
		a := p1.DensityAt(p1.PeakHeight + d)
		b := p2.DensityAt(p2.PeakHeight + 2*d)
		if math.Abs(a-b) > 1e-9*a {
			t.Fatalf("self-similarity broken at d=%v: %v vs %v", d, a, b)
		}
	}
	// The thicker layer is denser well above the peak.
	if p2.DensityAt(p1.PeakHeight+150e3) <= p1.DensityAt(p1.PeakHeight+150e3) {
		t.Fatalf("layer did not thicken when the scale height doubled")
	}
}

// Raising the peak height must move the profile maximum up with it.
func TestPeakFollowsRaisedPeakHeight(t *testing.T) {
	p := testLayer()
	p.PeakHeight = 420e3
	const step = 200.0
	hMax, _ := argmaxDensity(p, 0, 1e6, step)
	if math.Abs(hMax-p.PeakHeight) > step {
		t.Fatalf("argmax = %v m, want within %v m of raised peak %v m", hMax, step, p.PeakHeight)
	}
}

// The density must be positive and finite across the whole column.
func TestDensityPositiveEverywhere(t *testing.T) {
	p := testLayer()
	for h := 0.0; h <= 1e6; h += 25e3 {
		if n := p.DensityAt(h); !(n > 0) || math.IsNaN(n) || math.IsInf(n, 0) {
			t.Fatalf("density at %v m = %v, want positive and finite", h, n)
		}
	}
}

// The built-in demo case: peak at the given height, peak density equal to
// the input at zero zenith angle, positive TEC.
func TestDemoCase(t *testing.T) {
	p := DemoParams()
	if got := p.DensityAt(p.PeakHeight); got != p.PeakDensity {
		t.Fatalf("demo: density at peak = %v, want %v", got, p.PeakDensity)
	}
	comp, err := Compute(testConfig(), p, 100)
	if err != nil {
		t.Fatalf("demo compute failed: %v", err)
	}
	if comp.Peak.Height != p.PeakHeight {
		t.Fatalf("demo: peak height = %v, want %v", comp.Peak.Height, p.PeakHeight)
	}
	if comp.TECU <= 0 {
		t.Fatalf("demo: TECU = %v, want positive", comp.TECU)
	}
}
