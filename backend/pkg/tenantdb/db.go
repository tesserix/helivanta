package tenantdb

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var ErrInvalidTenant = errors.New("tenantdb: tenant id is not a valid uuid")

// DB owns two pools: app (non-BYPASSRLS role, all runtime access) and
// admin (the migration role, used for migrations and other privileged
// boot-time/ops operations — see Migrate, LintRLS, WithAdmin). There is
// no exported raw *gorm.DB.
type DB struct {
	app   *gorm.DB
	admin *gorm.DB
}

func Open(appDSN, adminDSN string) (*DB, error) {
	open := func(dsn string) (*gorm.DB, error) {
		return gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	}
	app, err := open(appDSN)
	if err != nil {
		return nil, fmt.Errorf("open app pool: %w", err)
	}
	admin, err := open(adminDSN)
	if err != nil {
		return nil, fmt.Errorf("open admin pool: %w", err)
	}
	for _, g := range []*gorm.DB{app, admin} {
		sqlDB, err := g.DB()
		if err != nil {
			return nil, err
		}
		sqlDB.SetMaxOpenConns(5)
		sqlDB.SetMaxIdleConns(2)
	}
	if err := assertNoRLSBypass(app); err != nil {
		return nil, err
	}
	return &DB{app: app, admin: admin}, nil
}

// assertNoRLSBypass fails when the pool's role can see through row-level
// security. FORCE ROW LEVEL SECURITY already covers the table-owner case;
// this covers BYPASSRLS and superuser, and together they close both.
//
// Without it, an APP_DATABASE_URL naming the admin role serves every
// tenant's data from every endpoint, while every test passes and LintRLS
// stays green — it inspects table metadata, not the connecting role.
func assertNoRLSBypass(g *gorm.DB) error {
	var caps struct {
		Bypass bool
		Super  bool
	}
	result := g.Raw(`SELECT rolbypassrls AS bypass, rolsuper AS super
		FROM pg_roles WHERE rolname = current_user`).Scan(&caps)
	if result.Error != nil {
		return fmt.Errorf("probe app role capabilities: %w", result.Error)
	}
	return evaluateRLSCaps(caps.Bypass, caps.Super, result.RowsAffected)
}

// evaluateRLSCaps turns a pg_roles probe into a pass/fail decision. It is
// split out from assertNoRLSBypass so the zero-row case — current_user
// matching no row in pg_roles, which Scan does not itself treat as an
// error — can be unit-tested without a real database. A guard that
// silently passes when it couldn't evaluate its own precondition is worse
// than no guard, so rowsAffected == 0 fails closed rather than falling
// through to the zero-value "safe" caps.
func evaluateRLSCaps(bypass, super bool, rowsAffected int64) error {
	if rowsAffected == 0 {
		return fmt.Errorf("tenantdb: could not determine app role capabilities " +
			"(current_user matched no row in pg_roles); refusing to assume it cannot " +
			"bypass row-level security")
	}
	if bypass || super {
		return fmt.Errorf("tenantdb: APP_DATABASE_URL connects as a role that can bypass "+
			"row-level security (bypassrls=%v superuser=%v); tenant isolation would be "+
			"silently disabled. Point it at the non-superuser application role", bypass, super)
	}
	return nil
}

func (d *DB) PingContext(ctx context.Context) error {
	sqlDB, err := d.app.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// Migrate applies migrations in order, once each, on the admin pool.
func (d *DB) Migrate(ctx context.Context, migs []Migration) error {
	if err := d.admin.WithContext(ctx).Exec(
		`CREATE TABLE IF NOT EXISTS schema_migrations (id text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`,
	).Error; err != nil {
		return err
	}
	for _, m := range migs {
		err := d.admin.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			res := tx.Exec(`INSERT INTO schema_migrations (id) VALUES (?) ON CONFLICT DO NOTHING`, m.ID)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return nil // already applied
			}
			return tx.Exec(m.SQL).Error
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", m.ID, err)
		}
	}
	return nil
}

// WithTenant is the single runtime data path. The tenant GUC is set
// with set_config(..., true) so it is transaction-local; an unset GUC
// makes every RLS policy evaluate NULL → zero rows (issue #2).
func (d *DB) WithTenant(ctx context.Context, tenantID string, fn func(tx *gorm.DB) error) error {
	if _, err := uuid.Parse(tenantID); err != nil {
		return ErrInvalidTenant
	}
	return d.app.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.tenant_id', ?, true)`, tenantID).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

// WithSystem runs fn in a transaction on the app pool WITHOUT a tenant
// GUC. Only for platform tables that have no tenant_id column (outbox,
// idempotency); RLS still hides every tenant-scoped table because the
// GUC is unset. WithSystem is deliberately not an escape hatch for
// tenant data — see WithAdmin for the narrow, explicitly-privileged
// alternative when a whole-system read genuinely needs to cross tenant
// boundaries.
func (d *DB) WithSystem(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return d.app.WithContext(ctx).Transaction(fn)
}

// WithAdmin runs fn in a transaction on the admin pool, which connects
// as the migration role and therefore bypasses RLS entirely — it sees
// every tenant's rows in every table. This is the same privileged class
// of operation Migrate and LintRLS already are (both also run on the
// admin pool outside WithTenant/WithSystem); WithAdmin exists so a third
// kind of whole-system, boot-time operation — enumerating tenants and
// memberships across the fleet, which by definition cannot be scoped to
// one tenant's GUC — doesn't have to either weaken WithSystem's
// documented guarantee or reach around DB entirely. It is for
// process-startup/ops code (the reconciler), never for request-path
// handlers; nothing in the type system enforces that boundary, the same
// as Migrate and LintRLS today.
func (d *DB) WithAdmin(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return d.admin.WithContext(ctx).Transaction(fn)
}

// lintAllowlist names the tables that legitimately carry no tenant_id.
//
// outbox_events is here under protest: it holds event payloads that today
// include patient names, outside any RLS boundary, readable by any
// WithSystem transaction. Giving it a tenant_id changes the dispatcher's
// access path, so it is tracked separately on #774 rather than fixed here.
var lintAllowlist = map[string]bool{
	"schema_migrations": true,
	"outbox_events":     true,
	"processed_events":  true,
}

// LintRLS returns every table that is not properly tenant-isolated, each
// as "<table>: <reason>".
//
// It enumerates all tables and subtracts an allowlist, rather than
// inspecting only tables that already have a tenant_id. The old direction
// protected the tables someone remembered to mark; this one protects
// everything, and a module that simply forgets the column now fails.
func (d *DB) LintRLS(ctx context.Context) ([]string, error) {
	type row struct {
		Relname        string
		IsView         bool
		HasTenant      bool
		RLS            bool
		Forced         bool
		PolicyCount    int
		HasPolicy      bool
		UsesFunction   bool
		CheckUsesFunc  bool
		BadRelkind     bool
		BadSchema      bool
		NonInvokerView bool
	}
	var rows []row
	// has_policy/uses_function/check_uses_func are all EXISTS-ed against the
	// SAME policy row (p.policyname carried through every clause) rather than
	// three independent EXISTS subqueries — independent EXISTS clauses could
	// each be satisfied by a different policy, and RLS permissive policies OR
	// together, so a second policy on the table (e.g. USING (true)) would
	// defeat isolation while every clause here still individually passed.
	// policy_count catches that same hole from the other side: exactly one
	// policy per tenant table is what every production table has today, and
	// a second policy — permissive or not — is a red flag regardless of what
	// it says, so it fails the lint even before its contents are examined.
	err := d.admin.WithContext(ctx).Raw(`
		SELECT c.relname,
		       (c.relkind = 'v') AS is_view,
		       EXISTS (SELECT 1 FROM information_schema.columns col
		               WHERE col.table_schema = n.nspname
		                 AND col.table_name = c.relname
		                 AND col.column_name = 'tenant_id') AS has_tenant,
		       c.relrowsecurity        AS rls,
		       c.relforcerowsecurity   AS forced,
		       (SELECT COUNT(*) FROM pg_policies p
		               WHERE p.schemaname = n.nspname AND p.tablename = c.relname) AS policy_count,
		       EXISTS (SELECT 1 FROM pg_policies p
		               WHERE p.schemaname = n.nspname AND p.tablename = c.relname
		                 AND p.qual IS NOT NULL AND p.with_check IS NOT NULL) AS has_policy,
		       EXISTS (SELECT 1 FROM pg_policies p
		               WHERE p.schemaname = n.nspname AND p.tablename = c.relname
		                 AND p.qual IS NOT NULL AND p.with_check IS NOT NULL
		                 AND p.qual LIKE '%hms_tenant_visible%') AS uses_function,
		       EXISTS (SELECT 1 FROM pg_policies p
		               WHERE p.schemaname = n.nspname AND p.tablename = c.relname
		                 AND p.qual IS NOT NULL AND p.with_check IS NOT NULL
		                 AND p.qual LIKE '%hms_tenant_visible%'
		                 AND p.with_check LIKE '%hms_tenant_visible%') AS check_uses_func,
		       (c.relkind IN ('p', 'f')
		         AND EXISTS (SELECT 1 FROM information_schema.columns col
		                     WHERE col.table_schema = n.nspname
		                       AND col.table_name = c.relname
		                       AND col.column_name = 'tenant_id')) AS bad_relkind,
		       (n.nspname NOT IN ('public', 'pg_catalog', 'information_schema')
		         AND n.nspname NOT LIKE 'pg_%'
		         AND EXISTS (SELECT 1 FROM information_schema.columns col
		                     WHERE col.table_schema = n.nspname
		                       AND col.table_name = c.relname
		                       AND col.column_name = 'tenant_id')) AS bad_schema,
		       (c.relkind = 'v'
		         AND EXISTS (SELECT 1 FROM information_schema.columns col
		                     WHERE col.table_schema = n.nspname
		                       AND col.table_name = c.relname
		                       AND col.column_name = 'tenant_id')
		         AND COALESCE((SELECT option_value::boolean FROM pg_options_to_table(c.reloptions)
		                       WHERE option_name = 'security_invoker'), false) IS NOT TRUE) AS non_invoker_view
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'p', 'f', 'v')
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		  AND n.nspname NOT LIKE 'pg_%'
		ORDER BY c.relname`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	var bad []string
	for _, r := range rows {
		if lintAllowlist[r.Relname] {
			continue
		}
		// Views never carry relrowsecurity/relforcerowsecurity — Postgres
		// has no such concept for a view, they read as false unconditionally
		// — so a view must never fall into the ordinary table switch below:
		// a compliant `security_invoker = true` view over a tenant table
		// would hit `!r.RLS || !r.Forced` and be reported as a false
		// positive. A view's only obligation is security_invoker, checked
		// here in its own branch with its own reason string.
		if r.IsView {
			if r.NonInvokerView {
				bad = append(bad, r.Relname+": view over a tenant table is not security_invoker")
			}
			continue
		}
		switch {
		case r.BadSchema:
			bad = append(bad, r.Relname+": tenant table lives outside the public schema")
		case r.BadRelkind:
			bad = append(bad, r.Relname+": tenant table is a partitioned parent or foreign table, not a plain table")
		case !r.HasTenant:
			bad = append(bad, r.Relname+": no tenant_id column")
		case !r.RLS || !r.Forced:
			bad = append(bad, r.Relname+": row-level security not enabled and forced")
		case r.PolicyCount != 1:
			bad = append(bad, r.Relname+": expected exactly one policy, found a different count")
		case !r.HasPolicy:
			bad = append(bad, r.Relname+": no policy with both USING and WITH CHECK")
		case !r.UsesFunction:
			bad = append(bad, r.Relname+": USING does not call hms_tenant_visible")
		case r.CheckUsesFunc:
			bad = append(bad, r.Relname+": WITH CHECK calls hms_tenant_visible instead of pinning to one tenant")
		}
	}
	return bad, nil
}
