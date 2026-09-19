package main

import "testing"

func TestParseValidParams(t *testing.T) {
	p, verr := ParseLayerParams(
		[]byte(`{"peakDensity":1.2e12,"peakHeight":300000,"scaleHeight":60000,"solarZenithAngle":20}`),
		testConfig())
	if verr != nil {
		t.Fatalf("unexpected error: %v", verr)
	}
	if p.PeakDensity != 1.2e12 || p.PeakHeight != 300000 || p.ScaleHeight != 60000 || p.SolarZenithAngle != 20 {
		t.Fatalf("parsed params wrong: %+v", p)
	}
}

func TestParseRejectsInvalidParams(t *testing.T) {
	cfg := testConfig()
	cases := []struct {
		name string
		body string
		want string // expected offending parameter
	}{
		{"not json", `{`, ""},
		{"missing peakDensity", `{"peakHeight":3e5,"scaleHeight":6e4,"solarZenithAngle":0}`, "peakDensity"},
		{"missing solarZenithAngle", `{"peakDensity":1e12,"peakHeight":3e5,"scaleHeight":6e4}`, "solarZenithAngle"},
		{"null field", `{"peakDensity":null,"peakHeight":3e5,"scaleHeight":6e4,"solarZenithAngle":0}`, "peakDensity"},
		{"non-numeric peakDensity", `{"peakDensity":"high","peakHeight":3e5,"scaleHeight":6e4,"solarZenithAngle":0}`, "peakDensity"},
		{"non-finite peakDensity", `{"peakDensity":1e999,"peakHeight":3e5,"scaleHeight":6e4,"solarZenithAngle":0}`, "peakDensity"},
		{"negative peakDensity", `{"peakDensity":-1,"peakHeight":3e5,"scaleHeight":6e4,"solarZenithAngle":0}`, "peakDensity"},
		{"zero peakDensity", `{"peakDensity":0,"peakHeight":3e5,"scaleHeight":6e4,"solarZenithAngle":0}`, "peakDensity"},
		{"zero scaleHeight", `{"peakDensity":1e12,"peakHeight":3e5,"scaleHeight":0,"solarZenithAngle":0}`, "scaleHeight"},
		{"negative scaleHeight", `{"peakDensity":1e12,"peakHeight":3e5,"scaleHeight":-6e4,"solarZenithAngle":0}`, "scaleHeight"},
		{"peak at ground", `{"peakDensity":1e12,"peakHeight":0,"scaleHeight":6e4,"solarZenithAngle":0}`, "peakHeight"},
		{"peak below ground", `{"peakDensity":1e12,"peakHeight":-1e5,"scaleHeight":6e4,"solarZenithAngle":0}`, "peakHeight"},
		{"peak above integration top", `{"peakDensity":1e12,"peakHeight":1.5e6,"scaleHeight":6e4,"solarZenithAngle":0}`, "peakHeight"},
		{"zenith 90 rejected", `{"peakDensity":1e12,"peakHeight":3e5,"scaleHeight":6e4,"solarZenithAngle":90}`, "solarZenithAngle"},
		{"zenith above 90 rejected", `{"peakDensity":1e12,"peakHeight":3e5,"scaleHeight":6e4,"solarZenithAngle":120}`, "solarZenithAngle"},
		{"negative zenith", `{"peakDensity":1e12,"peakHeight":3e5,"scaleHeight":6e4,"solarZenithAngle":-5}`, "solarZenithAngle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, verr := ParseLayerParams([]byte(tc.body), cfg)
			if verr == nil {
				t.Fatalf("expected a validation error, got none")
			}
			if verr.Parameter != tc.want {
				t.Fatalf("parameter = %q, want %q (message %q)", verr.Parameter, tc.want, verr.Message)
			}
		})
	}
}

// Just below 90 degrees is still a valid (if tenuous) daylight layer.
func TestZenithJustBelow90Accepted(t *testing.T) {
	_, verr := ParseLayerParams(
		[]byte(`{"peakDensity":1e12,"peakHeight":3e5,"scaleHeight":6e4,"solarZenithAngle":89.9}`),
		testConfig())
	if verr != nil {
		t.Fatalf("89.9 degrees should be accepted: %v", verr)
	}
}
