//go:build performance

package patientmaster_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestProxyClinicalNoticeCatalogPlanBoundsOtherCapacitiesAt100k(t *testing.T) {
	f := newClinicalFixtureForCapacity(t, "parental")
	var input map[string]any
	require.NoError(t, json.Unmarshal([]byte(f.body), &input))
	// Structural fixtures use deliberately invalid content hashes; the only notice
	// eligible for this proxy is the real API-created, independently approved one.
	require.NoError(t, proxyStaffTransaction(f.db, t, func(tx *gorm.DB) error {
		result := tx.Exec(`INSERT INTO patient_sharing_notices(tenant_id,hospital_id,id,purpose,locale,version_label,title,text,withdrawal_consequence,policy_reference,content_hash,creator_subject)
 SELECT ?,?,('00000000-0000-7000-8000-'||lpad(to_hex(i),12,'0'))::uuid,'care_coordination','en-IN','synthetic-capacity-plan-'||i,'Synthetic structural fixture','Synthetic structural fixture; not real policy.','Synthetic consequence.','00000000-0000-7000-8000-000000000001',repeat('0',64),'user-reviewer' FROM generate_series(1,100000)i`, testutil.TenantA, testHospital)
		require.EqualValues(t, 100000, result.RowsAffected)
		return result.Error
	}))
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-approver',true),set_config('app.patient_subject','user-approver',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
			return err
		}
		result := tx.Exec(`UPDATE patient_sharing_notices SET status='approved',version=2,reviewer_subject='user-approver',review_reference='00000000-0000-7000-8000-000000000002',approved_at=clock_timestamp(),updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND version_label LIKE 'synthetic-capacity-plan-%'`, testutil.TenantA, testHospital)
		require.EqualValues(t, 100000, result.RowsAffected)
		return result.Error
	}))
	require.NoError(t, f.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_analyze_proxy_notice_capacity", SQL: `ANALYZE patient_sharing_notices`}}))
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-owner',true),set_config('app.patient_consent_subject','user-owner',true),set_config('app.patient_clinical_recipient','user-owner',true),set_config('app.patient_hospital',?,true),set_config('app.patient_proxy_authority',?,true)`, testHospital, input["authority_id"]).Error; err != nil {
			return err
		}
		var plan string
		if err := tx.Raw(`EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT id,hospital_id,authority_kind,purpose,locale,version_label,title,text,withdrawal_consequence,policy_reference,content_hash,creator_subject,reviewer_subject,review_reference,status,version,approved_at,retired_at,created_at,updated_at FROM patient_sharing_notices WHERE tenant_id=? AND hospital_id=? AND status='approved' AND purpose='care_coordination' AND authority_kind='parental' ORDER BY created_at DESC,id DESC LIMIT 2`, testutil.TenantA, testHospital).Scan(&plan).Error; err != nil {
			return err
		}
		t.Log(plan)
		var parsed []map[string]any
		require.NoError(t, json.Unmarshal([]byte(plan), &parsed))
		root := parsed[0]["Plan"].(map[string]any)
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
		walk(root)
		require.Less(t, examined, float64(1000), "proxy choices must not scan other capacities")
		require.Less(t, root["Shared Hit Blocks"].(float64)+root["Shared Read Blocks"].(float64), float64(128))
		require.Contains(t, plan, "patient_sharing_notice_capacity_page")
		return nil
	}))
	page := scopedDo(f.router, "GET", "/v1/patients/my/proxy-authorities/"+input["authority_id"].(string)+"/sharing-notices?limit=1", "owner", "")
	require.Equal(t, 200, page.Code, page.Body.String())
	require.Contains(t, page.Body.String(), input["notice_id"])
	require.NotContains(t, page.Body.String(), "synthetic-capacity-plan-")
}
