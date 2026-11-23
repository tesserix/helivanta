package globalapi

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func assertHospitalStocktakeOutletDiscovery(t *testing.T, call func(*testing.T, string, string, string, any, int) map[string]any, hospital, department, revision, pharmacy, outlet, owner, counter, reviewer string, binding map[string]any) {
	t.Helper()
	endpoint := "/v1/pharmacy/hospitals/" + hospital + "/departments/" + department + "/stocktake-outlet-choices?limit=20"
	for _, subject := range []string{counter, reviewer} {
		page := call(t, "GET", endpoint, subject, nil, 200)
		rows := page["data"].([]any)
		require.Len(t, rows, 1, "a worker with no legacy outlet access discovers the actual approved binding")
		row := rows[0].(map[string]any)
		for key, value := range map[string]any{"hospital_id": hospital, "department_id": department, "department_revision_id": revision, "pharmacy_id": pharmacy, "outlet_id": outlet, "binding_id": binding["id"], "binding_version": binding["version"], "pharmacy_name": "Synthetic hospital stocktake pharmacy", "outlet_name": "Synthetic hospital counting outlet", "outlet_code": "SYN-HOSPITAL-COUNT", "country_code": "IN", "time_zone": "Asia/Kolkata"} {
			require.Equal(t, value, row[key], key)
		}
		for _, private := range []string{"subject", "staff_id", "link_id", "generation_id", "employment_epoch", "email", "account_id", "stock_units", "products"} {
			require.NotContains(t, row, private)
		}
		one := call(t, "GET", strings.Replace(endpoint, "limit=20", "limit=1", 1), subject, nil, 200)
		require.Equal(t, rows, one["data"])
		require.Equal(t, false, one["page"].(map[string]any)["has_more"])
	}
	call(t, "GET", endpoint, owner, nil, 404)
	call(t, "GET", strings.Replace(endpoint, department, v7(), 1), counter, nil, 404)
	call(t, "GET", strings.Replace(endpoint, hospital, v7(), 1), counter, nil, 404)
	call(t, "GET", endpoint+"&limit=1", counter, nil, 400)
	call(t, "GET", endpoint+"&subject="+reviewer, counter, nil, 400)
}
