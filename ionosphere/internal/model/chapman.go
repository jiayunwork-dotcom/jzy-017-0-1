// Package model implements the Chapman layer model for the electron density
// profile and the reduction of the peak density with solar zenith angle.
//
// All heights are altitudes above the ground datum in metres. The ground datum
// is fixed at zero metres. The dimensionless height is
//
//	z = (h - hm) / H
//
// where h is geometric altitude, hm the peak height at zenith and H the scale
// height.
//
// For a normal-incidence (solar zenith angle chi = 0) Chapman layer the
// electron density is proportional to
//
//	exp( 1/2 * (1 - z - exp(-z)) )
//
// The zenith angle enters the production rate through sec(chi), so the peak
// density is the supplied peak density Nm multiplied by
//
//	exp( 1/2 * (1 - sec chi) )
//
// The shape function exp(0.5*(1 - z - e^-z)) has its unique global maximum of
// one exactly at z = 0, so the layer maximum is pinned at h = hm for every
// zenith angle; changing chi changes only the peak amplitude, never the peak
// location.
package model

import (
	"math"
	"sort"
)

// Params holds one Chapman layer specification.
//
// Nm is the peak electron density at chi = 0 (electrons per cubic metre).
// Hm is the peak altitude above ground (metres).
// H is the scale height (metres, strictly positive).
// ChiDeg is the solar zenith angle in degrees.
type Params struct {
	Nm     float64 `json:"peak_density"`
	Hm     float64 `json:"peak_height"`
	H      float64 `json:"scale_height"`
	ChiDeg float64 `json:"zenith_angle"`
}

// Geometry fixes the vertical interval over which a layer is evaluated.
type Geometry struct {
	// GroundAltitude is the altitude of the ground datum in metres (fixed 0).
	GroundAltitude float64
	// TopAltitude is the service-wide integration ceiling in metres.
	TopAltitude float64
}

// ProfileSample is one point of the returned electron density profile.
type ProfileSample struct {
	Altitude float64 `json:"altitude"`
	Density  float64 `json:"density"`
}

// Profile is an electron density profile sampled on an ordered altitude grid.
type Profile struct {
	// PeakDensity is the layer maximum Nm * exp(0.5*(1-sec chi)).
	PeakDensity float64         `json:"peak_density"`
	PeakHeight  float64         `json:"peak_height"`
	Samples     []ProfileSample `json:"samples"`
}

// SecZenith returns sec(chi) for a solar zenith angle given in degrees.
// It is the pure geometric factor used by the production rate; parameter
// range checks (chi < 90 deg) live in the validate package.
func SecZenith(chiDeg float64) float64 {
	cosChi := math.Cos(chiDeg * math.Pi / 180.0)
	return 1.0 / cosChi
}

// PeakFactor returns exp(0.5*(1 - sec chi)), the factor by which the peak
// density is reduced relative to the chi = 0 value. It is strictly
// decreasing in chi on [0, 90 deg) and equals one at chi = 0.
func PeakFactor(chiDeg float64) float64 {
	return math.Exp(0.5 * (1.0 - SecZenith(chiDeg)))
}

// PeakDensity returns the electron density at the layer peak:
// Nm * exp(0.5*(1 - sec chi)). At chi = 0 this is exactly Nm.
func (p Params) PeakDensity() float64 {
	return p.Nm * PeakFactor(p.ChiDeg)
}

// DensityAt evaluates the Chapman electron density at altitude h (metres),
// in electrons per cubic metre:
//
//	N(h) = Nm * exp(0.5*(1-sec chi)) * exp(0.5*(1 - z - exp(-z)))
//	     = Nm * exp(0.5*(2 - sec chi - z - exp(-z)))
func (p Params) DensityAt(h float64) float64 {
	z := (h - p.Hm) / p.H
	exponent := 0.5 * (1.0 - z - math.Exp(-z))
	// Guard the deep-under-peak tail against any intermediate Inf*0 artefact:
	// far below the layer the density is physically zero.
	if exponent < -700 {
		return 0
	}
	return p.Nm * PeakFactor(p.ChiDeg) * math.Exp(exponent)
}

// BuildProfile samples the layer on a regular altitude grid from the ground
// datum to the geometry top. The peak altitude hm is always one of the grid
// points (so the returned samples literally contain the layer maximum), and
// the grid is sorted and de-duplicated.
func (p Params) BuildProfile(g Geometry, step float64) Profile {
	if step <= 0 {
		step = 1
	}
	heights := make([]float64, 0, int((g.TopAltitude-g.GroundAltitude)/step)+4)
	for h := g.GroundAltitude; h < g.TopAltitude; h += step {
		heights = append(heights, h)
	}
	heights = append(heights, g.TopAltitude)
	heights = append(heights, p.Hm)

	sort.Float64s(heights)
	unique := heights[:0]
	for _, h := range heights {
		if len(unique) == 0 || h-unique[len(unique)-1] > math.SmallestNonzeroFloat64 {
			unique = append(unique, h)
		}
	}

	samples := make([]ProfileSample, len(unique))
	for i, h := range unique {
		samples[i] = ProfileSample{Altitude: h, Density: p.DensityAt(h)}
	}
	return Profile{
		PeakDensity: p.PeakDensity(),
		PeakHeight:  p.Hm,
		Samples:     samples,
	}
}
