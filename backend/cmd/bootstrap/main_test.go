package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/bootstrap"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const (
	tenantA = "11111111-1111-1111-1111-111111111111"
	subject = "281234567890123456"
)

// openMigratedDB brings up the REAL schema — the same migration set
// cmd/migrate applies — rather than a hand-written iam_members fixture.
// That is deliberate: the idempotency test below depends on iam's
// UNIQUE (tenant_id, subject, role_key), and the pool choice depends on
// the table's FORCE ROW LEVEL SECURITY. A fixture table would be a
// replica of those two facts, and a replica keeps passing after the real
// migration changes.
func openMigratedDB(t *testing.T) *tenantdb.DB {
	t.Helper()
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)

	registry, err := bootstrap.NewRegistry(nil, nil)
	require.NoError(t, err)
	migs := bootstrap.PlatformMigrations()
	for _, m := range registry.All() {
		migs = append(migs, m.Migrations()...)
	}
	require.NoError(t, db.Migrate(context.Background(), migs))
	return db
}

// memberRoles returns every role_key held by subject in tenantID, read on
// the SYSTEM pool so that a row which exists but is invisible to the app
// pool still shows up — a test that could not see a wrongly-written row
// would report "wrote nothing" for the exact bug it is guarding.
//
// It read on the admin pool until #894, which is the same mistake the
// production code made: the admin pool connects as the schema owner, and
// iam_members is FORCE ROW LEVEL SECURITY, so that read returned nothing
// once the harness stopped making the owner a superuser. The assertions
// below need a pool that genuinely crosses tenants, and only the system
// pool does.
func memberRoles(t *testing.T, db *tenantdb.DB, tenantID, subj string) []string {
	t.Helper()
	var roles []string
	err := db.WithAllTenants(context.Background(), func(tx *gorm.DB) error {
		return tx.Raw(
			`SELECT role_key FROM iam_members WHERE tenant_id = ?::uuid AND subject = ? ORDER BY role_key`,
			tenantID, subj).Scan(&roles).Error
	})
	require.NoError(t, err)
	return roles
}

func TestGrantWritesTheMembershipRow(t *testing.T) {
	db := openMigratedDB(t)

	inserted, err := grant(t.Context(), db, tenantA, subject, authz.RoleTenantAdmin)
	require.NoError(t, err)
	require.True(t, inserted, "a first grant must report that it inserted")
	require.Equal(t, []string{"tenant_admin"}, memberRoles(t, db, tenantA, subject))
}

// TestGrantRejectsUnknownRoleAndWritesNothing is the defect this program
// exists to prevent, proven rather than asserted. A role_key that is not
// a known authz.Role is skipped by platform.applyGrants with only a
// slog.Warn (internal/platform/reconcile.go), so the row would reconcile
// to no tuple at all: the operator gets an account that authenticates
// and then sees nothing, with no error anywhere on the path that created
// it. The row must therefore never be written in the first place, which
// is why this checks the table and not just the returned error.
func TestGrantRejectsUnknownRoleAndWritesNothing(t *testing.T) {
	db := openMigratedDB(t)

	inserted, err := grant(t.Context(), db, tenantA, subject, authz.Role("tenant_admn"))
	require.Error(t, err)
	require.False(t, inserted)
	require.ErrorContains(t, err, "tenant_admn")
	// The message must be actionable on its own: an operator who typo'd
	// the role needs the valid list without reading the source.
	require.ErrorContains(t, err, "tenant_admin")
	require.ErrorContains(t, err, "lab_tech")
	require.Empty(t, memberRoles(t, db, tenantA, subject),
		"an unknown role must leave NO row behind — a row that reconciles to nothing is the whole failure mode")
}

// TestGrantRejectsMalformedTenant guards the other unvalidatable input:
// there is no tenants table, so nothing downstream ever objects to a
// tenant_id — a mistyped one silently becomes a separate, empty tenant.
func TestGrantRejectsMalformedTenant(t *testing.T) {
	db := openMigratedDB(t)

	inserted, err := grant(t.Context(), db, "1111-1111", subject, authz.RoleTenantAdmin)
	require.Error(t, err)
	require.False(t, inserted)
	require.ErrorContains(t, err, "not a UUID")

	var count int64
	require.NoError(t, db.WithAllTenants(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM iam_members`).Scan(&count).Error
	}))
	require.Zero(t, count, "a malformed tenant must not reach the database at all")
}

// TestGrantIsIdempotentAndSaysSo covers the re-run an operator actually
// performs (a Job retried, a runbook step repeated). The second run must
// report that it changed nothing: printing "granted" for a no-op tells
// somebody their fix just landed when it landed on an earlier run.
func TestGrantIsIdempotentAndSaysSo(t *testing.T) {
	db := openMigratedDB(t)

	first, err := grant(t.Context(), db, tenantA, subject, authz.RoleTenantAdmin)
	require.NoError(t, err)
	require.True(t, first)

	second, err := grant(t.Context(), db, tenantA, subject, authz.RoleTenantAdmin)
	require.NoError(t, err)
	require.False(t, second, "a repeat grant must report that the row already existed")

	require.Equal(t, []string{"tenant_admin"}, memberRoles(t, db, tenantA, subject),
		"the repeat must not have duplicated the row")
}
