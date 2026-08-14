package config

import (
	"log/slog"
	"os"
	"strconv"
)

type Config struct {
	Env              string
	Port             string
	LogLevel         string
	AppDatabaseURL   string
	AdminDatabaseURL string
	NATSURL          string
	GIPProjectID     string
	OpenFGAURL       string
	OpenFGAStore     string

	// Rate limits are env-configurable, unlike the pagination page-size
	// constants: a page size bounds a query, but a rate limit bounds
	// capacity, and capacity genuinely differs between a laptop running
	// the e2e suite and a hospital in production.
	RateLimitTenantPerMin    int
	RateLimitPrincipalPerMin int
	RateLimitMintPerMin      int
}

func Load() Config {
	return Config{
		Env:              getenv("HMS_ENV", "production"),
		Port:             getenv("PORT", "8080"),
		LogLevel:         getenv("LOG_LEVEL", "info"),
		AppDatabaseURL:   getenv("APP_DATABASE_URL", "postgres://hms_app:hms_app@localhost:5432/hms?sslmode=disable"),
		AdminDatabaseURL: getenv("ADMIN_DATABASE_URL", "postgres://hms:hms@localhost:5432/hms?sslmode=disable"),
		NATSURL:          getenv("NATS_URL", "nats://localhost:4222"),
		GIPProjectID:     getenv("GIP_PROJECT_ID", "demo-hms"),
		OpenFGAURL:       getenv("OPENFGA_URL", "http://localhost:8090"),
		OpenFGAStore:     getenv("OPENFGA_STORE", "hms"),

		RateLimitTenantPerMin:    getenvInt("RATE_LIMIT_TENANT_PER_MIN", 600),
		RateLimitPrincipalPerMin: getenvInt("RATE_LIMIT_PRINCIPAL_PER_MIN", 120),
		RateLimitMintPerMin:      getenvInt("RATE_LIMIT_MINT_PER_MIN", 10),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// getenvInt returns def when k is unset OR unparseable. A rate limit is a
// capacity control, not a data or identity control: per
// docs/standards/engineering-principles.md §3, capacity controls fail
// OPEN, not closed. A mistyped RATE_LIMIT_* value must not be able to
// stop a hospital's API from booting — the same direction LOG_LEVEL
// already takes in pkg/logging.NewWithWriter for the same reason. The
// fallback is logged so the mistype is visible rather than silently
// eaten.
func getenvInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("invalid integer env var; using default", "key", k, "value", v, "default", def)
		return def
	}
	return n
}

// IsDev reports whether this process is running in a developer
// environment. It defaults to false: the guards that consult it disable
// production safety checks, so an unset or misspelled HMS_ENV must fail
// closed rather than silently unlock them.
func (c Config) IsDev() bool { return c.Env == "dev" }
