package testutil

import (
	"testing"

	"github.com/tesserix/helivanta/internal/testinfra"
)

// StartPostgres boots postgres:16 and returns (appDSN, adminDSN,
// systemDSN). Mirrors PRODUCTION's roles — a non-superuser owner, a
// non-BYPASSRLS app role and a BYPASSRLS system role — not dev's (#894).
func StartPostgres(t *testing.T) (string, string, string) {
	return testinfra.StartPostgres(t)
}
