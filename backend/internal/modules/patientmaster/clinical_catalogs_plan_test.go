//go:build performance

package patientmaster_test

import (
	"encoding/json"
	"github.com/google/uuid"
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

func TestClinicalConsentLedgerPlansStayBoundedAt100k(t *testing.T) {
	f := newClinicalFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	created := scopedDo(f.router, "PUT", "/v1/patients/my/clinical-consents/"+id, "owner", f.body)
	require.Equal(t, 201, created.Code, created.Body.String())
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-reviewer',true),set_config('app.patient_subject','user-reviewer',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO patient_sharing_recipients(tenant_id,hospital_id,id,organization_reference,display_name,grantee_subject,policy_reference,identity_evidence_reference,valid_until,content_hash,creator_subject)
 SELECT ?,?,('00000000-0000-7000-8000-'||lpad(to_hex(i),12,'0'))::uuid,'synthetic-plan-'||i,'Synthetic plan draft','synthetic-plan-subject','00000000-0000-7000-8000-000000000001','00000000-0000-7000-8000-000000000002',clock_timestamp()+interval '1 hour',repeat('0',64),'user-reviewer' FROM generate_series(1,100000) i`, testutil.TenantA, testHospital).Error
	}))
	// Filler declarations are structural plan fixtures with deliberately invalid MACs.
	// Eligibility assertions use only the real API-created, signed declaration.
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-owner',true),set_config('app.patient_consent_subject','user-owner',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
			return err
		}
		result := tx.Exec(`INSERT INTO patient_clinical_consents(tenant_id,hospital_id,id,patient_id,subject,authority_id,review_request_id,notice_id,recipient_id,purpose,fingerprint,body,hmac,valid_until,captured_at)
 SELECT c.tenant_id,c.hospital_id,fixture.id,c.patient_id,c.subject,c.authority_id,c.review_request_id,c.notice_id,fixture.id,c.purpose,c.fingerprint,
 jsonb_set(jsonb_set(c.body::jsonb,'{id}',to_jsonb(fixture.id::text)),'{recipient_id}',to_jsonb(fixture.id::text))::text,
 'vault:v1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=',c.valid_until,c.captured_at
 FROM patient_clinical_consents c CROSS JOIN (SELECT ('00000000-0000-7000-8000-'||lpad(to_hex(i),12,'0'))::uuid id FROM generate_series(1,100000) i) fixture
 WHERE c.tenant_id=? AND c.id=?`, testutil.TenantA, id)
		require.EqualValues(t, 100000, result.RowsAffected)
		return result.Error
	}))
	for _, principal := range []string{"owner", "recipient"} {
		t.Run(principal, func(t *testing.T) {
			require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				actor := "user-" + principal
				if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true),set_config('app.patient_consent_subject',?,true),set_config('app.patient_clinical_recipient',?,true),set_config('app.patient_hospital',?,true),set_config('app.patient_clinical_consent',?,true)`, actor, actor, actor, testHospital, id).Error; err != nil {
					return err
				}
				var plan string
				var err error
				if principal == "owner" {
					err = tx.Raw(`EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT id,hospital_id,status,version,valid_until,captured_at,created_at,withdrawn_at,quarantined_at FROM patient_clinical_consents WHERE tenant_id=? AND hospital_id=? AND subject=? ORDER BY created_at DESC,id DESC LIMIT 2`, testutil.TenantA, testHospital, actor).Scan(&plan).Error
				} else {
					err = tx.Raw(`EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT id,body,hmac,status,version FROM patient_clinical_consents WHERE tenant_id=? AND hospital_id=? AND id=? AND EXISTS(SELECT 1 FROM patient_sharing_recipients r WHERE r.tenant_id=patient_clinical_consents.tenant_id AND r.hospital_id=patient_clinical_consents.hospital_id AND r.id=patient_clinical_consents.recipient_id AND r.grantee_subject=?)`, testutil.TenantA, testHospital, id, actor).Scan(&plan).Error
				}
				if err != nil {
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
				require.Less(t, examined, float64(1000), "exact lookup/history page must remain bounded with RLS and audit triggers enabled")
				root := parsed[0]["Plan"].(map[string]any)
				blocks := root["Shared Hit Blocks"].(float64) + root["Shared Read Blocks"].(float64)
				require.Less(t, blocks, float64(128), "known consent/history page must not scan index pages for the whole ledger")
				return nil
			}))
		})
	}
	allowed := scopedDo(f.router, "POST", "/v1/patients/clinical-consents/"+id+"/decide", "recipient", `{"purpose":"care_coordination","resource_type":"diagnostic_report"}`)
	require.Equal(t, 200, allowed.Code, allowed.Body.String())
	require.Contains(t, allowed.Body.String(), `"consent_gate":"allow"`)
}
