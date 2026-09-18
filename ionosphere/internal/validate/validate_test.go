package validate

import (
	"math"
	"testing"
)

func f(v float64) *float64 { return &v }

func valid() Layer {
	return Layer{PeakDensity: f(1.2e12), PeakHeight: f(300e3), ScaleHeight: f(60e3), ZenithAngle: f(0)}
}

const (
	ground = 0
	top    = 2000e3
)

func TestValidLayer(t *testing.T) {
	p, err := Validate(valid(), ground, top)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Nm != 1.2e12 || p.Hm != 300e3 || p.H != 60e3 || p.ChiDeg != 0 {
		t.Fatalf("params mismatch: %+v", p)
	}
}

func TestMissingFields(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
		mut   func(*Layer)
	}{
		{"missing peak_density", "peak_density", func(l *Layer) { l.PeakDensity = nil }},
		{"missing peak_height", "peak_height", func(l *Layer) { l.PeakHeight = nil }},
		{"missing scale_height", "scale_height", func(l *Layer) { l.ScaleHeight = nil }},
		{"missing zenith_angle", "zenith_angle", func(l *Layer) { l.ZenithAngle = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := valid()
			tc.mut(&l)
			_, err := Validate(l, ground, top)
			if err == nil || err.Field != tc.field {
				t.Fatalf("want field %q, got %v", tc.field, err)
			}
		})
	}
}

func TestNonFiniteRejected(t *testing.T) {
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		l := valid()
		l.PeakDensity = f(bad)
		_, err := Validate(l, ground, top)
		if err == nil || err.Field != "peak_density" {
			t.Fatalf("bad=%v want peak_density error, got %v", bad, err)
		}
	}
}

func TestNonPositiveRejected(t *testing.T) {
	for _, tc := range []struct {
		field string
		mut   func(*Layer)
	}{
		{"peak_density", func(l *Layer) { l.PeakDensity = f(0) }},
		{"peak_density", func(l *Layer) { l.PeakDensity = f(-1) }},
		{"scale_height", func(l *Layer) { l.ScaleHeight = f(0) }},
		{"scale_height", func(l *Layer) { l.ScaleHeight = f(-10) }},
	} {
		l := valid()
		tc.mut(&l)
		_, err := Validate(l, ground, top)
		if err == nil || err.Field != tc.field {
			t.Fatalf("want %s error, got %v", tc.field, err)
		}
	}
}

// 峰值高度必须高于地面（地面取海拔零）。
func TestPeakAtOrBelowGroundRejected(t *testing.T) {
	for _, hm := range []float64{0, -1, -1000} {
		l := valid()
		l.PeakHeight = f(hm)
		_, err := Validate(l, ground, top)
		if err == nil || err.Field != "peak_height" {
			t.Fatalf("hm=%v want peak_height error, got %v", hm, err)
		}
	}
}

// 天顶角 90 度被拒，负值被拒，[0,90) 接受。
func TestZenithRange(t *testing.T) {
	for _, chi := range []float64{90, 91, 120, -0.1, -90} {
		l := valid()
		l.ZenithAngle = f(chi)
		_, err := Validate(l, ground, top)
		if err == nil || err.Field != "zenith_angle" {
			t.Fatalf("chi=%v want zenith_angle rejection, got %v", chi, err)
		}
	}
	// 89.9° 仍可计算；更逼近地平线时峰因子会下溢，被专门的白昼校验拒绝。
	for _, chi := range []float64{0, 0.0001, 45, 60, 89.9} {
		l := valid()
		l.ZenithAngle = f(chi)
		if _, err := Validate(l, ground, top); err != nil {
			t.Fatalf("chi=%v should be accepted, got %v", chi, err)
		}
	}
}

// 积分顶高不足以覆盖峰值高度时拒绝。
func TestTopMustCoverPeak(t *testing.T) {
	l := valid()
	l.PeakHeight = f(top)
	if _, err := Validate(l, ground, top); err == nil || err.Field != "peak_height" {
		t.Fatalf("hm == top must be rejected, got %v", err)
	}
	l = valid()
	l.PeakHeight = f(top + 1e3)
	if _, err := Validate(l, ground, top); err == nil {
		t.Fatal("hm above top must be rejected")
	}
}
