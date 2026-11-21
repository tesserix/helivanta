package pharmacy_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
)

func TestSupplierReturnCreditInitialSourcePickerNeedsFinanceNotPhysicalReturnAuthority(t *testing.T) {
	w, dispatched := dispatchedCreditSource(t, "independent")
	grantCatalogue(t, w.r, "user-finance", "editor")
	require.Equal(t, 403, w.request("GET", supplierReturnPath(dispatched), "finance", "").Code)
	pending := w.request("PUT", supplierReturnPath(uuid.Must(uuid.NewV7()).String()), "editor", w.body(1))
	require.Equal(t, 200, pending.Code, pending.Body.String())
	root := payableRoot() + "/supplier-credits/source-returns"
	choices := w.request("GET", root+"?limit=1", "finance", "")
	require.Equal(t, 200, choices.Code, choices.Body.String())
	var page struct {
		Data []pharmacy.SupplierCreditReturnChoice `json:"data"`
		Page struct {
			HasMore    bool    `json:"has_more"`
			NextCursor *string `json:"next_cursor"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(choices.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, dispatched, page.Data[0].ID.String())
	require.Positive(t, page.Data[0].Number)
	require.Equal(t, "TEST External Supplier", page.Data[0].PayeeName)
	require.Equal(t, "TEST-BILL-1", page.Data[0].OriginalBillReference)
	require.Equal(t, "INR", page.Data[0].Currency)
	require.Zero(t, page.Data[0].MatchedMinor)
	require.False(t, page.Page.HasMore)
	require.Nil(t, page.Page.NextCursor)
	created := w.request("PUT", creditPath(uuid.Must(uuid.NewV7()).String()), "finance", creditJSON(t, actualCreditInput(dispatched, "TEST-FINANCE-FIRST", 750, nil)))
	require.Equal(t, 200, created.Code, created.Body.String())
	var receipt pharmacy.SupplierCreditDocumentReceipt
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &receipt))
	require.Equal(t, "user-finance", receipt.RecordedBy)
	require.Equal(t, "TEST-FINANCE-FIRST", receipt.DocumentReference)
	require.Equal(t, dispatched, receipt.ReturnID.String())
	require.Equal(t, 403, w.request("GET", supplierReturnPath(dispatched), "finance", "").Code)
	for _, actor := range []string{"foreign", "sibling"} {
		require.Equal(t, 404, w.request("GET", root, actor, "").Code)
	}
	require.Equal(t, 400, w.request("GET", root+"?unknown=1", "finance", "").Code)
	require.Equal(t, 400, w.request("GET", root+"?limit=51", "finance", "").Code)
}
