package globalapi

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func assertHospitalStocktakeDepartmentChoices(t *testing.T, db *tenantdb.DB, call func(*testing.T, string, string, string, any, int) map[string]any, hospital, department, revision, owner, counter, reviewer string) (string, string) {
	t.Helper()
	child, childRevision, sibling := v7(), v7(), v7()
	departmentRoot := "/v1/organization/hospitals/" + hospital + "/departments/"
	input := func(code, name string, parent any) map[string]any {
		kind := "department"
		if parent != nil {
			kind = "sub_unit"
		}
		return map[string]any{"expected_version": 0, "code": code, "name": name, "kind": kind, "parent_id": parent, "status": "active", "reason": "TEST inherited stocktake department selection"}
	}
	call(t, "PUT", departmentRoot+child+"/revisions/"+childRevision, owner, input("TEST-COUNT-CHILD", "TEST Inherited counting department", department), 200)
	t.Cleanup(func() {
		retired := input("TEST-COUNT-CHILD", "TEST Inherited counting department", department)
		retired["expected_version"], retired["status"] = 1, "retired"
		call(t, "PUT", departmentRoot+child+"/revisions/"+v7(), owner, retired, 200)
	})
	call(t, "PUT", departmentRoot+sibling+"/revisions/"+v7(), owner, input("TEST-COUNT-FOREIGN", "TEST Unrelated counting department", nil), 200)
	t.Cleanup(func() {
		retired := input("TEST-COUNT-FOREIGN", "TEST Unrelated counting department", nil)
		retired["expected_version"], retired["status"] = 1, "retired"
		call(t, "PUT", departmentRoot+sibling+"/revisions/"+v7(), owner, retired, 200)
	})
	endpoint := "/v1/pharmacy/hospitals/" + hospital + "/departments/" + department + "/stocktake-department-choices"
	for _, actor := range []string{counter, reviewer} {
		seen := map[string]bool{}
		query := endpoint + "?limit=1"
		for pages := 0; ; pages++ {
			require.Less(t, pages, 5)
			page := call(t, "GET", query, actor, nil, 200)
			rows := page["data"].([]any)
			require.LessOrEqual(t, len(rows), 1)
			for _, raw := range rows {
				row := raw.(map[string]any)
				id := row["department_id"].(string)
				require.Contains(t, []string{department, child}, id)
				require.False(t, seen[id])
				seen[id] = true
				require.Equal(t, testutil.TenantA, row["owner_tenant_id"])
				require.Equal(t, hospital, row["hospital_id"])
				require.Equal(t, department, row["scope_department_id"])
				require.NotEmpty(t, row["department_name"])
				require.NotEmpty(t, row["department_code"])
				if id == child {
					require.Equal(t, childRevision, row["department_revision_id"])
					require.Equal(t, "TEST Inherited counting department", row["department_name"])
				} else {
					require.Equal(t, revision, row["department_revision_id"])
				}
				for _, private := range []string{"subject", "staff_id", "generation_id", "link_id", "identity_hash", "actor_subject", "reason"} {
					require.NotContains(t, row, private)
				}
			}
			continuation := page["page"].(map[string]any)
			if !continuation["has_more"].(bool) {
				break
			}
			cursor := continuation["next_cursor"].(string)
			for _, foreign := range []string{
				"/v1/pharmacy/hospitals/" + v7() + "/departments/" + department + "/stocktake-department-choices?cursor=" + url.QueryEscape(cursor),
				"/v1/pharmacy/hospitals/" + hospital + "/departments/" + sibling + "/stocktake-department-choices?cursor=" + url.QueryEscape(cursor),
			} {
				call(t, "GET", foreign, actor, nil, 400)
			}
			query = endpoint + "?limit=1&cursor=" + url.QueryEscape(cursor)
		}
		require.Len(t, seen, 2)
	}

	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_stocktake_department_disclosure_failure", SQL: `ALTER TABLE audit_events ADD CONSTRAINT test_stocktake_department_disclosure_failure CHECK(action<>'organization.stocktake_departments.read')NOT VALID;`}}))
	failed := call(t, "GET", endpoint, counter, nil, 503)
	require.NotContains(t, failed, "data")
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_stocktake_department_disclosure_restore", SQL: `ALTER TABLE audit_events DROP CONSTRAINT test_stocktake_department_disclosure_failure;`}}))
	require.Len(t, call(t, "GET", endpoint, counter, nil, 200)["data"].([]any), 2)
	call(t, "GET", endpoint, owner, nil, 404)
	call(t, "GET", endpoint+"?include_retired=true", counter, nil, 400)
	call(t, "GET", endpoint+"?limit=21", counter, nil, 400)
	call(t, "GET", endpoint+"?limit=1&limit=2", counter, nil, 400)
	call(t, "GET", "/v1/pharmacy/hospitals/"+hospital+"/departments/"+sibling+"/stocktake-department-choices", counter, nil, 404)
	return child, childRevision
}
