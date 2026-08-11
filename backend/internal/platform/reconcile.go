package platform

import (
	"context"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/tenantdb"
)

// TupleReconciler is everything Reconcile needs: the write half modules
// already share (TupleWriter), plus the read/delete half that only the
// reconciler is allowed to use. Deliberately not folded into TupleWriter
// — a module that could enumerate and delete tuples could revoke
// authorization it does not own, and TupleWriter is handed to every
// module through Deps.
type TupleReconciler interface {
	TupleWriter
	ReadTuplesByTenant(ctx context.Context) (map[string][]authz.Tuple, error)
	DeleteTuple(ctx context.Context, t authz.Tuple) error
}

// tupleSet is a desired-state set keyed by authz.Tuple.Key(). The nil
// set is usable and simply records nothing, so the grant helpers can be
// shared by callers that are building a desired state (Reconcile) and
// callers that are not (ReconcileTenant).
type tupleSet map[string]struct{}

func (s tupleSet) add(t authz.Tuple) {
	if s == nil {
		return
	}
	s[t.Key()] = struct{}{}
}

func (s tupleSet) has(t authz.Tuple) bool {
	_, ok := s[t.Key()]
	return ok
}

// roleTuple and permTuple mirror exactly what authz.Client.GrantRole and
// GrantPermission write. They must stay in lockstep with those two
// methods: a drift in relation name or object format would make every
// backed tuple look orphaned and get deleted on the next boot. The
// object strings come from authz's own constructors precisely so the
// format cannot drift here.
func roleTuple(tenantID, subject string, role authz.Role) authz.Tuple {
	return authz.Tuple{
		User: "user:" + subject, Relation: "assignee", Object: authz.RoleObject(tenantID, role),
	}
}

func permTuple(tenantID string, perm authz.Permission, role authz.Role) authz.Tuple {
	return authz.Tuple{
		User:     authz.RoleObject(tenantID, role),
		Relation: "granted_role",
		Object:   authz.PermObject(tenantID, perm),
	}
}

// GrantsFor returns every module's declared grants with RoleTenantAdmin
// appended to each, which is why modules never list tenant_admin
// themselves.
//
// It fails loudly — matching how Registry.Register treats a duplicate
// module name — if two different modules declare a Grant for the same
// Permission. Without this check the second declaration would silently
// shadow the first wherever callers build a map keyed by Permission (as
// the approved-matrix oracle test does), so a copy-pasted permission
// name would ship with a role set nobody reviewed.
func GrantsFor(reg *Registry) ([]authz.Grant, error) {
	var out []authz.Grant
	declaredBy := map[authz.Permission]string{}
	for _, m := range reg.All() {
		for _, g := range m.Permissions() {
			if owner, dup := declaredBy[g.Permission]; dup {
				return nil, fmt.Errorf("permission %q declared by both module %q and module %q",
					g.Permission, owner, m.Name())
			}
			declaredBy[g.Permission] = m.Name()

			// Copy before appending: g.Roles is the module's own slice,
			// returned fresh on every Permissions() call but potentially
			// backed by a shared array if a module ever memoizes it.
			// Appending in place would risk mutating that shared backing
			// array across reconciler runs.
			roles := append([]authz.Role(nil), g.Roles...)
			roles = append(roles, authz.RoleTenantAdmin)
			out = append(out, authz.Grant{Permission: g.Permission, Roles: roles})
		}
	}
	return out, nil
}

// ReconcileTenant makes the tenant's perm objects and system-role grants
// match the module registry. Idempotent, so deploying a new zone grants
// its permissions to every existing tenant on the next boot with no
// migration and no manual step.
//
// Additive by design, and unlike Reconcile it deletes nothing. It is
// called from the iam grant path with a DB transaction already in flight
// (see iam/sync.go) and must not open a second one, so it cannot read
// what Postgres backs and has no basis on which to delete anything.
// Convergence — deleting tuples Postgres no longer backs — is Reconcile's
// job, and runs on every boot of every replica.
func ReconcileTenant(ctx context.Context, reg *Registry, w TupleWriter, tenantID string) error {
	grants, err := GrantsFor(reg)
	if err != nil {
		return fmt.Errorf("reconcile tenant %s: %w", tenantID, err)
	}
	return grantPermissions(ctx, w, grants, tenantID, nil)
}

// grantPermissions writes every permission tuple the registry implies for
// one tenant, recording each in want (which may be nil).
func grantPermissions(ctx context.Context, w TupleWriter, grants []authz.Grant, tenantID string, want tupleSet) error {
	for _, g := range grants {
		for _, role := range g.Roles {
			if err := w.GrantPermission(ctx, tenantID, g.Permission, role); err != nil {
				return fmt.Errorf("grant %s to %s in %s: %w", g.Permission, role, tenantID, err)
			}
			want.add(permTuple(tenantID, g.Permission, role))
		}
	}
	return nil
}

// membership is one row of iam_members: a tenant's subject holding a
// role. Read via raw SQL rather than an iam model import — platform is
// not a module, so importing iam would not trip the module-isolation
// arch test, but it cannot happen anyway: iam imports platform, and the
// reverse would be a cycle. The coupling to iam's schema (table name and
// column names) is deliberate and accepted for that reason.
type membership struct {
	TenantID string
	Subject  string
	RoleKey  string
}

// Reconcile makes OpenFGA's role and permission tuples match Postgres
// exactly, for every tenant, in both directions: it writes the tuples
// Postgres backs and deletes the tuples it does not. This is what makes
// "Postgres is the system of record; OpenFGA is fully rebuildable from
// Postgres" true, and it is the only thing that closes the fail-open
// gap a dropped member_revoked event would otherwise leave — a grant
// that never lands fails closed, but a revoke that never lands fails
// OPEN and would keep granting access forever. It is also what lets a
// role's permission set shrink: without the delete pass, tightening a
// role in code has no effect on tenants already reconciled, and
// permission sets can only ever grow across deploys.
//
// Ordering is load-bearing. OpenFGA is read BEFORE Postgres, so the
// existing-tuple set can only ever be older than the membership rows it
// is compared against. A tuple written by another replica after this
// read simply is not in the set and is therefore never a delete
// candidate. The failure mode of a concurrent grant is thus "an orphan
// survives until the next boot", never "a live grant is deleted" — the
// only acceptable direction for an operation whose whole purpose is
// removing authorization.
//
// This reads iam_members with WithAdmin, not WithSystem: iam_members is
// tenant-scoped and forced-RLS, and WithSystem's whole contract is that
// such a read comes back empty (see its doc). Enumerating every tenant's
// memberships in one pass is inherently a whole-system operation with no
// single tenant GUC to scope it by, so it needs the admin pool's
// deliberate RLS bypass — the same privileged class Migrate and LintRLS
// already use.
//
// A tenant with no members has nothing to authorize: its desired tuple
// set is empty, so any tuple it still has in OpenFGA is pruned. That is
// the last-member-revoked case, and skipping it would leave exactly the
// stale grant this function exists to remove.
//
// A row whose role_key is not one of the known authz.Role constants is
// skipped (with a slog.Warn) rather than written as a tuple, and is
// likewise absent from the desired set — so a hand-written row for a
// bogus role neither creates a tuple nor protects one. The HTTP grant
// path (iam's knownRole check) rejects unknown roles before they ever
// reach Postgres, but this raw-SQL read has no such gate: a row written
// directly (the seed script, a manual fixup) is untrusted input exactly
// like an HTTP body.
//
// A global empty read is treated as a misconfiguration, not as "every
// tenant lost its last member". If iam_members comes back with zero rows
// at all while OpenFGA still holds tuples, Reconcile refuses to prune and
// returns an error instead — the same forced-RLS table that makes
// WithAdmin necessary here (see above) would also silently return zero
// rows, with no error, if ADMIN_DATABASE_URL were ever pointed at a role
// that does not bypass RLS. Without this guard that misconfiguration
// would read as "no tenant has any members" and Reconcile would delete
// every role:/perm: tuple in the store on that boot. A per-tenant zero
// (the legitimate last-member-revoked case) is unaffected: it only trips
// when the membership read is empty across ALL tenants and there is
// existing tuple data to lose.
func Reconcile(ctx context.Context, reg *Registry, db *tenantdb.DB, w TupleReconciler) error {
	existing, err := w.ReadTuplesByTenant(ctx)
	if err != nil {
		return fmt.Errorf("read existing tuples: %w", err)
	}

	var members []membership
	err = db.WithAdmin(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT tenant_id::text AS tenant_id, subject, role_key FROM iam_members`).
			Scan(&members).Error
	})
	if err != nil {
		return fmt.Errorf("list memberships: %w", err)
	}

	if n := existingTupleCount(existing); len(members) == 0 && n > 0 {
		slog.ErrorContext(ctx, "refusing to reconcile: zero memberships read from Postgres but existing tuples found, this looks like a misconfiguration",
			"existing_tuple_count", n, "existing_tenant_count", len(existing))
		return fmt.Errorf(
			"refusing to reconcile: zero memberships read from Postgres but %d existing tuples found across %d tenants, this looks like a misconfiguration (e.g. ADMIN_DATABASE_URL pointed at a role that does not bypass RLS)",
			n, len(existing))
	}

	desired, err := applyGrants(ctx, reg, w, members)
	if err != nil {
		return err
	}

	before := existingTupleCount(existing)
	slog.InfoContext(ctx, "reconcile: starting prune",
		"tenant_count", len(existing), "existing_tuple_count", before)

	deleted, err := prune(ctx, w, existing, desired)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "reconcile: prune complete",
		"tenant_count", len(existing), "existing_tuple_count", before, "deleted_count", deleted)
	return nil
}

// existingTupleCount sums the tuple counts across every tenant bucket in
// existing, so the global-empty-read guard and the summary log can both
// report a single total.
func existingTupleCount(existing map[string][]authz.Tuple) int {
	n := 0
	for _, tuples := range existing {
		n += len(tuples)
	}
	return n
}

// applyGrants writes every tuple Postgres backs and returns that same
// set, bucketed by tenant, for prune to compare against. Both halves are
// built in one pass so the written set and the kept set can never
// disagree.
func applyGrants(ctx context.Context, reg *Registry, w TupleWriter, members []membership) (map[string]tupleSet, error) {
	grants, err := GrantsFor(reg)
	if err != nil {
		return nil, fmt.Errorf("reconcile: %w", err)
	}
	desired := map[string]tupleSet{}
	for _, m := range members {
		if _, seen := desired[m.TenantID]; !seen {
			desired[m.TenantID] = tupleSet{}
			if err := grantPermissions(ctx, w, grants, m.TenantID, desired[m.TenantID]); err != nil {
				return nil, err
			}
		}
		role := authz.Role(m.RoleKey)
		if !authz.KnownRole(role) {
			slog.WarnContext(ctx, "skipping iam_members row with unknown role_key",
				"tenant_id", m.TenantID, "subject", m.Subject, "role_key", m.RoleKey)
			continue
		}
		if err := w.GrantRole(ctx, m.TenantID, m.Subject, role); err != nil {
			return nil, fmt.Errorf("grant role %s to %s in %s: %w", role, m.Subject, m.TenantID, err)
		}
		desired[m.TenantID].add(roleTuple(m.TenantID, m.Subject, role))
	}
	return desired, nil
}

// prune deletes every existing tuple its tenant's desired set does not
// contain. A tenant absent from desired has an empty (nil) set, so all
// of its tuples are pruned — that is the tenant whose last member was
// revoked.
//
// Every delete is guarded twice over: the candidate came from the bucket
// authz.ReadTuplesByTenant filed it under, and the tenant is re-derived
// from the object here and must match that bucket. A tuple whose object
// does not parse, or parses to a different tenant than the bucket it
// arrived in, is left alone rather than deleted — for an operation this
// destructive, an unexplained tuple is a reason to stop, not to guess.
func prune(ctx context.Context, w TupleReconciler, existing map[string][]authz.Tuple, desired map[string]tupleSet) (int, error) {
	deleted := 0
	for tenantID, tuples := range existing {
		want := desired[tenantID]
		for _, t := range tuples {
			owner, ok := authz.TenantOfObject(t.Object)
			if !ok || owner != tenantID {
				slog.WarnContext(ctx, "skipping tuple whose object does not belong to its tenant bucket",
					"tenant_id", tenantID, "object", t.Object)
				continue
			}
			if want.has(t) {
				continue
			}
			if err := w.DeleteTuple(ctx, t); err != nil {
				return deleted, fmt.Errorf("delete orphaned tuple %s#%s@%s in tenant %s: %w",
					t.Object, t.Relation, t.User, tenantID, err)
			}
			deleted++
			slog.WarnContext(ctx, "deleted authorization tuple no longer backed by postgres",
				"tenant_id", tenantID, "object", t.Object, "relation", t.Relation, "user", t.User)
		}
	}
	return deleted, nil
}
