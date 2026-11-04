package workforce

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"strings"
	"testing"
)

func TestPostingSupervisorChoiceQueryUsesBoundedIndexedProbes(t *testing.T) {
	appDSN, ownerDSN, _ := testinfra.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, ownerDSN)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.Migrate(t.Context(), tenantdb.Migrations()))
	apply := func(id, sql string) {
		require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_supervisor_plan_" + id, SQL: sql}}))
	}
	// Reduced relation shapes isolate planner behavior; real scope, migrations and audit are tested through Global API.
	apply("relations", `
 CREATE TABLE workforce_staff(tenant_id uuid,hospital_id uuid,id uuid,employee_code text,display_name text,PRIMARY KEY(tenant_id,hospital_id,id));
 CREATE TABLE workforce_posting_proposals(tenant_id uuid,hospital_id uuid,staff_id uuid,id uuid,revision_id uuid,version bigint,status text,PRIMARY KEY(tenant_id,id));
 CREATE TABLE workforce_posting_proposal_revisions(tenant_id uuid,hospital_id uuid,staff_id uuid,proposal_id uuid,id uuid,version bigint,department_id uuid,department_revision_id uuid,starts_at timestamptz,ends_at timestamptz,eligibility_until timestamptz,PRIMARY KEY(tenant_id,id));
 CREATE TABLE workforce_posting_supervision_intents(tenant_id uuid,hospital_id uuid,staff_id uuid,proposal_id uuid,kind text,created_at timestamptz,PRIMARY KEY(tenant_id,hospital_id,staff_id,proposal_id));
 CREATE TEMP TABLE synthetic_placements AS SELECT
 CASE WHEN n%7<3 THEN '11111111-1111-4111-8111-111111111111' ELSE '99999999-9999-4999-8999-999999999999' END::uuid AS tenant_id,
 CASE WHEN n%3=0 THEN '22222222-2222-4222-8222-222222222222' ELSE '88888888-8888-4888-8888-888888888888' END::uuid AS hospital_id,
 '33333333-3333-4333-8333-333333333334'::uuid AS staff_id,
 ('019a0000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid AS id,
 CASE WHEN n%100=0 THEN 'hospital_management' WHEN n%10=0 THEN 'department_supervisor' ELSE 'department_member' END AS kind,
 CASE WHEN n%11=0 THEN 'pending' ELSE 'approved' END AS status,
 '2026-11-01T00:00:00Z'::timestamptz+n*interval '1 microsecond' AS created_at
 FROM generate_series(1,100000)n;
 INSERT INTO workforce_staff SELECT DISTINCT tenant_id,hospital_id,staff_id,'SYN-PLAN','Synthetic planner Staff' FROM synthetic_placements;
 INSERT INTO workforce_posting_proposals SELECT tenant_id,hospital_id,staff_id,id,id,2,status FROM synthetic_placements;
 INSERT INTO workforce_posting_proposal_revisions SELECT tenant_id,hospital_id,staff_id,id,id,2,'019a0000-0000-7000-8000-000000000007','019a0000-0000-7000-8000-000000000008','2026-10-01T00:00:00Z',NULL,'2028-01-01T00:00:00Z' FROM synthetic_placements;
 INSERT INTO workforce_posting_supervision_intents SELECT tenant_id,hospital_id,staff_id,id,kind,created_at FROM synthetic_placements;
 DROP TABLE synthetic_placements;
 `)
	scoped := scopedPostingsSchema[strings.Index(scopedPostingsSchema, "CREATE FUNCTION workforce_scoped_posting_context"):]
	scoped = scoped[:strings.Index(scoped, "$$;")+3]
	apply("scoped_context", scoped)
	proposalContext := postingProposalsSchema[strings.Index(postingProposalsSchema, "CREATE FUNCTION workforce_posting_proposal_context"):]
	proposalContext = proposalContext[:strings.Index(proposalContext, "$$;")+3]
	apply("proposal_context", proposalContext)
	for _, table := range []string{"workforce_posting_proposals", "workforce_posting_proposal_revisions", "workforce_posting_supervision_intents"} {
		apply("predecessor_"+table, "CREATE POLICY tenant_isolation ON "+table+" USING(hms_tenant_visible(tenant_id) AND workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,false)) WITH CHECK(workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,true))")
	}
	for _, migration := range New().Migrations() {
		if migration.ID == "0082_workforce_posting_supervisor_choices" || migration.ID == "0083_workforce_posting_supervisor_choice_index" {
			require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{migration}))
		}
	}
	apply("rls_and_statistics", `ALTER TABLE workforce_staff ENABLE ROW LEVEL SECURITY;ALTER TABLE workforce_staff FORCE ROW LEVEL SECURITY;
 ALTER TABLE workforce_posting_proposals ENABLE ROW LEVEL SECURITY;ALTER TABLE workforce_posting_proposals FORCE ROW LEVEL SECURITY;
 ALTER TABLE workforce_posting_proposal_revisions ENABLE ROW LEVEL SECURITY;ALTER TABLE workforce_posting_proposal_revisions FORCE ROW LEVEL SECURITY;
 ALTER TABLE workforce_posting_supervision_intents ENABLE ROW LEVEL SECURITY;ALTER TABLE workforce_posting_supervision_intents FORCE ROW LEVEL SECURITY;
 CREATE POLICY fixture_staff_directory ON workforce_staff FOR SELECT USING(hms_tenant_visible(tenant_id) AND workforce_scoped_posting_context(tenant_id,hospital_id,NULLIF(current_setting('app.workforce_posting_actor',true),'')::uuid,false) AND current_setting('app.workforce_posting_directory',true)='read');
 GRANT SELECT ON workforce_staff TO hms_app;
 GRANT SELECT,UPDATE,DELETE ON workforce_posting_proposals,workforce_posting_proposal_revisions,workforce_posting_supervision_intents TO hms_app;
 ANALYZE workforce_staff;ANALYZE workforce_posting_proposals;ANALYZE workforce_posting_proposal_revisions;ANALYZE workforce_posting_supervision_intents;`)
	principal := authn.Principal{TenantID: "11111111-1111-4111-8111-111111111111", Subject: "synthetic-planner"}
	hospital := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	require.NoError(t, db.WithTenant(t.Context(), principal.TenantID, func(tx *gorm.DB) error {
		err := tx.Exec(`SELECT set_config('app.workforce_hospital','22222222-2222-4222-8222-222222222222',true),set_config('app.workforce_subject','synthetic-planner',true),set_config('app.workforce_posting_staff','33333333-3333-4333-8333-333333333333',true),set_config('app.workforce_posting_actor','33333333-3333-4333-8333-333333333333',true),set_config('app.workforce_posting_link','019a0000-0000-7000-8000-000000000001',true),set_config('app.workforce_posting_generation','019a0000-0000-7000-8000-000000000002',true),set_config('app.workforce_posting_assignment','019a0000-0000-7000-8000-000000000003',true),set_config('app.workforce_posting_assignment_version','1',true),set_config('app.workforce_posting_scope_epoch','1',true),set_config('app.workforce_posting_membership_epoch','1',true),set_config('app.workforce_posting','read',true),set_config('app.workforce_posting_scoped','read',true),set_config('app.workforce_posting_review_mode','read',true),set_config('app.workforce_posting_permission','workforce.posting.submit',true),set_config('app.workforce_posting_supervisor_choices','read',true),set_config('app.workforce_posting_directory','read',true)`).Error
		if err != nil {
			return err
		}
		var indexBytes int64
		require.NoError(t, tx.Raw("SELECT pg_relation_size('workforce_posting_supervisor_choice_order')").Scan(&indexBytes).Error)
		t.Logf("100000 synthetic intent index bytes=%d", indexBytes)
		page := pagination.Params{Limit: 8}
		for _, name := range []string{"first", "continuation"} {
			var data []postingSupervisorChoiceRecord
			statement := postingSupervisorChoiceQuery(tx.Session(&gorm.Session{DryRun: true}), principal, hospital, page).Find(&data).Statement
			rows, err := tx.Statement.ConnPool.QueryContext(t.Context(), "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+statement.SQL.String(), statement.Vars...)
			require.NoError(t, err)
			require.True(t, rows.Next())
			var raw []byte
			require.NoError(t, rows.Scan(&raw))
			require.NoError(t, rows.Close())
			require.Contains(t, string(raw), "workforce_posting_supervisor_choice_order")
			var plan []struct {
				ExecutionTime float64 `json:"Execution Time"`
				Plan          struct {
					ActualRows int `json:"Actual Rows"`
				} `json:"Plan"`
			}
			require.NoError(t, json.Unmarshal(raw, &plan))
			require.Equal(t, 9, plan[0].Plan.ActualRows)
			t.Logf("%s: 100000 synthetic postings, indexed manager probes, rows=%d execution_ms=%.3f", name, plan[0].Plan.ActualRows, plan[0].ExecutionTime)
			require.NoError(t, postingSupervisorChoiceQuery(tx, principal, hospital, page).Find(&data).Error)
			require.Len(t, data, 9)
			page.After = &pagination.Cursor{TenantID: principal.TenantID, CreatedAt: data[7].CreatedAt, ID: data[7].ID}
		}
		for _, table := range []string{"workforce_posting_supervision_intents", "workforce_posting_proposal_revisions", "workforce_posting_proposals"} {
			deleted := tx.Exec("DELETE FROM "+table+" WHERE tenant_id=? AND hospital_id=?", principal.TenantID, hospital)
			require.NoError(t, deleted.Error)
			require.Zero(t, deleted.RowsAffected, "discovery read context must not widen DELETE visibility: %s", table)
			column := "version"
			if table == "workforce_posting_supervision_intents" {
				column = "kind"
			}
			updated := tx.Exec("UPDATE "+table+" SET "+column+"="+column+" WHERE tenant_id=? AND hospital_id=?", principal.TenantID, hospital)
			require.NoError(t, updated.Error)
			require.Zero(t, updated.RowsAffected, "discovery read context must not widen UPDATE visibility: %s", table)
		}
		return nil
	}))
}
