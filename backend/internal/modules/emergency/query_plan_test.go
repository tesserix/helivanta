//go:build emergencyplans

package emergency_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
