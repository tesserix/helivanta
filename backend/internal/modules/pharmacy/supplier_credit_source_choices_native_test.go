package pharmacy_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
)

func TestSupplierReturnCreditSourceChoicesUseCurrentOwnerBalancesAndDate(t *testing.T) {
	w, source := dispatchedCreditSource(t, "independent")
	credit := recordSupplierCreditFixture(t, w, source, "TEST-SOURCE-CHOICES", 750)
	bill := futureSupplierCreditBill(t, w)
	contextRead := w.request("GET", credit+"/context", "reader", "")
	require.Equal(t, 200, contextRead.Code, contextRead.Body.String())
	var context pharmacy.SupplierCreditContext
	require.NoError(t, json.Unmarshal(contextRead.Body.Bytes(), &context))
	require.Equal(t, int64(1), context.Version)
	require.Equal(t, int64(750), context.AvailableMinor)
	require.Equal(t, "Asia/Kolkata", context.Timezone)
	at, err := time.Parse(time.RFC3339Nano, context.AsOf)
	require.NoError(t, err)
	zone, err := time.LoadLocation(context.Timezone)
	require.NoError(t, err)
	require.Equal(t, at.In(zone).Format(time.DateOnly), context.LocalDate)
	choices := w.request("GET", credit+"/eligible-payables?limit=1", "reader", "")
	require.Equal(t, 200, choices.Code, choices.Body.String())
	var page struct {
		Data       []pharmacy.SupplierCreditPayableChoice `json:"data"`
		NextCursor *string                                `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(choices.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, bill, page.Data[0].ID.String())
	require.Equal(t, "TEST-FUTURE-BILL", page.Data[0].DocumentReference)
	require.Equal(t, "TEST External Supplier", page.Data[0].PayeeName)
	require.Equal(t, int64(1180), page.Data[0].OutstandingMinor)
	require.Equal(t, int64(1), page.Data[0].Version)
	returns := w.request("GET", credit+"/eligible-returns", "reader", "")
	require.Equal(t, 200, returns.Code, returns.Body.String())
	var returnPage struct {
		Data []pharmacy.SupplierCreditReturnChoice `json:"data"`
	}
	require.NoError(t, json.Unmarshal(returns.Body.Bytes(), &returnPage))
	require.Len(t, returnPage.Data, 1)
	require.Equal(t, source, returnPage.Data[0].ID.String())
	require.Equal(t, "TEST External Supplier", returnPage.Data[0].PayeeName)
	require.Zero(t, returnPage.Data[0].MatchedMinor)
	for _, route := range []string{"/context", "/eligible-payables", "/eligible-returns"} {
		for _, actor := range []string{"foreign", "sibling"} {
			require.Equal(t, 404, w.request("GET", credit+route, actor, "").Code)
		}
	}
	for _, route := range []string{"/eligible-payables", "/eligible-returns"} {
		require.Equal(t, 400, w.request("GET", credit+route+"?limit=51", "reader", "").Code)
		require.Equal(t, 400, w.request("GET", credit+route+"?unknown=1", "reader", "").Code)
	}
}
