package testinfra

import (
	"net/url"
	"strings"
	"testing"
)

// StartStaffIdentityOwnerPostgres uses the existing CI-only server and a separate
// test database. It adds no service or privileged role to application migrations.
func StartStaffIdentityOwnerPostgres(t *testing.T) string {
	t.Helper()
	_, admin, _ := StartPostgres(t)
	parsed, err := url.Parse(admin)
	if err != nil {
		t.Fatal("identity owner test database unavailable")
	}
	sql := "DO $$ BEGIN PERFORM pg_advisory_xact_lock(hashtextextended('staff-identity-owner-fixture-role',0)); IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='onboarding') THEN CREATE ROLE onboarding NOLOGIN; END IF; END $$; GRANT onboarding TO hms_owner;"
	if err = runSQL(t.Context(), strings.TrimPrefix(parsed.Path, "/"), sql); err != nil {
		t.Fatal(err)
	}
	return admin
}
