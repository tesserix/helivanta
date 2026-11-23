package globalapi

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func assertHospitalStocktakeScopeDiscovery(t *testing.T, call func(*testing.T, string, string, string, any, int) map[string]any, owner, counter, reviewer, hospital, department string) {
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
	ownerPage := call(t, "GET", endpoint, owner, nil, 200)
	require.Empty(t, ownerPage["data"], "tenant ownership alone must not manufacture a hospital Staff scope")
}
