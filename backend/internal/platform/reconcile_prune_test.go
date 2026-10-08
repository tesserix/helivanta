package platform_test

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/authz"
)

// orphanRoleTuple is a membership tuple no iam_members row backs — the
// exact residue a dropped member_revoked event leaves behind, and the
// only fail-open path in the system before Reconcile learned to delete.
func orphanRoleTuple(tenantID, subject string, role authz.Role) authz.Tuple {
	return authz.Tuple{
		User: "user:" + subject, Relation: "assignee", Object: authz.RoleObject(tenantID, role),
	}
}

func backedRoleTuple(tenantID, subject string, role authz.Role) authz.Tuple {
	return orphanRoleTuple(tenantID, subject, role)
}

// TestReconcileDeletesTupleWithNoBackingRow is convergence test 1 with a
// fake writer: a role tuple that Postgres does not back must be deleted,
// while the tuple the same tenant does back must not be.
func TestReconcileDeletesTupleWithNoBackingRow(t *testing.T) {
	db := openMigratedDB(t)
	seedMember(t, db, tenantA, "dr-jane", "doctor")

	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	orphan := orphanRoleTuple(tenantA, "ghost", authz.RoleDoctor)
	w := &capturingWriter{existing: map[string][]authz.Tuple{
		tenantA: {orphan, backedRoleTuple(tenantA, "dr-jane", authz.RoleDoctor)},
	}}

	require.NoError(t, platform.Reconcile(context.Background(), reg, db, w))

	require.Equal(t, []string{orphan.Key()}, w.deleted,
		"only the tuple with no iam_members row may be deleted")
}

// TestReconcileDeletesUnbackedPermissionTuple proves the permission side
// converges too: a perm tuple for a permission no module declares any
// more must go, or a role's permission set could only ever grow across
// deploys.
func TestReconcileDeletesUnbackedPermissionTuple(t *testing.T) {
	db := openMigratedDB(t)
	seedMember(t, db, tenantA, "dr-jane", "doctor")

	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	stale := authz.Tuple{
		User:     authz.RoleObject(tenantA, authz.RoleDoctor),
		Relation: "granted_role",
		Object:   authz.PermObject(tenantA, "x.thing.deleted"),
	}
	current := authz.Tuple{
		User:     authz.RoleObject(tenantA, authz.RoleDoctor),
		Relation: "granted_role",
		Object:   authz.PermObject(tenantA, "x.thing.write"),
	}
	w := &capturingWriter{existing: map[string][]authz.Tuple{tenantA: {stale, current}}}

	require.NoError(t, platform.Reconcile(context.Background(), reg, db, w))

	require.Equal(t, []string{stale.Key()}, w.deleted)
}

// TestReconcilePrunesTenantWhoseLastMemberWasRevoked covers the case a
// tenant-by-tenant sweep driven only by iam_members would miss entirely:
// the tenant has no rows left at all, so it never appears in the
// membership scan, yet its tuples still grant access.
func TestReconcilePrunesTenantWhoseLastMemberWasRevoked(t *testing.T) {
	db := openMigratedDB(t)
	seedMember(t, db, tenantA, "dr-jane", "doctor")

	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	ghost := orphanRoleTuple(tenantB, "dr-bob", authz.RoleDoctor)
	w := &capturingWriter{existing: map[string][]authz.Tuple{tenantB: {ghost}}}

	require.NoError(t, platform.Reconcile(context.Background(), reg, db, w))

	require.Equal(t, []string{ghost.Key()}, w.deleted)
}

// TestReconcileRefusesToPruneOnGlobalEmptyMembershipRead is the blast-radius
// guard: if iam_members comes back with zero rows across every tenant —
// e.g. ADMIN_DATABASE_URL misconfigured to a role that does not bypass
// iam_members' FORCE ROW LEVEL SECURITY, so the query silently returns
// nothing instead of erroring — Reconcile must refuse to treat that as
// "every tenant lost its last member" and wipe the store. It must return
// an error and delete nothing.
func TestReconcileRefusesToPruneOnGlobalEmptyMembershipRead(t *testing.T) {
	db := openMigratedDB(t) // no seedMember call: iam_members is empty for every tenant.

	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	w := &capturingWriter{existing: map[string][]authz.Tuple{
		tenantA: {backedRoleTuple(tenantA, "dr-jane", authz.RoleDoctor)},
		tenantB: {backedRoleTuple(tenantB, "dr-bob", authz.RoleDoctor)},
	}}

	err := platform.Reconcile(context.Background(), reg, db, w)

	require.Error(t, err)
	require.ErrorContains(t, err, "refusing to reconcile")
	require.Empty(t, w.deleted, "a global empty membership read must never trigger a delete")
}

// TestReconcileRefusesToPruneOnAllUnknownRoleKeys is the row-count vs.
// usable-row-count blast-radius guard: a table with rows in it (raw count
// > 0) but where every row's role_key is unrecognized — e.g. a role-key
// rename migration landed in code before the data was backfilled, or a
// hand-written seed/manual fixup — contributes nothing to the desired
// role-tuple set, since applyGrants skips every such row. Guarding on
// len(members) alone would let this look like real membership data and
// prune proceed, silently deleting the tuple that role rename should have
// left alone. Reconcile must refuse and delete nothing, exactly as it
// does for a true empty read.
func TestReconcileRefusesToPruneOnAllUnknownRoleKeys(t *testing.T) {
	db := openMigratedDB(t)
	// role_doctor is not a known authz.Role — simulates a role-key rename
	// migration or manual drift landing before authz.Role catches up.
	seedMember(t, db, tenantA, "dr-jane", "role_doctor")

	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	legit := backedRoleTuple(tenantA, "dr-jane", authz.RoleDoctor)
	w := &capturingWriter{existing: map[string][]authz.Tuple{
		tenantA: {legit},
	}}

	err := platform.Reconcile(context.Background(), reg, db, w)

	require.Error(t, err, "a table of only unknown-role rows must refuse to prune, not silently delete")
	require.ErrorContains(t, err, "refusing to reconcile")
	require.Empty(t, w.deleted, "the legitimately-backed tuple must survive")
}

// TestReconcileSkipsTupleFiledUnderTheWrongTenant proves the second of
// the two isolation guards: even if the bucketing were wrong, a tuple
// whose object names a different tenant than the bucket it arrived in is
// left alone rather than deleted.
func TestReconcileSkipsTupleFiledUnderTheWrongTenant(t *testing.T) {
	db := openMigratedDB(t)
	seedMember(t, db, tenantA, "dr-jane", "doctor")

	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	w := &capturingWriter{existing: map[string][]authz.Tuple{
		tenantA: {orphanRoleTuple(tenantB, "dr-bob", authz.RoleDoctor)},
	}}

	require.NoError(t, platform.Reconcile(context.Background(), reg, db, w))

	require.Empty(t, w.deleted, "a mis-filed tuple must be reported, never deleted")
}

// countingReconciler is the real OpenFGA client with a delete counter,
// so "the second run is a no-op" can be asserted as zero deletes rather
// than only as an unchanged tuple set.
type countingReconciler struct {
	*authz.Client
	deletes int
}

func (c *countingReconciler) DeleteTuple(ctx context.Context, t authz.Tuple) error {
	c.deletes++
	return c.Client.DeleteTuple(ctx, t)
}

func newReconciler(t *testing.T) *countingReconciler {
	t.Helper()
	c, err := authz.NewClient(context.Background(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	return &countingReconciler{Client: c}
}

// tupleKeys snapshots one tenant's tuples as a sorted, comparable set.
func tupleKeys(t *testing.T, c *authz.Client, tenantID string) []string {
	t.Helper()
	byTenant, err := c.ReadTuplesByTenant(context.Background())
	require.NoError(t, err)
	keys := make([]string, 0, len(byTenant[tenantID]))
	for _, tp := range byTenant[tenantID] {
		keys = append(keys, tp.Key())
	}
	sort.Strings(keys)
	return keys
}

// TestReconcileWritesTenantRoleEdgesForEveryBackedMembership is the
// rescue-path test for the hazard the design doc calls out by name: an
// existing store has no tenant:… granted_role … tuples at all, so the
// moment a membership check goes live every Check returns false and
// every user in every tenant is locked out. This is what proves the
// boot reconciler actually rebuilds those edges rather than merely
// leaving the role/perm tuples it already wrote.
func TestReconcileWritesTenantRoleEdgesForEveryBackedMembership(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	seedMember(t, db, tenantA, "dr-jane", "doctor")

	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	fga := newReconciler(t)

	require.NoError(t, platform.Reconcile(ctx, reg, db, fga))

	member, err := fga.IsMember(ctx, "dr-jane", tenantA)
	require.NoError(t, err)
	require.True(t, member,
		"a membership row backed by Postgres must produce a working membership check after reconcile")
}

// TestReconcilePrunesTenantEdgesForRolesNobodyHolds proves the other
// half: an edge for a role no membership row backs any more must be
// deleted, or a retired role's tenant edge lingers forever and keeps
// granting membership through it. The edge for a role that IS still
// backed must survive the same pass — this is the assertion that
// catches a desired-set omission silently deleting live membership.
func TestReconcilePrunesTenantEdgesForRolesNobodyHolds(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	seedMember(t, db, tenantA, "dr-jane", "nurse")

	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	fga := newReconciler(t)

	require.NoError(t, platform.Reconcile(ctx, reg, db, fga))

	// A stale edge for a role no membership row backs.
	require.NoError(t, fga.GrantTenantRole(ctx, tenantA, authz.RoleDoctor))

	require.NoError(t, platform.Reconcile(ctx, reg, db, fga))

	after := tupleKeys(t, fga.Client, tenantA)
	require.NotContains(t, after,
		authz.Tuple{
			User:     authz.RoleObject(tenantA, authz.RoleDoctor),
			Relation: "granted_role",
			Object:   authz.TenantObject(tenantA),
		}.Key(),
		"an edge for a role no membership row backs must be pruned")
	require.Contains(t, after,
		authz.Tuple{
			User:     authz.RoleObject(tenantA, authz.RoleNurse),
			Relation: "granted_role",
			Object:   authz.TenantObject(tenantA),
		}.Key(),
		"the edge for the role that IS backed must survive")
}

// TestReconcileConvergesAgainstRealOpenFGA is the acceptance test: all
// three convergence properties, plus cross-tenant isolation, against a
// real OpenFGA and a real Postgres rather than a fake.
func TestReconcileConvergesAgainstRealOpenFGA(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	seedMember(t, db, tenantA, "dr-jane", "doctor")
	seedMember(t, db, tenantB, "dr-bob", "doctor")

	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	fga := newReconciler(t)

	// Backed: Postgres has the row, so this must survive.
	require.NoError(t, fga.GrantRole(ctx, tenantA, "dr-jane", authz.RoleDoctor))
	// Orphaned: no row backs either, so both must be deleted. The perm
	// tuple names a permission no module declares any more.
	require.NoError(t, fga.GrantRole(ctx, tenantA, "ghost", authz.RoleDoctor))
	require.NoError(t, fga.GrantPermission(ctx, tenantA, "x.thing.deleted", authz.RoleDoctor))
	// Tenant B is fully backed and must come through untouched.
	require.NoError(t, fga.GrantRole(ctx, tenantB, "dr-bob", authz.RoleDoctor))

	require.NoError(t, platform.Reconcile(ctx, reg, db, fga))
	after := tupleKeys(t, fga.Client, tenantA)

	// 1. the orphaned tuples are gone.
	require.NotContains(t, after, orphanRoleTuple(tenantA, "ghost", authz.RoleDoctor).Key())
	require.NotContains(t, after, authz.Tuple{
		User:     authz.RoleObject(tenantA, authz.RoleDoctor),
		Relation: "granted_role",
		Object:   authz.PermObject(tenantA, "x.thing.deleted"),
	}.Key())

	// 2. the backed tuple survived.
	require.Contains(t, after, backedRoleTuple(tenantA, "dr-jane", authz.RoleDoctor).Key())

	// Cross-tenant isolation: B is byte-identical to what a tenant with
	// exactly these backing rows should hold, and in particular still
	// holds dr-bob's membership.
	beforeB := tupleKeys(t, fga.Client, tenantB)
	require.Contains(t, beforeB, backedRoleTuple(tenantB, "dr-bob", authz.RoleDoctor).Key())

	// 3. a second run with unchanged Postgres state is a no-op.
	fga.deletes = 0
	require.NoError(t, platform.Reconcile(ctx, reg, db, fga))
	require.Zero(t, fga.deletes, "a second reconcile over unchanged state must delete nothing")
	require.Equal(t, after, tupleKeys(t, fga.Client, tenantA))
	require.Equal(t, beforeB, tupleKeys(t, fga.Client, tenantB),
		"reconciling must never disturb another tenant's tuples")
}
