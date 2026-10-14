package audit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/audit"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// probe is a tenant table opted into the trail exactly as module tables are.
var probe = tenantdb.Migration{
	ID: "0001_audit_probe",
	SQL: `
		CREATE TABLE audit_probe (
		  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
		  tenant_id uuid NOT NULL,
		  secret text NOT NULL,
		  note text NOT NULL
		);
		ALTER TABLE audit_probe ENABLE ROW LEVEL SECURITY;
		ALTER TABLE audit_probe FORCE ROW LEVEL SECURITY;
		CREATE POLICY tenant_isolation ON audit_probe
		  USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
		CREATE TRIGGER audit_probe_audit AFTER INSERT OR UPDATE OR DELETE ON audit_probe
		  FOR EACH ROW EXECUTE FUNCTION audit_record_change();`,
}

type event struct {
	ActorSubject  string
	Action        string
	ResourceType  string
	ResourceID    string
	ChangedFields string
}

func setUp(t *testing.T) *tenantdb.DB {
	t.Helper()
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	migrations := append(tenantdb.Migrations(), audit.Migrations()...)
	require.NoError(t, db.Migrate(context.Background(), append(migrations, probe)))
	return db
}

func events(t *testing.T, db *tenantdb.DB, tenant, id string) []event {
	t.Helper()
	var rows []event
	require.NoError(t, db.WithTenant(context.Background(), tenant, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT actor_subject, action, resource_type, resource_id::text AS resource_id,
			array_to_string(changed_fields, ',') AS changed_fields
			FROM audit_events WHERE resource_id = ? ORDER BY occurred_at, action DESC`, id).Scan(&rows).Error
	}))
	return rows
}

func TestTriggerRecordsActorAndChangedColumnNamesOnly(t *testing.T) {
	db := setUp(t)
	ctx := tenantdb.WithActor(context.Background(), "user-1")
	tenantA, tenantB, id := uuid.NewString(), uuid.NewString(), uuid.NewString()

	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO audit_probe (id, tenant_id, secret, note) VALUES (?, ?, 'old-secret', 'n')`, id, tenantA).Error
	}))
	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE audit_probe SET secret = 'new-secret' WHERE id = ?`, id).Error
	}))
	require.NoError(t, db.WithTenant(tenantdb.WithActor(context.Background(), "system:probe"), tenantA, func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM audit_probe WHERE id = ?`, id).Error
	}))

	require.Equal(t, []event{
		{ActorSubject: "user-1", Action: "insert", ResourceType: "audit_probe", ResourceID: id, ChangedFields: ""},
		{ActorSubject: "user-1", Action: "update", ResourceType: "audit_probe", ResourceID: id, ChangedFields: "secret"},
		{ActorSubject: "system:probe", Action: "delete", ResourceType: "audit_probe", ResourceID: id, ChangedFields: ""},
	}, events(t, db, tenantA, id))
	require.Empty(t, events(t, db, tenantB, id))

	var leaked int64
	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM audit_events WHERE row_to_json(audit_events)::text LIKE '%-secret%'`).Scan(&leaked).Error
	}))
	require.Zero(t, leaked, "audit rows must not carry column values")
}

func TestTriggerRejectsWriteWithoutActor(t *testing.T) {
	db := setUp(t)
	tenant := uuid.NewString()
	err := db.WithTenant(context.Background(), tenant, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO audit_probe (tenant_id, secret, note) VALUES (?, 's', 'n')`, tenant).Error
	})
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr))
	require.Equal(t, "42501", pgErr.Code)
}
