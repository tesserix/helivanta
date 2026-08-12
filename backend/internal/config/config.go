package config

import "os"

type Config struct {
	Env              string
	Port             string
	AppDatabaseURL   string
	AdminDatabaseURL string
	NATSURL          string
	GIPProjectID     string
	OpenFGAURL       string
	OpenFGAStore     string
}

func Load() Config {
	return Config{
		Env:              getenv("HMS_ENV", "production"),
		Port:             getenv("PORT", "8080"),
		AppDatabaseURL:   getenv("APP_DATABASE_URL", "postgres://hms_app:hms_app@localhost:5432/hms?sslmode=disable"),
		AdminDatabaseURL: getenv("ADMIN_DATABASE_URL", "postgres://hms:hms@localhost:5432/hms?sslmode=disable"),
		NATSURL:          getenv("NATS_URL", "nats://localhost:4222"),
		GIPProjectID:     getenv("GIP_PROJECT_ID", "demo-hms"),
		OpenFGAURL:       getenv("OPENFGA_URL", "http://localhost:8090"),
		OpenFGAStore:     getenv("OPENFGA_STORE", "hms"),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// IsDev reports whether this process is running in a developer
// environment. It defaults to false: the guards that consult it disable
// production safety checks, so an unset or misspelled HMS_ENV must fail
// closed rather than silently unlock them.
func (c Config) IsDev() bool { return c.Env == "dev" }
