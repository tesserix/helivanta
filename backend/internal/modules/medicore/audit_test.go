package medicore_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
)

func TestVisitCreationIsAuditedPerTenant(t *testing.T) {
	r, db, ctx := setup(t)

	w := do(r, "POST", "/v1/medicore/visits", "tokA", `{"patient_name":"Asha Rao","department":"OPD"}`)
	require.Equal(t, http.StatusAccepted, w.Code)
	var resp struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	type auditRow struct {
		ActorSubject string
		Action       string
		ResourceType string
		ResourceID   string
	}
	read := func(tenant string) []auditRow {
		var rows []auditRow
		require.NoError(t, db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT actor_subject, action, resource_type, resource_id::text AS resource_id
				FROM audit_events WHERE resource_id = ?`, resp.ID).Scan(&rows).Error
		}))
		return rows
	}
	require.Equal(t, []auditRow{{
		ActorSubject: "user-tokA", Action: "insert", ResourceType: "medicore_visits", ResourceID: resp.ID,
	}}, read(testutil.TenantA))
	require.Empty(t, read(testutil.TenantB))
}

func TestAuditTrailIsAppendOnly(t *testing.T) {
	r, db, ctx := setup(t)
	require.Equal(t, http.StatusAccepted,
		do(r, "POST", "/v1/medicore/visits", "tokA", `{"patient_name":"Asha Rao","department":"OPD"}`).Code)

	// The app role cannot TRUNCATE at all (42501); the trigger refuses UPDATE and DELETE (23514).
	for stmt, code := range map[string]string{
		`UPDATE audit_events SET actor_subject = 'someone-else'`: "23514",
		`DELETE FROM audit_events`:                               "23514",
		`TRUNCATE audit_events`:                                  "42501",
	} {
		err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return tx.Exec(stmt).Error })
		var pgErr *pgconn.PgError
		require.True(t, errors.As(err, &pgErr), stmt)
		require.Equal(t, code, pgErr.Code, stmt)
	}
}

func TestWriteWithoutActorIsRejected(t *testing.T) {
	_, db, ctx := setup(t)
	err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO medicore_visits (tenant_id, patient_name, department) VALUES (?, 'X', 'OPD')`,
			testutil.TenantA).Error
	})
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr))
	require.Equal(t, "42501", pgErr.Code)
}
