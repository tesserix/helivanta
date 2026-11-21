package pharmacy_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
)

func TestSupplierReturnCreditAllocationContextIncludesPaidBillAndCurrentVersions(t *testing.T) {
	w, source := dispatchedCreditSource(t, "independent")
	credit := recordSupplierCreditFixture(t, w, source, "TEST-PAID-CONTEXT", 750)
	bill := futureSupplierCreditBill(t, w)
	allocation := credit + "/allocations/" + uuid.Must(uuid.NewV7()).String()
	created := w.request("PUT", allocation, "editor", creditJSON(t, allocationIntent(bill, 1, 750, 1, 1180, 750)))
	require.Equal(t, 200, created.Code, created.Body.String())
	paid := do(w.r, "PUT", payableRoot()+"/payables/"+bill+"/payments/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"kind":"payment","amount_minor":430,"currency":"INR","paid_by":"TEST Finance operator","method":"bank","source_reference":"TEST-PAID-CONTEXT","occurred_on":"2026-11-19","reason":"TEST finish payment before reversal context","reversal_of":null}`)
	require.Equal(t, 200, paid.Code, paid.Body.String())
	read := w.request("GET", allocation+"/context", "reader", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	var context struct {
		Credit     pharmacy.SupplierCreditContext        `json:"credit"`
		Allocation pharmacy.SupplierCreditAllocationView `json:"allocation"`
		Payable    struct {
			ID                uuid.UUID `json:"id"`
			OutletID          uuid.UUID `json:"outlet_id"`
			Version           int64     `json:"version"`
			DocumentReference string    `json:"document_reference"`
			Paid              int64     `json:"paid_minor"`
			Credit            int64     `json:"credit_minor"`
			Outstanding       int64     `json:"outstanding_minor"`
		} `json:"payable"`
	}
	require.NoError(t, json.Unmarshal(read.Body.Bytes(), &context))
	require.Equal(t, int64(2), context.Credit.Version)
	require.Zero(t, context.Credit.AvailableMinor)
	require.Equal(t, int64(750), context.Credit.AllocatedMinor)
	require.NotEmpty(t, context.Credit.LocalDate)
	require.Equal(t, "Asia/Kolkata", context.Credit.Timezone)
	require.Equal(t, bill, context.Payable.ID.String())
	require.Equal(t, context.Allocation.OutletID, context.Payable.OutletID)
	require.Equal(t, "TEST-FUTURE-BILL", context.Payable.DocumentReference)
	require.Equal(t, int64(3), context.Payable.Version)
	require.Equal(t, int64(430), context.Payable.Paid)
	require.Equal(t, int64(750), context.Payable.Credit)
	require.Zero(t, context.Payable.Outstanding)
	require.Equal(t, "user-editor", context.Allocation.RecordedBy)
	require.Equal(t, int64(2), context.Allocation.PayableVersion)
	require.Nil(t, context.Allocation.ReversalID)
	reversed := w.request("PUT", allocation+"/reversals/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, allocationReversalIntent(2, 0, 3, 0)))
	require.Equal(t, 200, reversed.Code, reversed.Body.String())
	after := w.request("GET", allocation+"/context", "reader", "")
	require.Equal(t, 200, after.Code, after.Body.String())
	require.NoError(t, json.Unmarshal(after.Body.Bytes(), &context))
	require.Equal(t, int64(3), context.Credit.Version)
	require.Equal(t, int64(750), context.Credit.AvailableMinor)
	require.Equal(t, int64(4), context.Payable.Version)
	require.Equal(t, int64(430), context.Payable.Paid)
	require.Zero(t, context.Payable.Credit)
	require.Equal(t, int64(750), context.Payable.Outstanding)
	require.Equal(t, int64(2), context.Allocation.PayableVersion)
	require.NotNil(t, context.Allocation.ReversalID)
	for _, actor := range []string{"foreign", "sibling"} {
		require.Equal(t, 404, w.request("GET", allocation+"/context", actor, "").Code)
	}
	other := recordSupplierCreditFixture(t, w, source, "TEST-OTHER-CONTEXT", 100)
	require.Equal(t, 404, w.request("GET", other+"/allocations/"+context.Allocation.ID.String()+"/context", "reader", "").Code)
	require.Equal(t, 400, w.request("GET", allocation+"/context?unknown=1", "reader", "").Code)
}
