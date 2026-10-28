package config

import (
	"errors"
	"net/url"
	"strings"
)

// RequireEmergencyWorker never falls back to application or migration credentials.
func (c Config) RequireEmergencyWorker() error {
	if strings.TrimSpace(c.EmergencyWorkerDatabaseURL) == "" || strings.TrimSpace(c.TemporalAddress) == "" || strings.TrimSpace(c.TemporalNamespace) == "" {
		return errors.New("emergency worker requires its database credential and Temporal address/namespace")
	}
	u, err := url.Parse(c.EmergencyWorkerDatabaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" || u.User == nil || u.User.Username() != "helivanta_emergency_worker" {
		return errors.New("emergency worker requires its dedicated PostgreSQL identity")
	}
	if c.Env == "production" && (u.Query().Get("sslmode") != "verify-full" || u.Query().Get("sslrootcert") == "") {
		return errors.New("emergency worker requires verified PostgreSQL TLS in production")
	}
	return nil
}

// RequireEmergencyWorkflows validates dispatch orchestration independently of file storage.
func (c Config) RequireEmergencyWorkflows() error {
	if strings.TrimSpace(c.TemporalAddress) == "" || strings.TrimSpace(c.TemporalNamespace) == "" {
		return errors.New("emergency dispatch requires a named Temporal namespace/address")
	}
	return nil
}
