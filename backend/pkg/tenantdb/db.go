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

// LintRLS returns tables carrying tenant_id without forced RLS + a policy.
func (d *DB) LintRLS(ctx context.Context) ([]string, error) {
	var bad []string
	// Platform convention: policies must carry both USING and WITH CHECK
	// explicitly, so a policy with only one clause is deliberately flagged.
	err := d.admin.WithContext(ctx).Raw(`
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r'
		  AND EXISTS (
		    SELECT 1 FROM information_schema.columns col
		    WHERE col.table_schema = 'public'
		      AND col.table_name = c.relname
		      AND col.column_name = 'tenant_id')
		  AND NOT (
		    c.relrowsecurity AND c.relforcerowsecurity
		    AND EXISTS (SELECT 1 FROM pg_policies p
		                WHERE p.schemaname = 'public' AND p.tablename = c.relname
		                  AND p.qual IS NOT NULL AND p.with_check IS NOT NULL))
		ORDER BY c.relname`).Scan(&bad).Error
	return bad, err
}
