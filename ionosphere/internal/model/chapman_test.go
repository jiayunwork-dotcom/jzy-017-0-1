package model

import (
	"math"
	"testing"
)

func testParams(chi float64) Params {
	return Params{Nm: 1.2e12, Hm: 300e3, H: 60e3, ChiDeg: chi}
}

// 天顶角为零时峰点密度必须等于给定峰值密度。
func TestPeakDensityAtZenith(t *testing.T) {
	p := testParams(0)
	got := p.PeakDensity()
	if math.Abs(got-p.Nm)/p.Nm > 1e-12 {
		t.Fatalf("peak density at chi=0 = %.6e, want %.6e", got, p.Nm)
	}
	if d := p.DensityAt(p.Hm); math.Abs(d-p.Nm)/p.Nm > 1e-12 {
		t.Fatalf("density at hm at chi=0 = %.6e, want Nm %.6e", d, p.Nm)
	}
}

// 极大值高度等于峰值高度：峰值高度上下两侧密度都不得超过峰点值。
func TestMaximumAtPeakHeight(t *testing.T) {
	for _, chi := range []float64{0, 30, 60, 80} {
		p := testParams(chi)
		peak := p.DensityAt(p.Hm)
		for _, step := range []float64{1e-3, 1.0, 100.0, 1000.0, 5000.0, 60000.0} {
			for _, sign := range []float64{-1, 1} {
				h := p.Hm + sign*step
				if h < 0 {
					continue
				}
				if d := p.DensityAt(h); d > peak*(1+1e-12) {
					t.Fatalf("chi=%.0f density %.6e at h=%.1f exceeds peak %.6e", chi, d, h, peak)
				}
			}
		}
	}
}

// 峰在剖面网格上就是最密点，且样本里的最大密度出现在 hm。
func TestProfileArgmaxIsHm(t *testing.T) {
	p := testParams(45)
	prof := p.BuildProfile(Geometry{GroundAltitude: 0, TopAltitude: 2000e3}, 2e3)
	idx, max := 0, -1.0
	foundHm := false
	for i, s := range prof.Samples {
		if s.Density > max {
			max, idx = s.Density, i
		}
		if s.Altitude == p.Hm {
			foundHm = true
		}
	}
	if !foundHm {
		t.Fatal("hm must be a grid point")
	}
	if math.Abs(prof.Samples[idx].Altitude-p.Hm) > 1e-6 {
		t.Fatalf("argmax altitude = %.2f, want hm = %.2f", prof.Samples[idx].Altitude, p.Hm)
	}
	if math.Abs(prof.PeakDensity-p.PeakDensity())/p.PeakDensity() > 1e-12 {
		t.Fatalf("profile peak field %.6e != computed %.6e", prof.PeakDensity, p.PeakDensity())
	}
}

// 单独把峰值密度翻倍，整条剖面翻倍。
func TestDoublePeakDensityDoublesProfile(t *testing.T) {
	p1 := testParams(37)
	p2 := p1
	p2.Nm = 2 * p1.Nm
	g := Geometry{GroundAltitude: 0, TopAltitude: 2000e3}
	a := p1.BuildProfile(g, 2e3)
	b := p2.BuildProfile(g, 2e3)
	if len(a.Samples) != len(b.Samples) {
		t.Fatalf("grid lengths differ %d vs %d", len(a.Samples), len(b.Samples))
	}
	for i := range a.Samples {
		if a.Samples[i].Altitude != b.Samples[i].Altitude {
			t.Fatalf("altitude grids diverge at %d", i)
		}
		want := 2 * a.Samples[i].Density
		if a.Samples[i].Density == 0 {
			if b.Samples[i].Density != 0 {
				t.Fatalf("tail density mismatch at %.0f", a.Samples[i].Altitude)
			}
			continue
		}
		if relErr(b.Samples[i].Density, want) > 1e-12 {
			t.Fatalf("density at %.0f = %.4e, want 2x %.4e", a.Samples[i].Altitude, b.Samples[i].Density, want)
		}
	}
}

// 单独把标高翻倍，峰仍停在原峰值高度（层只变厚）。
func TestDoubleScaleHeightPeakStays(t *testing.T) {
	p1 := testParams(20)
	p2 := p1
	p2.H = 2 * p1.H
	if peakA, peakB := p1.DensityAt(p1.Hm), p2.DensityAt(p2.Hm); relErr(peakB, peakA) > 1e-12 {
		t.Fatalf("doubling H moved/changed peak: %.6e vs %.6e", peakB, peakA)
	}
	// 远离峰的翼部应变厚：一倍标高处，厚层密度更大。
	wingA := p1.DensityAt(p1.Hm + p1.H)
	wingB := p2.DensityAt(p2.Hm + p1.H)
	if !(wingB > wingA) {
		t.Fatalf("thicker layer wing %.4e should exceed thin wing %.4e", wingB, wingA)
	}
}

// 单独抬高峰值高度，峰位必须跟随。
func TestRaisedPeakMovesUp(t *testing.T) {
	p1 := testParams(10)
	p2 := p1
	p2.Hm = 500e3
	for _, h := range []float64{p1.Hm - 50e3, p1.Hm, p1.Hm + 50e3} {
		// 旧峰附近在抬升后的剖面里不应超过新峰
		if d := p2.DensityAt(h); d > p2.DensityAt(p2.Hm)*(1+1e-12) {
			t.Fatalf("raised layer has density %.4e at old-region %.0f above new peak", d, h)
		}
	}
	if d1, d2 := p2.DensityAt(p2.Hm), p2.PeakDensity(); relErr(d1, d2) > 1e-12 {
		t.Fatalf("new peak not at raised hm: %.6e vs %.6e", d1, d2)
	}
}

// 天顶角从 0 增到 60 度，峰点密度必须下降，且峰位不漂移。
func TestPeakDecreasesWithZenith(t *testing.T) {
	var prev float64 = math.Inf(1)
	for _, chi := range []float64{0, 15, 30, 45, 60} {
		p := testParams(chi)
		n := p.PeakDensity()
		if !(n < prev) {
			t.Fatalf("peak density did not decrease at chi=%.0f: %.6e >= %.6e", chi, n, prev)
		}
		if math.Abs(p.Hm-testParams(0).Hm) > 0 {
			t.Fatalf("peak height drifted with zenith")
		}
		prev = n
	}
}

// 峰因子的单调与边界量值：sec(60°)=2，因子为 exp(-0.5)。
func TestPeakFactor60Degrees(t *testing.T) {
	got := PeakFactor(60)
	want := math.Exp(-0.5)
	if relErr(got, want) > 1e-12 {
		t.Fatalf("PeakFactor(60) = %.6e, want %.6e", got, want)
	}
}

func relErr(got, want float64) float64 {
	return math.Abs(got-want) / math.Max(math.Abs(want), 1e-300)
}
