//go:build emergencyplans

package emergency_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

func TestQueueAndExpiryAccessPathsAtOneHundredThousandCases(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	for first := 1; first <= 100000; first += 1000 {
		last := first + 999
		require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.emergency_subject','user-supervisor',true),set_config('app.emergency_base',?,true)`, f.base).Error; err != nil {
				return err
			}
			// Synthetic envelopes are for query planning only; no decryption is attempted.
			if err := tx.Exec(`INSERT INTO emergency_cases(tenant_id,base_id,id,case_number,channel,requested_service,processing_basis,policy_reference,request_fingerprint,contact_ciphertext,incident_ciphertext,created_at,updated_at,contact_retain_until)
 SELECT ?::uuid,?::uuid,('01990000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'EC-PLAN-'||n,'phone','unknown','medical_emergency','SYN-QUERY-PLAN','vault:v1:synthetic','vault:v1:synthetic','vault:v1:synthetic',now()-interval '3 days'+n*interval '1 second',now(),now()+(CASE WHEN n<=50000 THEN -1 ELSE 1 END)*interval '1 day'
 FROM generate_series(?::int,?::int) n`, testutil.TenantA, f.base, first, last).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO emergency_case_timeline(tenant_id,base_id,case_id,id,version,state,actor_subject) SELECT tenant_id,base_id,id,id,1,'received','user-supervisor' FROM emergency_cases WHERE tenant_id=? AND base_id=? AND id BETWEEN ('01990000-0000-7000-8000-'||lpad(to_hex(?::int),12,'0'))::uuid AND ('01990000-0000-7000-8000-'||lpad(to_hex(?::int),12,'0'))::uuid`, testutil.TenantA, f.base, first, last).Error
		}))

	}
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.emergency_subject','user-supervisor',true),set_config('app.emergency_base',?,true)`, f.base).Error; err != nil {
			return err
		}
		var count int64
		require.NoError(t, tx.Raw(`SELECT count(*) FROM emergency_cases`).Scan(&count).Error)
		require.EqualValues(t, 100000, count)
		queries := []string{
			`EXPLAIN (ANALYZE,BUFFERS) SELECT id,case_number,status,priority,created_at FROM emergency_cases WHERE tenant_id=? AND base_id=? ORDER BY created_at DESC,id DESC LIMIT 51`,
			`EXPLAIN (ANALYZE,BUFFERS) SELECT tenant_id,base_id,id FROM emergency_cases WHERE contact_ciphertext IS NOT NULL AND contact_retain_until<=now() AND tenant_id=? AND base_id=? ORDER BY contact_retain_until,tenant_id,base_id,id LIMIT 200 FOR UPDATE SKIP LOCKED`,
		}
		for _, query := range queries {
			var lines []string
			if err := tx.Raw(query, testutil.TenantA, f.base).Scan(&lines).Error; err != nil {
				return err
			}
			plan := strings.Join(lines, "\n")
			t.Log(plan)
			require.NotContains(t, plan, "Seq Scan on emergency_cases")
			require.Contains(t, plan, "Index Scan")
		}
		return nil
	}))
}

func TestCrewDiscoveryDoesNotScanDispatchHistory(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, v, d := prepareDispatch(t, f)
	id := newID()
	require.Equal(t, 202, testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+id, "dispatcher", dispatchBody(c, a, v, d)).Code)
	_, err := dispatchProcessor(t, f).Offer(t.Context(), emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: f.base, DispatchID: id})
	require.NoError(t, err)
	require.NoError(t, f.db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
		require.NoError(t, tx.Exec(`SELECT set_config('app.tenant_id',?,true),set_config('app.emergency_subject','user-supervisor',true),set_config('app.emergency_base',?,true)`, testutil.TenantA, f.base).Error)
		// Only this disposable plan fixture bypasses lifecycle triggers; all foreign keys and checks remain enforced.
		require.NoError(t, tx.Exec(`ALTER TABLE emergency_dispatches DISABLE TRIGGER USER`).Error)
		require.NoError(t, tx.Exec(`INSERT INTO emergency_dispatches(tenant_id,base_id,id,case_id,case_version,assessment_id,vehicle_id,duty_id,acknowledgement_seconds,requested_by,driver_subject,clinician_subject,state,acknowledgement_deadline)
 SELECT d.tenant_id,d.base_id,('01990000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,d.case_id,d.case_version,d.assessment_id,d.vehicle_id,d.duty_id,d.acknowledgement_seconds,d.requested_by,d.driver_subject,d.clinician_subject,'cancelled',d.acknowledgement_deadline
 FROM emergency_dispatches d CROSS JOIN generate_series(1,100000)n WHERE d.tenant_id=? AND d.base_id=? AND d.id=?`, testutil.TenantA, f.base, id).Error)
		require.NoError(t, tx.Exec(`ALTER TABLE emergency_dispatches ENABLE TRIGGER USER`).Error)
		return tx.Exec(`ANALYZE emergency_dispatches`).Error
	}))
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		require.NoError(t, tx.Exec(`SELECT set_config('app.emergency_subject','user-driver',true)`).Error)
		var plan []string
		require.NoError(t, tx.Raw(`EXPLAIN(ANALYZE,BUFFERS) SELECT id,base_id,state,version,created_at FROM emergency_dispatches WHERE tenant_id=? AND ? IN(driver_subject,clinician_subject) AND acknowledgement_deadline IS NOT NULL AND state NOT IN('workflow_pending','cleared','cancelled') ORDER BY created_at DESC,id DESC LIMIT 51`, testutil.TenantA, "user-driver").Scan(&plan).Error)
		text := strings.Join(plan, "\n")
		t.Log(text)
		require.NotContains(t, text, "Seq Scan on emergency_dispatches")
		require.Contains(t, text, "Index Scan")
		return nil
	}))
}
