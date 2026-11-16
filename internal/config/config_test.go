package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/api_test")
	t.Setenv("PORT", "")
	t.Setenv("DB_MAX_OPEN_CONNS", "")

	cfg := Load()
	if cfg.DatabaseURL != "postgres://localhost/api_test" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.Port != "8080" {
		t.Fatalf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.MaxOpenConns != 8 {
		t.Fatalf("MaxOpenConns = %d, want 8", cfg.MaxOpenConns)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://db/api_test")
	t.Setenv("PORT", "9090")
	t.Setenv("DB_MAX_OPEN_CONNS", "3")

	cfg := Load()
	if cfg.Port != "9090" {
		t.Fatalf("Port = %q, want 9090", cfg.Port)
	}
	if cfg.MaxOpenConns != 3 {
		t.Fatalf("MaxOpenConns = %d, want 3", cfg.MaxOpenConns)
	}
}

func TestLoadIgnoresInvalidPoolSize(t *testing.T) {
	for _, value := range []string{"abc", "0", "-4", " "} {
		t.Setenv("DB_MAX_OPEN_CONNS", value)
		if got := Load().MaxOpenConns; got != 8 {
			t.Fatalf("DB_MAX_OPEN_CONNS=%q gave %d, want fallback 8", value, got)
		}
	}
}
