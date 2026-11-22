package globalapi

import (
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStocktakeReviewedReadsAndRegisterStayWithinReviewedHistory(t *testing.T) {
	f, path := newSubmittedReviewedStocktake(t)
	root := path[:len(path)-37]
	readHeader := func(state string, version int64) {
		t.Helper()
		var header struct {
			State          string `json:"state"`
			Version        int64  `json:"version"`
			CountedRows    int64  `json:"counted_rows"`
			DifferenceRows int64  `json:"difference_rows"`
		}
		require.NoError(t, json.Unmarshal([]byte(f.call("GET", path, "receiver", nil, 200)), &header))
		require.Equal(t, state, header.State)
		require.Equal(t, version, header.Version)
		require.EqualValues(t, 1, header.CountedRows)
		require.EqualValues(t, 1, header.DifferenceRows)
	}
	readHeader("submitted", 2)
	for _, suffix := range []string{"", "/lines", "/register.csv"} {
		f.call("GET", path+suffix, "owner", nil, 404)
		f.call("GET", path+suffix, "foreign", nil, 404)
		f.call("GET", strings.Replace(path, f.outlet, f.sibling, 1)+suffix, "receiver", nil, 404)
	}
	f.call("GET", path+"?limit=1", "receiver", nil, 400)
	f.call("GET", path+"/register.csv?limit=1", "receiver", nil, 400)
	f.call("GET", root+"?scope=all", "receiver", nil, 400)
	activateReviewedStocktakeReviewer(t, &f)
	f.call("PUT", path+"/decisions/"+v7(), "stock-reviewer", map[string]any{"decision": "approve", "expected_version": 2, "reason": "TEST independently reviewed register", "extra_units_disposition": "no_extra_units"}, 200)
	readHeader("posted", 3)
	register := f.call("GET", path+"/register.csv", "receiver", nil, 200)
	rows, err := csv.NewReader(strings.NewReader(register)).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	values := make(map[string]string, len(rows[0]))
	for i, column := range rows[0] {
		values[column] = rows[1][i]
	}
	require.Equal(t, "posted", values["stocktake_state"])
	require.Equal(t, "30", values["book_units"])
	require.Equal(t, "27", values["counted_units"])
	require.Equal(t, "-3", values["adjustment_units"])
	require.Equal(t, "user-stock-reviewer", values["approved_by_reference"])
	require.NotEmpty(t, values["adjustment_movement_id"])
	legacy := v7()
	f.call("PUT", "/v1/pharmacy/pharmacies/"+f.pharmacy+"/outlets/"+f.outlet+"/stock/stocktakes/"+legacy, "owner", map[string]any{"selection": "all", "product_ids": []string{}, "quarantine_location_id": f.quarantine, "reason": "TEST legacy history stays separate"}, 200)
	for _, suffix := range []string{"", "/lines", "/register.csv"} {
		f.call("GET", root+"/"+legacy+suffix, "receiver", nil, 404)
	}
	var page struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(f.call("GET", root+"?limit=1", "receiver", nil, 200)), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, path[len(path)-36:], page.Data[0].ID)
	f.checks.down.Store(true)
	for _, read := range []string{path, root, path + "/register.csv"} {
		unavailable := f.call("GET", read, "receiver", nil, 503)
		require.NotContains(t, unavailable, "TEST-REVIEW-PARTICIPANT")
		require.NotContains(t, unavailable, "user-stock-reviewer")
	}
	f.checks.down.Store(false)
	require.Equal(t, register, f.call("GET", path+"/register.csv", "receiver", nil, 200))
}
