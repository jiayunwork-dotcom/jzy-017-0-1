package tec

import (
	"math"
	"testing"

	"ionosphere/internal/model"
)

const (
	ground = 0.0
	top    = 2000e3
)

func baseParams(chi float64) model.Params {
	return model.Params{Nm: 1.2e12, Hm: 300e3, H: 60e3, ChiDeg: chi}
}

func newTestIntegrator() *Integrator {
	return NewIntegrator(ground, top, 1e-6, 20)
}

// TEC 必须为正，单位为 TECU。
func TestTECPositiveAndPhysical(t *testing.T) {
	in := newTestIntegrator()
	v, panels, err := in.Integrate(baseParams(0))
	if err != nil {
		t.Fatal(err)
	}
	if !(v > 0) {
		t.Fatalf("TEC = %v, want positive", v)
	}
	// Nm*H = 1.2e12 * 60e3 = 7.2e16 m^-2; 解析常数约 4.5e，
	// 结果应在每平方米 1e17 量级，即若干 TECU 到几十 TECU。
	if v < 1 || v > 1000 {
		t.Fatalf("TEC = %.3f TECU outside physical noon-F2 range", v)
	}
	t.Logf("noon F2 demo TEC = %.3f TECU (%d panels)", v, panels)
}

// 数值积分必须真沿高度积分：与在更密网格上的高精度参考解吻合，而不是系数公式。
func TestIntegralConvergesToReference(t *testing.T) {
	p := baseParams(0)
	in := NewIntegrator(ground, top, 1e-11, 22)
	v, _, err := in.Integrate(p)
	if err != nil {
		t.Fatal(err)
	}
	// 高分辨率复合辛普森独立参考。
	ref := compositeSimpson(func(h float64) float64 { return p.DensityAt(h) }, ground, top, 1<<18) / TECU
	if math.Abs(v-ref)/ref > 1e-8 {
		t.Fatalf("TEC %.6f disagrees with fine reference %.6f", v, ref)
	}
}

// 单独把峰值密度翻倍，TEC 翻倍（线性性）。
func TestDoublePeakDensityDoublesTEC(t *testing.T) {
	in := newTestIntegrator()
	p1 := baseParams(25)
	p2 := p1
	p2.Nm *= 2
	v1, _, err := in.Integrate(p1)
	if err != nil {
		t.Fatal(err)
	}
	v2, _, err := in.Integrate(p2)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(v2-2*v1)/v1 > 1e-9 {
		t.Fatalf("doubling Nm gave ratio %.6f, want 2", v2/v1)
	}
}

// 单独把标高翻倍，层变厚、TEC 近似翻倍；顶高足够时收敛到精确的两倍。
func TestDoubleScaleHeightApproxDoublesTEC(t *testing.T) {
	in := newTestIntegrator()
	p1 := baseParams(0)
	p2 := p1
	p2.H = 2 * p1.H
	v1, _, _ := in.Integrate(p1)
	v2, _, _ := in.Integrate(p2)
	ratio := v2 / v1
	if ratio < 1.98 || ratio > 2.02 {
		t.Fatalf("doubling H gave TEC ratio %.4f, want ~2", ratio)
	}
}

// 抬高峰值高度：顶高足够覆盖时，TEC 不应被上界截掉（高 hm 结果趋近同一无穷域值）。
func TestRaisedPeakNotTruncated(t *testing.T) {
	in := newTestIntegrator()
	pLow := baseParams(0)
	pHigh := pLow
	pHigh.Hm = 700e3 // 距 2000 km 顶仍有 1300 km ≈ 21 个厚标高
	vLow, _, err := in.Integrate(pLow)
	if err != nil {
		t.Fatal(err)
	}
	vHigh, _, err := in.Integrate(pHigh)
	if err != nil {
		t.Fatal(err)
	}
	// 顶高足够时积分域均近似覆盖全层，二者应几乎相等。
	if math.Abs(vHigh-vLow)/vLow > 2e-3 {
		t.Fatalf("raised peak truncated: low-hm TEC %.4f vs high-hm %.4f", vLow, vHigh)
	}
}

// 再加密网格时 TEC 变化小于容差：自适应必须真的停下来且稳定。
func TestRefinementBelowTolerance(t *testing.T) {
	p := baseParams(0)
	tol := 1e-7
	in := NewIntegrator(ground, top, tol, 22)
	_, panels, err := in.Integrate(p)
	if err != nil {
		t.Fatal(err)
	}
	vN := compositeSimpson(func(h float64) float64 { return p.DensityAt(h) }, ground, top, panels) / TECU
	v2N := compositeSimpson(func(h float64) float64 { return p.DensityAt(h) }, ground, top, panels*2) / TECU
	if change := math.Abs(v2N-vN) / vN; change >= tol {
		t.Fatalf("refinement change %.3e >= tolerance %.0e at %d panels", change, tol, panels)
	}
}
