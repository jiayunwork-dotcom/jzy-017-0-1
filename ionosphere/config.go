package main

import (
	"log"
	"os"
	"strconv"
)

// Config holds the service configuration. The physical anchors (ground
// reference, integration top height, TECU conversion and integration
// tolerance) are echoed back by the /v1/config endpoint.
type Config struct {
	Port           int     // HTTP listen port
	GroundAltitude float64 // ground reference, m above sea level
	TopHeight      float64 // integration top height, m
	TECTolerance   float64 // relative refinement tolerance for the TEC integral
	ProfilePoints  int     // default number of profile samples
	MaxBatchSize   int     // maximum items per batch request
	DatabaseURL    string  // PostgreSQL DSN; empty means in-memory store
}

// Addr returns the listen address for the HTTP server.
func (c Config) Addr() string {
	return ":" + strconv.Itoa(c.Port)
}

// LoadConfig reads configuration from the environment, applying defaults.
func LoadConfig() Config {
	return Config{
		Port:           envInt("PORT", 8080),
		GroundAltitude: envFloat("GROUND_ALTITUDE_M", 0),
		TopHeight:      envFloat("TOP_HEIGHT_M", 1_000_000),
		TECTolerance:   envFloat("TEC_TOLERANCE", 1e-9),
		ProfilePoints:  envInt("PROFILE_POINTS", 400),
		MaxBatchSize:   envInt("MAX_BATCH_SIZE", 500),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
	}
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
		log.Printf("invalid %s=%q, using default %g", key, v, def)
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		log.Printf("invalid %s=%q, using default %d", key, v, def)
	}
	return def
}
