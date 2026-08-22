package tenantdb

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var ErrInvalidTenant = errors.New("tenantdb: tenant id is not a valid uuid")

// DB owns up to three pools, each connecting as a role with a different
// privilege, because the three needs are genuinely different:
//
//   - app    — the non-BYPASSRLS role. All runtime request access.
//   - admin  — the schema OWNER. DDL: migrations and LintRLS. Owning the
//     tables is not the same as seeing every tenant's rows, and under
//     FORCE ROW LEVEL SECURITY it explicitly is not (#894).
//   - system — a role holding BYPASSRLS and no DDL. The only way to read
//     or write across every tenant at once. Nil unless OpenWithSystem
//     was used; WithAllTenants then fails loudly rather than falling back.
//
// There is no exported raw *gorm.DB.
type DB struct {
	app    *gorm.DB
	admin  *gorm.DB
	system *gorm.DB
}

// Open opens the app and admin pools. Callers that never cross tenant
// boundaries — cmd/migrate, cmd/bootstrap — want this; WithAllTenants on
// the result returns an error rather than silently degrading.
func Open(appDSN, adminDSN string) (*DB, error) {
	// logger.Silent is a PHI control, not a noise preference. GORM's logger
	// renders the executed SQL with parameter values inlined — verified
	// against the dev database, an errored insert logs
	// `INSERT INTO phi_probe VALUES (2,'HQ-OPD-0001427','Suresh Kumar')` —
	// and it does so on the error and slow-query paths, which is exactly
	// where a debugging session lives. Raising this to logger.Info or
	// logger.Warn to see a slow query would dump every column of every
	// failing write, patient names included, into the log stream. Do not.
	// Two things stop that regression:
	// TestGormOpenIsOnlyCalledFromTheAllowlist in internal/archtest keeps
	// the set of pools to this file and one test helper, and
	// TestOpenNeverLogsQueryParameters below asserts the property against
	// a real Postgres.
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

// OpenWithSystem opens Open's two pools plus the system pool, and refuses
// to return unless that pool's role can actually bypass RLS.
//
// That assertion is the point of this function (#894). Its absence is what
// let three whole-system call sites run for weeks against a role that
// could not see past FORCE ROW LEVEL SECURITY: every query succeeded and
// returned zero rows, so the reconciler wrote no tuples, the outbox never
// drained, and the pruner deleted nothing — each of them logging success.
// A cross-tenant pool that cannot cross tenants has no safe degraded mode,
// so this fails at boot with the role name rather than at 3am with a
// missing permission.
//
// It is the exact mirror of assertNoRLSBypass, which fails when the APP
// pool CAN bypass. Between them, each pool is pinned to the privilege its
// callers assume, and neither assumption can drift silently again.
func OpenWithSystem(appDSN, adminDSN, systemDSN string) (*DB, error) {
	db, err := Open(appDSN, adminDSN)
	if err != nil {
		return nil, err
	}
	if systemDSN == "" {
		return nil, errors.New("tenantdb: SYSTEM_DATABASE_URL is empty; " +
			"the reconciler, outbox drainer and retention pruner all read across " +
			"every tenant and cannot use the admin pool, whose role is bound by " +
			"FORCE ROW LEVEL SECURITY like any other")
	}
	system, err := gorm.Open(postgres.Open(systemDSN),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, fmt.Errorf("open system pool: %w", err)
	}
	sqlDB, err := system.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(2)
	if err := assertRLSBypass(system); err != nil {
		return nil, err
	}
	db.system = system
	return db, nil
}

// assertRLSBypass fails when the system pool's role CANNOT see through
// row-level security — the inverse of assertNoRLSBypass, and the guard
// whose absence produced #894.
//
// superuser counts as bypass here because it is one, but it is not what
// this should be pointed at: a role holding BYPASSRLS alone is the whole
// design, so the privilege stays auditable to one attribute on one role.
func assertRLSBypass(g *gorm.DB) error {
	var caps struct {
		Bypass bool
		Super  bool
	}
	result := g.Raw(`SELECT rolbypassrls AS bypass, rolsuper AS super
		FROM pg_roles WHERE rolname = current_user`).Scan(&caps)
	if result.Error != nil {
		return fmt.Errorf("probe system role capabilities: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return errors.New("tenantdb: could not determine system role capabilities " +
			"(current_user matched no row in pg_roles); refusing to assume it can " +
			"cross tenant boundaries")
	}
	if !caps.Bypass && !caps.Super {
		return fmt.Errorf("tenantdb: SYSTEM_DATABASE_URL connects as a role that "+
			"cannot bypass row-level security (bypassrls=%v superuser=%v). Every "+
			"cross-tenant read would return zero rows WITHOUT error — no tuples "+
			"reconciled, no outbox drained, no retention pruned, all reporting "+
			"success. Point it at the BYPASSRLS role", caps.Bypass, caps.Super)
	}
	return nil
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

// WithAdmin runs fn in a transaction on the admin pool, which connects as
// the schema OWNER. That is a DDL privilege, not a visibility one: it is
// the same privileged class Migrate and LintRLS are, and it exists for
// them.
//
// IT DOES NOT BYPASS RLS. This comment previously said it did — "bypasses
// RLS entirely — it sees every tenant's rows in every table" — and that
// sentence was false wherever it mattered (#894). Owning a table and
// seeing every row in it are different things, and under FORCE ROW LEVEL
// SECURITY they are explicitly different: FORCE binds the owner too,
// which is the entire reason it is chosen over plain ENABLE. The claim
// held only in dev and under the old test harness, where the owner was
// the Postgres image's superuser; in production CNPG creates a plain
// owner and every cross-tenant read returned zero rows, without error,
// for as long as anyone looked.
//
// For reads or writes that must cross tenants, use WithAllTenants. It is
// for process-startup/ops code, never for request-path handlers; nothing
// in the type system enforces that boundary, the same as Migrate and
// LintRLS today — internal/archtest does.
func (d *DB) WithAdmin(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return d.admin.WithContext(ctx).Transaction(fn)
}

// WithAllTenants runs fn in a transaction on the system pool, whose role
// holds BYPASSRLS — so it is the only way to read or write across every
// tenant at once. For process-startup and ops code (the reconciler, the
// outbox drainer, the retention pruner); never for request handlers.
//
// It exists because WithAdmin cannot do this and never could (#894). The
// admin pool connects as the schema OWNER, and both iam_members and
// outbox_events are FORCE ROW LEVEL SECURITY, which binds the owner —
// that being exactly why FORCE was chosen over ENABLE. WithAdmin's own
// comment claimed the opposite for months; it was true in dev, where the
// owner is the image superuser, and false in production, where CNPG
// creates a plain owner. Nothing failed, because a policy that hides
// every row returns zero rows, not an error.
//
// A nil system pool is an error, never a fallback to admin: falling back
// would restore precisely the silent-zero-rows behaviour this replaces.
func (d *DB) WithAllTenants(ctx context.Context, fn func(tx *gorm.DB) error) error {
	if d.system == nil {
		return errors.New("tenantdb: no system pool; this DB was opened with Open, " +
			"not OpenWithSystem, so it cannot read across tenants")
	}
	return d.system.WithContext(ctx).Transaction(fn)
}

// lintAllowlist names the tables that legitimately carry no tenant_id.
//
// outbox_events is deliberately NOT here (#835/#774, Task 1): it now
// carries a nullable tenant_id and forced RLS via
// events.Migrations()'s 0002_events_outbox_tenant, so a module that
// forgets its own table's isolation is still caught the same as before —
// this table stopped being a hole.
var lintAllowlist = map[string]bool{
	"schema_migrations": true,
	// processed_events is the cross-consumer idempotency ledger
	// (consumer, event_id, processed_at) — keyed on (consumer, event_id),
	// not per-tenant. Not every event carries a tenant (broadcasts, e.g.
	// CredentialRevoked, have none — bus.go's handleMsg only sets the GUC
	// when evt.TenantID parses), so a NOT NULL tenant_id would break
	// dedup for those, and a nullable one gives a tenant_isolation policy
	// nothing to enforce (every consumer's claim row would be equally
	// unscoped). It holds no PHI — consumer name, event id, timestamp —
	// the same argument iam_credential_revocations makes below. It needs
	// pruning (retention, not RLS), tracked separately (#835 Task 2).
	"processed_events": true,
	// A GIP subject is global, not tenant-scoped, so a revocation
	// watermark cannot carry a tenant_id and cannot be RLS-policied. The
	// table holds no tenant data and no PHI: subject, timestamp, reason,
	// actor. See backend/internal/modules/iam/revocation.go.
	"iam_credential_revocations": true,
	// login_attempt holds the Zitadel session between the password step
	// and the factor step. Login happens BEFORE a tenant is selected, so
	// there is no tenant_id to scope on — the same argument
	// iam_credential_revocations makes above. See
	// backend/internal/modules/iam/loginattempt.go.
	"login_attempt": true,
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

// LintDirectedProvenance returns every declared directed-write table that
// cannot carry provenance, each as "<table>: <reason>".
//
// A table that accepts a write on behalf of another tenant must record
// WHICH tenant and WHICH record it came from, and must record them
// NOT NULL — a nullable provenance column is provenance that will
// eventually be NULL on the row someone needs during an audit (design D5).
//
// A declared table that is on LintRLS's lintAllowlist fails outright,
// before its columns are examined. The two linters are otherwise
// independent — LintRLS covers any table carrying tenant_id — so a table
// appearing in BOTH a module's DirectedWriteTables() and the allowlist
// would pass both while carrying provenance and no tenant isolation at
// all. Accepting a cross-tenant write into a table nothing isolates is
// the exact failure this design exists to prevent, so it fails closed
// here rather than being covered by neither.
//
// Separate from LintRLS rather than folded into it: LintRLS enumerates
// every table and subtracts an allowlist, whereas this checks only the
// tables modules declared, so the two have different inputs. It runs
// beside LintRLS at boot and in cmd/migrate.
func (d *DB) LintDirectedProvenance(ctx context.Context, tables []string) ([]string, error) {
	if len(tables) == 0 {
		return nil, nil
	}
	type row struct {
		Relname        string
		TableExists    bool
		OriginTenantOK bool
		OriginRecordOK bool
	}
	var rows []row
	err := d.admin.WithContext(ctx).Raw(`
		SELECT t.relname,
		       EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		               WHERE n.nspname = 'public' AND c.relname = t.relname
		                 AND c.relkind = 'r') AS table_exists,
		       EXISTS (SELECT 1 FROM information_schema.columns col
		               WHERE col.table_schema = 'public' AND col.table_name = t.relname
		                 AND col.column_name = 'origin_tenant_id'
		                 AND col.is_nullable = 'NO') AS origin_tenant_ok,
		       EXISTS (SELECT 1 FROM information_schema.columns col
		               WHERE col.table_schema = 'public' AND col.table_name = t.relname
		                 AND col.column_name = 'origin_record_id'
		                 AND col.is_nullable = 'NO') AS origin_record_ok
		  FROM unnest(string_to_array(?, ',')) AS t(relname)`,
		strings.Join(tables, ",")).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("lint directed provenance: %w", err)
	}

	var bad []string
	for _, r := range rows {
		switch {
		case lintAllowlist[r.Relname]:
			bad = append(bad, r.Relname+": declared as a directed-write table but is on LintRLS's "+
				"allowlist, so no linter checks its tenant isolation")
		case !r.TableExists:
			bad = append(bad, r.Relname+": declared as a directed-write table but does not exist")
		case !r.OriginTenantOK:
			bad = append(bad, r.Relname+": origin_tenant_id is missing or nullable")
		case !r.OriginRecordOK:
			bad = append(bad, r.Relname+": origin_record_id is missing or nullable")
		}
	}
	return bad, nil
}
