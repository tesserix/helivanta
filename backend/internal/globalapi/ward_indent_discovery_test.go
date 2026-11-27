package globalapi

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func assertHospitalWardIndentScopeDiscovery(t *testing.T, db *tenantdb.DB, call func(*testing.T, string, string, string, any, int) map[string]any, owner, reader, requester, hospital, department string) {
	t.Helper()
	const endpoint = "/v1/iam/me/hospital-ward-indent-scopes?limit=20"
	expected := map[string][]any{
		reader:    {"pharmacy.indent.read", "workforce.ward_indent_actor.use"},
		requester: {"pharmacy.indent.read", "pharmacy.indent.raise", "workforce.ward_indent_actor.use"},
	}
	for subject, permissions := range expected {
		rows := call(t, "GET", endpoint, subject, nil, 200)["data"].([]any)
		matched := 0
		for _, raw := range rows {
			row := raw.(map[string]any)
			require.True(t, strings.HasPrefix(row["template_key"].(string), "pharmacy.department_ward_indent_"), "only ward indent roles are ward indent scopes")
			for _, private := range []string{"subject", "staff_id", "link_id", "generation_id", "employment_epoch", "email", "account_id"} {
				require.NotContains(t, row, private)
			}
			if row["hospital_id"] != hospital || row["department_id"] != department {
				continue
			}
			for _, field := range []string{"hospital_name", "department_name", "template_label"} {
				value, ok := row[field].(string)
				require.True(t, ok, field)
				require.NotEmpty(t, strings.TrimSpace(value), field)
				require.NotEqual(t, hospital, value, "a display name cannot fall back to a resource ID")
				require.NotEqual(t, department, value, "a display name cannot fall back to a resource ID")
			}
			require.ElementsMatch(t, permissions, row["assigned_permissions"])
			matched++
		}
		require.Equal(t, 1, matched, "each actual ward indent assignment needs one named choice")
		stocktake := call(t, "GET", "/v1/iam/me/hospital-pharmacy-stocktake-scopes?limit=20", subject, nil, 200)["data"].([]any)
		for _, raw := range stocktake {
			require.NotContains(t, raw.(map[string]any)["template_key"], "ward_indent", "a ward indent role is never a stocktake scope")
		}
	}
	call(t, "GET", endpoint+"&limit=1", requester, nil, 400)
	call(t, "GET", endpoint+"&subject="+reader, requester, nil, 400)

	audits := func() int64 {
		var n int64
		require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), owner), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM workforce_audit WHERE tenant_id=? AND hospital_id=? AND actor_subject=? AND action IN('workforce.ward_indent_actor.use','workforce.ward_indent_hospital_name.read')`, testutil.TenantA, hospital, requester).Scan(&n).Error
		}))
		return n
	}
	before := audits()
	require.Positive(t, before, "a named ward indent scope records its Staff source and name read")
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_hospital_ward_indent_discovery_audit_failure", SQL: `CREATE FUNCTION test_hospital_ward_indent_discovery_audit_failure()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.action='iam.hospital_ward_indent_scope.read'THEN RAISE EXCEPTION 'TEST private discovery audit failure';END IF;RETURN NEW;END$$;CREATE TRIGGER test_hospital_ward_indent_discovery_audit_failure BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION test_hospital_ward_indent_discovery_audit_failure();`}}))
	failed := call(t, "GET", endpoint, requester, nil, 503)
	require.NotContains(t, failed, "data", "a disclosure failure must not return a partial named page")
	require.Equal(t, before, audits(), "failed disclosure rolls back its Staff source and name-read evidence")
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_hospital_ward_indent_discovery_audit_restore", SQL: `DROP TRIGGER test_hospital_ward_indent_discovery_audit_failure ON audit_events;DROP FUNCTION test_hospital_ward_indent_discovery_audit_failure();`}}))
	require.NotEmpty(t, call(t, "GET", endpoint, requester, nil, 200)["data"])
	require.Greater(t, audits(), before)
	require.Empty(t, call(t, "GET", endpoint, owner, nil, 200)["data"], "tenant ownership alone must not manufacture a ward indent scope")

	listed := call(t, "GET", "/v1/iam/pharmacy-ward-indent-assignment-proposals?limit=50", owner, nil, 200)["data"].([]any)
	keys := map[string]bool{}
	for _, raw := range listed {
		key := raw.(map[string]any)["input"].(map[string]any)["template_key"].(string)
		require.True(t, strings.HasPrefix(key, "pharmacy.department_ward_indent_"), "the family list holds only ward indent proposals")
		keys[key] = true
	}
	require.Equal(t, map[string]bool{"pharmacy.department_ward_indent_reader.v1": true, "pharmacy.department_ward_indent_requester.v1": true}, keys)
}
