package globalapi

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"net/url"
	"strings"
	"testing"
)

func assertPostingSupervisorRootChoice(t *testing.T, call func(*testing.T, string, string, string, any, int) map[string]any, hospital, submitter, reviewer, staff string, approved map[string]any) {
	t.Helper()
	for _, read := range []struct{ subject, purpose string }{{submitter, "submission"}, {reviewer, "review"}} {
		page := call(t, "GET", "/v1/workforce/hospitals/"+hospital+"/posting-supervisor-choices?purpose="+read.purpose+"&limit=1", read.subject, nil, 200)
		rows := page["data"].([]any)
		require.Len(t, rows, 1)
		row := rows[0].(map[string]any)
		require.Equal(t, approved["id"], row["id"])
		require.Equal(t, approved["revision_id"], row["revision_id"])
		require.Equal(t, hospital, row["hospital_id"])
		require.Equal(t, "hospital_management", row["kind"])
		label := row["staff"].(map[string]any)
		require.Equal(t, staff, label["id"])
		require.NotEmpty(t, label["display_name"])
		require.NotEmpty(t, label["employee_code"])
		require.NotContains(t, row, "reason")
		require.NotContains(t, row, "submission_reason")
		require.NotContains(t, row, "supervisor_staff_id")
	}
}

func assertPostingSupervisorChoiceContinuation(t *testing.T, db *tenantdb.DB, tenant string, call func(*testing.T, string, string, string, any, int) map[string]any, hospital, submitter, reviewer string, department, root map[string]any) {
	t.Helper()
	base := "/v1/workforce/hospitals/" + hospital + "/posting-supervisor-choices"
	page := call(t, "GET", base+"?purpose=submission&limit=1", submitter, nil, 200)
	first := page["data"].([]any)
	require.Len(t, first, 1)
	require.Equal(t, department["id"], first[0].(map[string]any)["id"], "newer ordinary members never appear as supervisor choices")
	require.Equal(t, "department_supervisor", first[0].(map[string]any)["kind"])
	cursor := page["page"].(map[string]any)["next_cursor"].(string)
	require.True(t, page["page"].(map[string]any)["has_more"].(bool))
	next := call(t, "GET", base+"?purpose=submission&limit=1&cursor="+url.QueryEscape(cursor), submitter, nil, 200)
	require.Equal(t, root["id"], next["data"].([]any)[0].(map[string]any)["id"])
	require.Equal(t, "hospital_management", next["data"].([]any)[0].(map[string]any)["kind"])
	for _, read := range []struct{ subject, purpose string }{{submitter, "review"}, {reviewer, "submission"}} {
		denied := call(t, "GET", base+"?purpose="+read.purpose+"&limit=1&cursor="+url.QueryEscape(cursor), read.subject, nil, 400)
		require.NotContains(t, denied, "data")
	}
	for _, query := range []string{"", "?purpose=submission&purpose=review", "?purpose=all", "?purpose=review&staff_id=" + uuid.Must(uuid.NewV7()).String(), "?purpose=review&limit=9"} {
		denied := call(t, "GET", base+query, reviewer, nil, 400)
		require.NotContains(t, denied, "data")
	}
	before := postingRootApprovalEffects(t, db, tenant, reviewer)
	constraint := "synthetic_supervisor_choices_read_audit"
	apply := func(sql string) {
		require.NoError(t, db.Migrate(context.WithoutCancel(t.Context()), []tenantdb.Migration{{ID: "test_supervisor_choices_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}
	clear := func() { apply("ALTER TABLE workforce_audit DROP CONSTRAINT IF EXISTS " + constraint) }
	t.Cleanup(clear)
	apply("ALTER TABLE workforce_audit ADD CONSTRAINT " + constraint + " CHECK(action<>'workforce.posting_proposal.supervisor_choices_read') NOT VALID")
	denied := call(t, "GET", base+"?purpose=review", reviewer, nil, 500)
	require.NotContains(t, denied, "data")
	require.Equal(t, before, postingRootApprovalEffects(t, db, tenant, reviewer), "failed disclosure also rolls back actor-use audit")
	clear()
	recovered := call(t, "GET", base+"?purpose=review&limit=1", reviewer, nil, 200)
	require.Equal(t, department["id"], recovered["data"].([]any)[0].(map[string]any)["id"])
}

func applySupervisorDiscoveryMigration(t *testing.T, db *tenantdb.DB, ctx context.Context, migration tenantdb.Migration) {
	t.Helper()
	type policy struct{ TableName, ReadExpression, WriteExpression string }
	read := func() []policy {
		var result []policy
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT c.relname AS table_name,pg_get_expr(p.polqual,p.polrelid) AS read_expression,pg_get_expr(p.polwithcheck,p.polrelid) AS write_expression FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid WHERE p.polpermissive AND c.relname IN ('workforce_posting_proposals','workforce_posting_proposal_revisions','workforce_posting_supervision_intents') ORDER BY c.relname`).Scan(&result).Error
		}))
		require.Len(t, result, 3, "one permissive policy per affected table")
		return result
	}
	before := read()
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{migration}))
	after := read()
	for index, prior := range before {
		require.Equal(t, prior.TableName, after[index].TableName)
		require.Equal(t, prior.WriteExpression, after[index].WriteExpression, "discovery never broadens a write check")
		require.Contains(t, after[index].ReadExpression, "workforce_posting_supervisor_choices_context")
		if strings.Contains(prior.ReadExpression, "workforce_posting_supervisor_choices_context") {
			continue
		}
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			var guards []struct{ Command, Expression string }
			if err := tx.Raw(`SELECT p.polcmd::text AS command,pg_get_expr(p.polqual,p.polrelid) AS expression FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid WHERE NOT p.polpermissive AND c.relname=? AND p.polname IN ('supervisor_choices_update_guard','supervisor_choices_delete_guard') ORDER BY p.polcmd`, prior.TableName).Scan(&guards).Error; err != nil {
				return err
			}
			require.Len(t, guards, 2)
			require.Equal(t, "d", guards[0].Command)
			require.Equal(t, "w", guards[1].Command)
			for _, guard := range guards {
				require.NoError(t, tx.Exec("CREATE OR REPLACE TEMP VIEW supervisor_guard_expected AS SELECT ("+prior.ReadExpression+") AS allowed FROM public."+prior.TableName).Error)
				require.NoError(t, tx.Exec("CREATE OR REPLACE TEMP VIEW supervisor_guard_actual AS SELECT ("+guard.Expression+") AS allowed FROM public."+prior.TableName).Error)
				var parsed struct{ Expected, Actual string }
				require.NoError(t, tx.Raw("SELECT pg_get_viewdef('pg_temp.supervisor_guard_expected'::regclass,false) AS expected,pg_get_viewdef('pg_temp.supervisor_guard_actual'::regclass,false) AS actual").Scan(&parsed).Error)
				require.Equal(t, parsed.Expected, parsed.Actual, "Postgres-parsed guard preserves every prior read condition")
				require.NoError(t, tx.Exec("DROP VIEW pg_temp.supervisor_guard_expected,pg_temp.supervisor_guard_actual").Error)
			}
			return nil
		}))
	}
}
