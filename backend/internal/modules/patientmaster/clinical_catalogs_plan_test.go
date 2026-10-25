//go:build performance

package patientmaster_test

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
	"testing"
)

func TestClinicalRecipientCatalogPlanBoundsHistoricalDraftScan(t *testing.T) {
	f := newClinicalFixture(t)
	var input map[string]any
	require.NoError(t, json.Unmarshal([]byte(f.body), &input))
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-reviewer',true),set_config('app.patient_subject','user-reviewer',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO patient_sharing_recipients(tenant_id,hospital_id,id,organization_reference,display_name,grantee_subject,policy_reference,identity_evidence_reference,valid_until,content_hash,creator_subject)
 SELECT ?,?,('00000000-0000-7000-8000-'||lpad(to_hex(i),12,'0'))::uuid,'synthetic-plan-'||i,'Synthetic plan draft','synthetic-plan-subject','00000000-0000-7000-8000-000000000001','00000000-0000-7000-8000-000000000002',clock_timestamp()+interval '1 hour',repeat('0',64),'user-reviewer' FROM generate_series(1,100000) i`, testutil.TenantA, testHospital).Error
	}))
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-owner',true),set_config('app.patient_consent_subject','user-owner',true),set_config('app.patient_clinical_recipient','user-owner',true),set_config('app.patient_hospital',?,true),set_config('app.patient_clinical_authority',?,true)`, testHospital, input["authority_id"]).Error; err != nil {
			return err
		}
		var plan string
		if err := tx.Raw(`EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT id,hospital_id,organization_reference,display_name,version,valid_until,created_at FROM patient_sharing_recipients WHERE tenant_id=? AND hospital_id=? AND status='approved' AND valid_until>clock_timestamp() ORDER BY created_at DESC,id DESC LIMIT 2`, testutil.TenantA, testHospital).Scan(&plan).Error; err != nil {
			return err
		}
		t.Log(plan)
		var parsed []map[string]any
		require.NoError(t, json.Unmarshal([]byte(plan), &parsed))
		var examined float64
		var walk func(map[string]any)
		walk = func(node map[string]any) {
			for _, key := range []string{"Actual Rows", "Rows Removed by Filter"} {
				if n, ok := node[key].(float64); ok {
					examined += n
				}
			}
			if children, ok := node["Plans"].([]any); ok {
				for _, child := range children {
					walk(child.(map[string]any))
				}
			}
		}
		walk(parsed[0]["Plan"].(map[string]any))
		require.Less(t, examined, float64(1000), "approved choices must not scan all historical drafts")
		return nil
	}))
}
