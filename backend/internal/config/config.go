package config

import "os"

type Config struct {
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
