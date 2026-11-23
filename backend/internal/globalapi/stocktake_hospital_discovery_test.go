package globalapi

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func assertHospitalStocktakeScopeDiscovery(t *testing.T, db *tenantdb.DB, call func(*testing.T, string, string, string, any, int) map[string]any, owner, counter, reviewer, hospital, department string) {
	t.Helper()
	endpoint := "/v1/iam/me/hospital-pharmacy-stocktake-scopes?limit=20"
	for _, person := range []struct {
		subject    string
		operations []string
	}{
		{counter, []string{"pharmacy.stocktake.count", "pharmacy.stocktake.review"}},
		{reviewer, []string{"pharmacy.stocktake.review"}},
	} {
		page := call(t, "GET", endpoint, person.subject, nil, 200)
		rows, ok := page["data"].([]any)
		require.True(t, ok, "hospital scope discovery must return a real bounded collection")
		matches := map[string]int{}
		for _, raw := range rows {
			row := raw.(map[string]any)
			for _, private := range []string{"subject", "staff_id", "link_id", "generation_id", "employment_epoch", "email", "account_id"} {
				require.NotContains(t, row, private)
			}
			if row["hospital_id"] != hospital || row["department_id"] != department {
				continue
			}
			require.NotEmpty(t, row["id"], "the choice references its actual current assignment")
			for _, field := range []string{"hospital_name", "department_name", "template_label"} {
				value, ok := row[field].(string)
				require.True(t, ok, field)
				require.NotEmpty(t, strings.TrimSpace(value), field)
				require.NotEqual(t, hospital, value, "a display name cannot fall back to a resource ID")
				require.NotEqual(t, department, value, "a display name cannot fall back to a resource ID")
			}
			require.NotEqual(t, row["template_key"], row["template_label"], "show the actual role label")
			permissions := row["assigned_permissions"].([]any)
			for _, operation := range person.operations {
				for _, permission := range permissions {
					if permission == operation {
						require.ElementsMatch(t, []any{"pharmacy.stocktake.read", operation, "workforce.stocktake_actor.use"}, permissions)
						matches[operation]++
					}
				}
			}
		}
		for _, operation := range person.operations {
			require.Equal(t, 1, matches[operation], "each actual counter/reviewer assignment needs a named choice")
		}
	}

	first := call(t, "GET", endpoint+"&limit=1", counter, nil, 400)
	require.NotEmpty(t, first, "duplicate pagination keys must not become an unbounded query")
	pagePath := "/v1/iam/me/hospital-pharmacy-stocktake-scopes?limit=1"
	seen := map[string]bool{}
	for attempt := 0; ; attempt++ {
		require.Less(t, attempt, 10, "scope discovery must make bounded keyset progress")
		page := call(t, "GET", pagePath, counter, nil, 200)
		rows := page["data"].([]any)
		require.LessOrEqual(t, len(rows), 1)
		for _, raw := range rows {
			id := raw.(map[string]any)["id"].(string)
			require.False(t, seen[id], "paging must not repeat an assignment")
			seen[id] = true
		}
		state := page["page"].(map[string]any)
		if !state["has_more"].(bool) {
			break
		}
		cursor := state["next_cursor"].(string)
		require.NotEmpty(t, cursor)
		pagePath = "/v1/iam/me/hospital-pharmacy-stocktake-scopes?limit=1&cursor=" + url.QueryEscape(cursor)
		call(t, "GET", pagePath, reviewer, nil, 400)
	}
	require.GreaterOrEqual(t, len(seen), 2, "both counter assignments must survive separate pages")
	call(t, "GET", endpoint+"&subject="+url.QueryEscape(reviewer), counter, nil, 400)

	audits := func() int64 {
		var n int64
		require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), owner), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM workforce_audit WHERE tenant_id=? AND hospital_id=? AND actor_subject=? AND action IN('workforce.stocktake_actor.use','workforce.stocktake_hospital_name.read')`, testutil.TenantA, hospital, counter).Scan(&n).Error
		}))
		return n
	}
	before := audits()
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_hospital_stocktake_discovery_audit_failure", SQL: `CREATE FUNCTION test_hospital_stocktake_discovery_audit_failure()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.action='iam.hospital_stocktake_scope.read'THEN RAISE EXCEPTION 'TEST private discovery audit failure';END IF;RETURN NEW;END$$;CREATE TRIGGER test_hospital_stocktake_discovery_audit_failure BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION test_hospital_stocktake_discovery_audit_failure();`}}))
	failed := call(t, "GET", endpoint, counter, nil, 503)
	require.NotContains(t, failed, "data", "a disclosure failure must not return a partial named page")
	require.Equal(t, before, audits(), "failed disclosure rolls back its Staff source and name-read evidence")
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_hospital_stocktake_discovery_audit_restore", SQL: `DROP TRIGGER test_hospital_stocktake_discovery_audit_failure ON audit_events;DROP FUNCTION test_hospital_stocktake_discovery_audit_failure();`}}))
	restored := call(t, "GET", endpoint, counter, nil, 200)
	require.NotEmpty(t, restored["data"])
	require.Greater(t, audits(), before)
	ownerPage := call(t, "GET", endpoint, owner, nil, 200)
	require.Empty(t, ownerPage["data"], "tenant ownership alone must not manufacture a hospital Staff scope")
}
