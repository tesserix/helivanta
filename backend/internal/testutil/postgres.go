package testutil

import (
	"testing"

	"github.com/tesserix/hms/internal/containerhelpers"
)

// StartPostgres boots postgres:16, creates the non-BYPASSRLS app role,
// and returns (appDSN, adminDSN). Mirrors dev/init-db.sql.
func StartPostgres(t *testing.T) (string, string) {
	return containerhelpers.StartPostgres(t)
}
