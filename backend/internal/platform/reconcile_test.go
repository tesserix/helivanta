package platform_test

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/testinfra"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

type grantingModule struct{ name string }

func (g grantingModule) Name() string                     { return g.name }
func (g grantingModule) Migrations() []tenantdb.Migration { return nil }
func (g grantingModule) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: "x.thing.read", Roles: []authz.Role{authz.RoleNurse}},
		{Permission: "x.thing.write", Roles: []authz.Role{authz.RoleDoctor}},
	}
}
func (g grantingModule) Routes(*platform.Router, platform.Deps)      {}
func (g grantingModule) Consumers(platform.Deps) []events.Consumer   { return nil }
func (g grantingModule) Broadcasts(platform.Deps) []events.Broadcast { return nil }

// capturingWriter records both permission grants (module -> role) and
// role grants (tenant member -> role), so it can stand in for the real
// OpenFGA client across both reconciliation paths.
type capturingWriter struct {
	pairs       []string // "<permission>@<role>"
	roles       []string // "<tenantID>|<subject>|<role>"
	tenantRoles []string // "<tenantID>|<role>"

	// existing is what OpenFGA is pretending to already hold, and
	// deleted records every tuple Reconcile pruned from it, so the
	// delete half of reconciliation can be asserted without a container.
	existing map[string][]authz.Tuple
	deleted  []string // authz.Tuple.Key()
}

func (c *capturingWriter) ReadTuplesByTenant(context.Context) (map[string][]authz.Tuple, error) {
	return c.existing, nil
}

func (c *capturingWriter) DeleteTuple(_ context.Context, t authz.Tuple) error {
	c.deleted = append(c.deleted, t.Key())
	return nil
}

func (c *capturingWriter) GrantRole(_ context.Context, tenantID, subject string, r authz.Role) error {
	c.roles = append(c.roles, tenantID+"|"+subject+"|"+string(r))
	return nil
}

func (c *capturingWriter) RevokeRole(context.Context, string, string, authz.Role) error { return nil }

func (c *capturingWriter) GrantPermission(_ context.Context, tenantID string, p authz.Permission, r authz.Role) error {
	c.pairs = append(c.pairs, string(p)+"@"+string(r))
	return nil
}

func (c *capturingWriter) GrantTenantRole(_ context.Context, tenantID string, r authz.Role) error {
	c.tenantRoles = append(c.tenantRoles, tenantID+"|"+string(r))
	return nil
}


// TestGrantsForRejectsDuplicatePermission is the fix for the bug the
// approved-matrix oracle test in archtest/matrix_test.go could not see:
// two modules declaring a Grant for the same Permission used to produce
// two entries in GrantsFor's output, and any caller that folds that
// slice into a map keyed by Permission (as the oracle test's `actual`
// map does) would silently keep only the last one. GrantsFor must fail
// loudly instead, the same way Registry.Register fails loudly on a
// duplicate module name.
func TestGrantsForRejectsDuplicatePermission(t *testing.T) {
	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	require.NoError(t, reg.Register(duplicatingModule{"y"}))

	_, err := platform.GrantsFor(reg)
	require.Error(t, err)
	require.ErrorContains(t, err, "x.thing.read")
	require.ErrorContains(t, err, `"x"`)
	require.ErrorContains(t, err, `"y"`)
}

// duplicatingModule declares a Grant for a permission grantingModule
// already declares ("x.thing.read"), simulating a copy-paste mistake
// across two different modules.
type duplicatingModule struct{ name string }

func (d duplicatingModule) Name() string                     { return d.name }
func (d duplicatingModule) Migrations() []tenantdb.Migration { return nil }
func (d duplicatingModule) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: "x.thing.read", Roles: []authz.Role{authz.RolePharmacist}},
	}
}
func (d duplicatingModule) Routes(*platform.Router, platform.Deps)      {}
func (d duplicatingModule) Consumers(platform.Deps) []events.Consumer   { return nil }
func (d duplicatingModule) Broadcasts(platform.Deps) []events.Broadcast { return nil }

func TestTenantAdminReceivesEveryDeclaredPermission(t *testing.T) {
	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	w := &capturingWriter{}

	require.NoError(t, platform.ReconcileTenant(context.Background(), reg, w, "tenant-1"))

	sort.Strings(w.pairs)
	require.Equal(t, []string{
		"x.thing.read@nurse",
		"x.thing.read@tenant_admin",
		"x.thing.write@doctor",
		"x.thing.write@tenant_admin",
	}, w.pairs)
}

func TestReconcileIsIdempotent(t *testing.T) {
	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	w := &capturingWriter{}

	require.NoError(t, platform.ReconcileTenant(context.Background(), reg, w, "tenant-1"))
	first := len(w.pairs)
	require.NoError(t, platform.ReconcileTenant(context.Background(), reg, w, "tenant-1"))

	// Writes repeat, but GrantPermission is idempotent at the client, so
	// the operation stays safe to run on every boot.
	require.Equal(t, first*2, len(w.pairs))
}

// membersMigration creates a table shaped exactly like iam's iam_members
// (tenant_id, subject, role_key), without importing the iam module —
// platform cannot import iam (iam imports platform), so this test proves
// Reconcile's raw-SQL read against that schema using its own fixture,
// the same way reconcile.go itself never references iam's Go types.
var membersMigration = []tenantdb.Migration{{
	ID: "0001_test_iam_members",
	SQL: `
		CREATE TABLE iam_members (
		  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
		  tenant_id uuid NOT NULL,
		  subject text NOT NULL,
		  role_key text NOT NULL,
		  created_at timestamptz NOT NULL DEFAULT now()
		);
		ALTER TABLE iam_members ENABLE ROW LEVEL SECURITY;
		ALTER TABLE iam_members FORCE ROW LEVEL SECURITY;
		CREATE POLICY tenant_isolation ON iam_members
		  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
		  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);`,
}}

const (
	tenantA = "11111111-1111-1111-1111-111111111111"
	tenantB = "22222222-2222-2222-2222-222222222222"
)

func openMigratedDB(t *testing.T) *tenantdb.DB {
	t.Helper()
	appDSN, adminDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(), membersMigration))
	return db
}

func seedMember(t *testing.T, db *tenantdb.DB, tenantID, subject, roleKey string) {
	t.Helper()
	err := db.WithTenant(context.Background(), tenantID, func(tx *gorm.DB) error {
		return tx.Exec(
			`INSERT INTO iam_members (tenant_id, subject, role_key) VALUES (?, ?, ?)`,
			tenantID, subject, roleKey,
		).Error
	})
	require.NoError(t, err)
}

// TestReconcileReappliesEveryMembership is the amendment to the brief:
// Reconcile must re-apply member->role tuples (GrantRole), not just
// permission->role tuples (GrantPermission). Without this, wiping
// OpenFGA's tuples (routine in dev, which runs an in-memory store) and
// restarting would restore every permission grant but leave no user
// belonging to any role.
func TestReconcileReappliesEveryMembership(t *testing.T) {
	db := openMigratedDB(t)
	seedMember(t, db, tenantA, "dr-jane", "doctor")
	seedMember(t, db, tenantA, "nurse-amy", "nurse")
	seedMember(t, db, tenantB, "dr-bob", "doctor")

	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	w := &capturingWriter{}

	require.NoError(t, platform.Reconcile(context.Background(), reg, db, w))

	sort.Strings(w.roles)
	require.Equal(t, []string{
		tenantA + "|dr-jane|doctor",
		tenantA + "|nurse-amy|nurse",
		tenantB + "|dr-bob|doctor",
	}, w.roles)
}

// TestReconcileSkipsUnknownRoleKey proves the fix for the raw-SQL gap:
// iam_members rows are read straight from Postgres with no HTTP-path
// validation in front of them (the seed script, or a manual fixup,
// writes them directly), so a row whose role_key does not match one of
// the known authz.Role constants must not become a tuple write — it must
// be skipped and logged instead. Known-good rows in the same tenant, and
// in a different tenant, must still reconcile normally.
func TestReconcileSkipsUnknownRoleKey(t *testing.T) {
	db := openMigratedDB(t)
	seedMember(t, db, tenantA, "dr-jane", "doctor")
	seedMember(t, db, tenantA, "ghost", "not_a_real_role")
	seedMember(t, db, tenantB, "dr-bob", "doctor")

	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	w := &capturingWriter{}

	require.NoError(t, platform.Reconcile(context.Background(), reg, db, w))

	sort.Strings(w.roles)
	require.Equal(t, []string{
		tenantA + "|dr-jane|doctor",
		tenantB + "|dr-bob|doctor",
	}, w.roles, "the unknown role_key must not produce a GrantRole tuple, but known rows still must")
}

// TestReconcileAlsoReappliesPermissionsPerTenant proves the two passes
// compose: every tenant discovered via iam_members still gets its
// permission tuples reconciled, not only its membership tuples.
func TestReconcileAlsoReappliesPermissionsPerTenant(t *testing.T) {
	db := openMigratedDB(t)
	seedMember(t, db, tenantA, "dr-jane", "doctor")
	seedMember(t, db, tenantB, "dr-bob", "doctor")

	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	w := &capturingWriter{}

	require.NoError(t, platform.Reconcile(context.Background(), reg, db, w))

	// Each of the 2 tenants gets 4 permission tuples (2 perms x
	// nurse/doctor role, plus tenant_admin appended to each) reconciled
	// exactly once.
	require.Len(t, w.pairs, 8)
	require.Contains(t, w.pairs, "x.thing.read@nurse")
	require.Contains(t, w.pairs, "x.thing.write@doctor")
}
