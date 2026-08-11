package platform

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/tenantdb"
)

// GrantsFor returns every module's declared grants with RoleTenantAdmin
// appended to each, which is why modules never list tenant_admin
// themselves.
func GrantsFor(reg *Registry) []authz.Grant {
	var out []authz.Grant
	for _, m := range reg.All() {
		for _, g := range m.Permissions() {
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
	return out
}

// ReconcileTenant makes the tenant's perm objects and system-role grants
// match the module registry. Idempotent, so deploying a new zone grants
// its permissions to every existing tenant on the next boot with no
// migration and no manual step.
//
// DB-free by design: it is called from the iam grant path with a DB
// transaction already in flight (see iam/sync.go), and must not open a
// second one.
func ReconcileTenant(ctx context.Context, reg *Registry, w TupleWriter, tenantID string) error {
	for _, g := range GrantsFor(reg) {
		for _, role := range g.Roles {
			if err := w.GrantPermission(ctx, tenantID, g.Permission, role); err != nil {
				return fmt.Errorf("grant %s to %s in %s: %w", g.Permission, role, tenantID, err)
			}
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

// Reconcile rebuilds OpenFGA's role and permission tuples for every
// tenant that has at least one membership row, from Postgres alone. This
// is what makes "Postgres is the system of record; OpenFGA is fully
// rebuildable from Postgres" true: on a fresh boot against a wiped or
// restarted (dev runs OpenFGA in-memory, so this is routine) store, this
// is the only thing that restores who belongs to what.
//
// This reads iam_members with WithAdmin, not WithSystem: iam_members is
// tenant-scoped and forced-RLS, and WithSystem's whole contract is that
// such a read comes back empty (see its doc). Enumerating every tenant's
// memberships in one pass is inherently a whole-system operation with no
// single tenant GUC to scope it by, so it needs the admin pool's
// deliberate RLS bypass — the same privileged class Migrate and LintRLS
// already use.
//
// A tenant with no members has nothing to authorize and needs no tuples;
// its first grant creates the row and the next boot reconciles it.
func Reconcile(ctx context.Context, reg *Registry, db *tenantdb.DB, w TupleWriter) error {
	var members []membership
	err := db.WithAdmin(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT tenant_id::text AS tenant_id, subject, role_key FROM iam_members`).
			Scan(&members).Error
	})
	if err != nil {
		return fmt.Errorf("list memberships: %w", err)
	}

	seenTenants := map[string]bool{}
	for _, m := range members {
		if !seenTenants[m.TenantID] {
			if err := ReconcileTenant(ctx, reg, w, m.TenantID); err != nil {
				return err
			}
			seenTenants[m.TenantID] = true
		}
		role := authz.Role(m.RoleKey)
		if err := w.GrantRole(ctx, m.TenantID, m.Subject, role); err != nil {
			return fmt.Errorf("grant role %s to %s in %s: %w", role, m.Subject, m.TenantID, err)
		}
	}
	return nil
}
