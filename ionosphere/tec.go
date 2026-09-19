package main

import "math"

// TECU is one TEC unit: 10^16 electrons per square metre.
const TECU = 1e16

// maxRefinements bounds the number of trapezoid doublings in IntegrateTEC.
const maxRefinements = 22

// IntegrateTEC numerically integrates an electron-density profile f(h)
// (m^-3) over geometric height from a to b (m) and returns the vertical
// total electron content in m^-2, the number of trapezoid intervals used,
// and whether the refinement converged.
//
// It uses the trapezoidal rule with successive interval doubling: each
// refinement reuses the previous sum and only evaluates the new midpoints.
// Integration stops as soon as two successive estimates differ by less than
// tol (relative to the estimate), i.e. further refinement would change the
// TEC by less than the pinned tolerance.
func IntegrateTEC(f func(h float64) float64, a, b, tol float64, initialIntervals int) (tec float64, intervals int, converged bool) {
	n := initialIntervals
	if n < 16 {
		n = 16
	}
	h := (b - a) / float64(n)
	sum := 0.5 * (f(a) + f(b))
	for i := 1; i < n; i++ {
		sum += f(a + float64(i)*h)
	}
	prev := sum * h
	for r := 0; r < maxRefinements; r++ {
		// Evaluate f only at the new midpoints of the current intervals.
		var mid float64
		for i := 0; i < n; i++ {
			mid += f(a + (float64(i)+0.5)*h)
		}
		h *= 0.5
		n *= 2
		cur := 0.5*prev + mid*h
		if math.Abs(cur-prev) <= tol*math.Max(math.Abs(cur), 1) {
			return cur, n, true
		}
		prev = cur
	}
	return prev, n, false
}

// InitialIntervals picks a starting interval count for IntegrateTEC that
// resolves the layer: at least 512 intervals, and enough to put at least 4
// grid points inside one scale height (capped at 2^22).
func InitialIntervals(from, to, scaleHeight float64) int {
	n := 512
	if scaleHeight > 0 {
		need := 4 * (to - from) / scaleHeight
		for float64(n) < need && n < 1<<22 {
			n *= 2
		}
	}
	return n
}

// ToTECU converts a column content in m^-2 to TEC units.
func ToTECU(tecM2 float64) float64 {
	return tecM2 / TECU
}
