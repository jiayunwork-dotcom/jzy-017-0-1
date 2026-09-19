package main

import "math"

// LayerParams describes a single Chapman layer of the ionosphere.
//
//	PeakDensity      peak electron density NmF2, in m^-3 (must be > 0)
//	PeakHeight       height of the peak hmF2, in m above ground (must be above ground)
//	ScaleHeight      neutral scale height H, in m (must be > 0)
//	SolarZenithAngle solar zenith angle chi, in degrees, must be in [0, 90)
type LayerParams struct {
	PeakDensity      float64 `json:"peakDensity"`
	PeakHeight       float64 `json:"peakHeight"`
	ScaleHeight      float64 `json:"scaleHeight"`
	SolarZenithAngle float64 `json:"solarZenithAngle"`
}

// DemoParams returns the built-in demonstration case: a noon F2 layer
// (NmF2 = 1.2e12 m^-3, hmF2 = 300 km, H = 60 km, overhead sun).
func DemoParams() LayerParams {
	return LayerParams{
		PeakDensity:      1.2e12,
		PeakHeight:       300e3,
		ScaleHeight:      60e3,
		SolarZenithAngle: 0,
	}
}

// zenithFactor scales the peak density for solar zenith angle chi (degrees).
// The zenith angle enters the production rate through sec(chi), so the
// density at the peak is NmF2 * exp(0.5*(1 - sec(chi))).
func zenithFactor(chiDeg float64) float64 {
	sec := 1 / math.Cos(chiDeg*math.Pi/180)
	return math.Exp(0.5 * (1 - sec))
}

// PeakDensityAtPeak returns the electron density exactly at the peak height.
// At chi = 0 this equals PeakDensity; it decreases monotonically as chi
// grows towards 90 degrees. The peak *position* never moves with chi.
func (p LayerParams) PeakDensityAtPeak() float64 {
	return p.PeakDensity * zenithFactor(p.SolarZenithAngle)
}

// DensityAt returns the electron density (m^-3) at geometric height h (m).
//
// With the dimensionless height z = (h - hmF2)/H the classic Chapman layer
// for an overhead sun is
//
//	N(h) = Nmax * exp( 0.5 * (1 - z - exp(-z)) )
//
// whose maximum sits exactly at z = 0, i.e. at h = hmF2. The zenith angle
// only scales the magnitude (through zenithFactor), never the position.
func (p LayerParams) DensityAt(h float64) float64 {
	z := (h - p.PeakHeight) / p.ScaleHeight
	shape := math.Exp(0.5 * (1 - z - math.Exp(-z)))
	return p.PeakDensityAtPeak() * shape
}
