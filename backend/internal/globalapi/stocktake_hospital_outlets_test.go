package globalapi

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
)

func assertHospitalStocktakeOutletDiscovery(t *testing.T, call func(*testing.T, string, string, string, any, int) map[string]any, hospital, department, revision, pharmacy, outlet, owner, counter, reviewer string, binding map[string]any) []any {
	t.Helper()
	endpoint := "/v1/pharmacy/hospitals/" + hospital + "/departments/" + department + "/stocktake-outlet-choices?limit=20"
	var survivors []any
	for _, subject := range []string{counter, reviewer} {
		page := call(t, "GET", endpoint, subject, nil, 200)
		rows := page["data"].([]any)
		require.Len(t, rows, 2, "the prior approved hospital outlet and the new counting outlet are both discoverable")
		var target map[string]any
		for _, raw := range rows {
			row := raw.(map[string]any)
			for key, value := range map[string]any{"owner_tenant_id": testutil.TenantA, "hospital_id": hospital, "department_id": department, "department_revision_id": revision, "country_code": "IN", "time_zone": "Asia/Kolkata"} {
				require.Equal(t, value, row[key], key)
			}
			require.NotEmpty(t, row["pharmacy_name"])
			require.NotEmpty(t, row["outlet_name"])
			require.NotEqual(t, row["pharmacy_id"], row["pharmacy_name"])
			require.NotEqual(t, row["outlet_id"], row["outlet_name"])
			for _, private := range []string{"subject", "staff_id", "link_id", "generation_id", "employment_epoch", "email", "account_id", "stock_units", "products"} {
				require.NotContains(t, row, private)
			}
			if row["binding_id"] == binding["id"] {
				require.Nil(t, target, "a binding appears at most once")
				target = row
			} else if subject == counter {
				survivors = append(survivors, row)
			}
		}
		require.NotNil(t, target)
		for key, value := range map[string]any{"pharmacy_id": pharmacy, "outlet_id": outlet, "binding_id": binding["id"], "binding_version": binding["version"], "pharmacy_name": "Synthetic hospital stocktake pharmacy", "outlet_name": "Synthetic hospital counting outlet", "outlet_code": "SYN-HOSPITAL-COUNT"} {
			require.Equal(t, value, target[key], key)
		}
		path := strings.Replace(endpoint, "limit=20", "limit=1", 1)
		seen := map[string]bool{}
		paged := []any{}
		for attempt := 0; ; attempt++ {
			require.Less(t, attempt, 4, "outlet pages must make bounded keyset progress")
			next := call(t, "GET", path, subject, nil, 200)
			chunk := next["data"].([]any)
			require.LessOrEqual(t, len(chunk), 1)
			for _, raw := range chunk {
				id := raw.(map[string]any)["binding_id"].(string)
				require.False(t, seen[id], "one-row pages must not repeat or lose a binding")
				seen[id] = true
				paged = append(paged, raw)
			}
			state := next["page"].(map[string]any)
			if !state["has_more"].(bool) {
				break
			}
			cursor := url.QueryEscape(state["next_cursor"].(string))
			path = strings.Replace(endpoint, "limit=20", "limit=1", 1) + "&cursor=" + cursor
			if subject == counter {
				call(t, "GET", path, reviewer, nil, 400)
				call(t, "GET", strings.Replace(path, hospital, v7(), 1), subject, nil, 400)
				call(t, "GET", strings.Replace(path, department, v7(), 1), subject, nil, 400)
			}
		}
		require.ElementsMatch(t, rows, paged, "every approved outlet survives independent pages")
	}
	require.Len(t, survivors, 1)
	call(t, "GET", endpoint, owner, nil, 404)
	call(t, "GET", strings.Replace(endpoint, department, v7(), 1), counter, nil, 404)
	call(t, "GET", strings.Replace(endpoint, hospital, v7(), 1), counter, nil, 404)
	call(t, "GET", endpoint+"&limit=1", counter, nil, 400)
	call(t, "GET", endpoint+"&subject="+reviewer, counter, nil, 400)
	return survivors
}
