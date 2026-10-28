package config

import (
	"errors"
	"net/url"
	"strings"
)

// RequirePrivacyWorker never falls back to application or migration credentials.
func (c Config) RequirePrivacyWorker() error {
	if strings.TrimSpace(c.PrivacyWorkerDatabaseURL) == "" || strings.TrimSpace(c.TemporalAddress) == "" || strings.TrimSpace(c.TemporalNamespace) == "" {
		return errors.New("privacy worker requires its database credential and Temporal address/namespace")
	}
	u, err := url.Parse(c.PrivacyWorkerDatabaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" || u.User == nil || u.User.Username() != "helivanta_privacy_worker" {
		return errors.New("privacy worker requires its dedicated PostgreSQL identity")
	}
	if c.Env == "production" && (u.Query().Get("sslmode") != "verify-full" || u.Query().Get("sslrootcert") == "") {
		return errors.New("privacy worker requires verified PostgreSQL TLS in production")
	}
	return nil
}

// RequirePrivacyDeadlines validates privacy deadline orchestration independently of other products.
func (c Config) RequirePrivacyDeadlines() error {
	if strings.TrimSpace(c.TemporalAddress) == "" || strings.TrimSpace(c.TemporalNamespace) == "" {
		return errors.New("privacy deadlines require a named Temporal namespace/address")
	}
	return nil
}
