// Package config holds the fixed and configurable service parameters.
package config

import (
	"os"
	"strconv"
	"time"
)

// Config is the resolved service configuration.
type Config struct {
	HTTPAddr string

	// GroundAltitude is the altitude of the ground datum in metres (zero).
	GroundAltitude float64
	// TopAltitude is the fixed integration ceiling in metres.
	TopAltitude float64
	// ProfileStep is the output profile sampling step in metres.
	ProfileStep float64
	// Tolerance is the relative convergence tolerance for TEC integration.
	Tolerance float64
	// TECU is the electrons-per-square-metre value of one TECU.
	TECU float64

	DatabaseURL string
	DBTimeout   time.Duration
}

// Default returns the default configuration (a noon F2-layer scale domain).
func Default() Config {
	return Config{
		HTTPAddr:       ":8080",
		GroundAltitude: 0.0,
		TopAltitude:   2000.0 * 1000.0, // 2000 km
		ProfileStep:    2.0 * 1000.0,    // 2 km
		Tolerance:      1e-6,
		TECU:           1e16,
		DatabaseURL:    "postgres://ionosphere:ionosphere@localhost:5432/ionosphere?sslmode=disable",
		DBTimeout:      10 * time.Second,
	}
}

// FromEnv overlays environment variables on top of the defaults.
func FromEnv() Config {
	c := Default()
	c.HTTPAddr = envStr("HTTP_ADDR", c.HTTPAddr)
	c.DatabaseURL = envStr("DATABASE_URL", c.DatabaseURL)
	c.GroundAltitude = envFloat("GROUND_ALTITUDE", c.GroundAltitude)
	c.TopAltitude = envFloat("TOP_ALTITUDE", c.TopAltitude)
	c.ProfileStep = envFloat("PROFILE_STEP", c.ProfileStep)
	c.Tolerance = envFloat("INTEGRATION_TOLERANCE", c.Tolerance)
	if v, ok := os.LookupEnv("DB_TIMEOUT"); ok {
		if d, err := time.ParseDuration(v); err == nil {
			c.DBTimeout = d
		}
	}
	return c
}

func envStr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
