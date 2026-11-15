package config

import (
	"os"
	"strconv"
)

// Config holds the runtime settings read from the environment.
type Config struct {
	DatabaseURL  string
	Port         string
	MaxOpenConns int
}

// Load reads configuration from the environment, applying small-box defaults.
func Load() Config {
	return Config{
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		Port:         stringOr("PORT", "8080"),
		MaxOpenConns: intOr("DB_MAX_OPEN_CONNS", 8),
	}
}

func stringOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func intOr(key string, fallback int) int {
	if value, err := strconv.Atoi(os.Getenv(key)); err == nil && value > 0 {
		return value
	}
	return fallback
}
