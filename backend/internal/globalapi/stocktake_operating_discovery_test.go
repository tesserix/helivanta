package globalapi

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/testutil"
)

func TestStocktakeReviewedReadsDiscoverCurrentDutyLocations(t *testing.T) {
	f := newPharmacyReceiverFixture(t)
	f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
	counter := f.duty[len(f.duty)-36:]
	f.activate(t)
	f.duty = "/v1/iam/operating-duties/" + v7()
	f.input.TemplateKey = "pharmacy.stocktake_reader.v1"
	reader := f.duty[len(f.duty)-36:]
	f.activate(t)
	const path = "/v1/iam/me/pharmacy-stocktake-locations"
	type choice struct {
		OwnerTenantID       string   `json:"owner_tenant_id"`
		DutyID              string   `json:"duty_id"`
		EmployerID          string   `json:"employer_id"`
		LocationID          string   `json:"location_id"`
		EmployerName        string   `json:"employer_name"`
		LocationName        string   `json:"location_name"`
		LocationCode        string   `json:"location_code"`
		CountryCode         string   `json:"country_code"`
		TimeZone            string   `json:"time_zone"`
		TemplateKey         string   `json:"template_key"`
		TemplateLabel       string   `json:"template_label"`
		AssignedPermissions []string `json:"assigned_permissions"`
	}
	type page struct {
		Data []choice `json:"data"`
		Page struct {
			HasMore    bool    `json:"has_more"`
			NextCursor *string `json:"next_cursor"`
		} `json:"page"`
	}
	var first page
	require.NoError(t, json.Unmarshal([]byte(f.call("GET", path+"?limit=1", "receiver", nil, 200)), &first))
	require.Len(t, first.Data, 1)
	require.True(t, first.Page.HasMore)
	require.NotNil(t, first.Page.NextCursor)
	require.Equal(t, counter, first.Data[0].DutyID)
	require.Equal(t, "pharmacy.stocktake_counter.v1", first.Data[0].TemplateKey)
	require.Equal(t, "Outlet stocktake counter", first.Data[0].TemplateLabel)
	require.ElementsMatch(t, []string{"pharmacy.stocktake.read", "pharmacy.stocktake.count"}, first.Data[0].AssignedPermissions)
	for _, actor := range []string{"owner", "foreign"} {
		f.call("GET", path+"?limit=1&cursor="+url.QueryEscape(*first.Page.NextCursor), actor, nil, 400)
	}
	var second page
	require.NoError(t, json.Unmarshal([]byte(f.call("GET", path+"?limit=1&cursor="+url.QueryEscape(*first.Page.NextCursor), "receiver", nil, 200)), &second))
	require.Len(t, second.Data, 1)
	require.False(t, second.Page.HasMore)
	require.Nil(t, second.Page.NextCursor)
	require.Equal(t, reader, second.Data[0].DutyID)
	require.Equal(t, "pharmacy.stocktake_reader.v1", second.Data[0].TemplateKey)
	require.Equal(t, "Outlet stocktake reader", second.Data[0].TemplateLabel)
	require.Equal(t, []string{"pharmacy.stocktake.read"}, second.Data[0].AssignedPermissions)
	for _, row := range []choice{first.Data[0], second.Data[0]} {
		require.Equal(t, testutil.TenantA, row.OwnerTenantID)
		require.Equal(t, f.pharmacy, row.EmployerID)
		require.Equal(t, f.outlet, row.LocationID)
		require.Equal(t, "TEST Independent Pharmacy", row.EmployerName)
		require.Equal(t, "TEST TEST-RX-MAIN", row.LocationName)
		require.Equal(t, "TEST-RX-MAIN", row.LocationCode)
		require.Equal(t, "IN", row.CountryCode)
		require.Equal(t, "Asia/Kolkata", row.TimeZone)
	}
	for _, actor := range []string{"owner", "foreign"} {
		var empty page
		require.NoError(t, json.Unmarshal([]byte(f.call("GET", path, actor, nil, 200)), &empty))
		require.Empty(t, empty.Data)
		require.False(t, empty.Page.HasMore)
		require.Nil(t, empty.Page.NextCursor)
	}
	for _, query := range []string{"?limit=21", "?limit=1&limit=2", "?employer=" + f.pharmacy, "?", "?cursor="} {
		f.call("GET", path+query, "receiver", nil, 400)
	}
	f.checks.down.Store(true)
	unavailable := f.call("GET", path, "receiver", nil, 503)
	require.NotContains(t, unavailable, "TEST Independent Pharmacy")
	require.NotContains(t, unavailable, "TEST TEST-RX-MAIN")
}
