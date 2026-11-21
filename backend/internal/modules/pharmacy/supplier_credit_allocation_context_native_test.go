package pharmacy_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/pkg/tenantdb"
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
	var observed pharmacy.SupplierCreditAllocationContext
	require.NoError(t, json.Unmarshal(read.Body.Bytes(), &observed))
	require.Equal(t, int64(2), observed.Credit.Version)
	require.Zero(t, observed.Credit.AvailableMinor)
	require.Equal(t, int64(750), observed.Credit.AllocatedMinor)
	require.NotEmpty(t, observed.Credit.LocalDate)
	require.Equal(t, "Asia/Kolkata", observed.Credit.Timezone)
	require.Equal(t, bill, observed.Payable.ID.String())
	require.Equal(t, observed.Allocation.OutletID, observed.Payable.OutletID)
	require.Equal(t, "TEST-FUTURE-BILL", observed.Payable.DocumentReference)
	require.Equal(t, "INR", observed.Payable.Currency)
	require.Equal(t, "2026-11-17", observed.Payable.IssuedOn)
	require.Equal(t, "2026-11-30", observed.Payable.DueOn)
	require.Equal(t, int64(1180), observed.Payable.TotalMinor)
	require.Equal(t, int64(3), observed.Payable.Version)
	require.Equal(t, int64(430), observed.Payable.PaidMinor)
	require.Equal(t, int64(750), observed.Payable.CreditMinor)
	require.Zero(t, observed.Payable.OutstandingMinor)
	require.Equal(t, "user-editor", observed.Allocation.RecordedBy)
	require.Equal(t, int64(2), observed.Allocation.PayableVersion)
	require.Nil(t, observed.Allocation.ReversalID)
	reversed := w.request("PUT", allocation+"/reversals/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, allocationReversalIntent(2, 0, 3, 0)))
	require.Equal(t, 200, reversed.Code, reversed.Body.String())
	after := w.request("GET", allocation+"/context", "reader", "")
	require.Equal(t, 200, after.Code, after.Body.String())
	require.NoError(t, json.Unmarshal(after.Body.Bytes(), &observed))
	require.Equal(t, int64(3), observed.Credit.Version)
	require.Equal(t, int64(750), observed.Credit.AvailableMinor)
	require.Equal(t, int64(4), observed.Payable.Version)
	require.Equal(t, int64(430), observed.Payable.PaidMinor)
	require.Zero(t, observed.Payable.CreditMinor)
	require.Equal(t, int64(750), observed.Payable.OutstandingMinor)
	require.Equal(t, int64(2), observed.Allocation.PayableVersion)
	require.NotNil(t, observed.Allocation.ReversalID)
	voided := w.request("PUT", credit+"/voids/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, map[string]any{"expected_version": 3, "reason": "TEST void after full allocation reversal"}))
	require.Equal(t, 200, voided.Code, voided.Body.String())
	retained := w.request("GET", allocation+"/context", "reader", "")
	require.Equal(t, 200, retained.Code, retained.Body.String())
	require.NoError(t, json.Unmarshal(retained.Body.Bytes(), &observed))
	require.Equal(t, "voided", observed.Credit.State)
	require.Equal(t, int64(4), observed.Credit.Version)
	require.Zero(t, observed.Credit.AvailableMinor)
	require.Equal(t, int64(4), observed.Payable.Version)
	require.Equal(t, int64(750), observed.Payable.OutstandingMinor)
	require.Equal(t, int64(2), observed.Allocation.PayableVersion)
	require.NotNil(t, observed.Allocation.ReversalID)
	apply := func(sql string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		require.NoError(t, w.db.Migrate(ctx, []tenantdb.Migration{{ID: "test_allocation_context_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}
	clear := func() {
		apply("ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS synthetic_credit_allocation_context_read")
	}
	t.Cleanup(clear)
	apply("ALTER TABLE audit_events ADD CONSTRAINT synthetic_credit_allocation_context_read CHECK(resource_type<>'pharmacy_supplier_credit_allocation_context') NOT VALID")
	failed := w.request("GET", allocation+"/context", "reader", "")
	require.Equal(t, 503, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "TEST-PAID-CONTEXT")
	require.NotContains(t, failed.Body.String(), "TEST-FUTURE-BILL")
	require.NotContains(t, failed.Body.String(), "TEST External Supplier")
	clear()
	require.Equal(t, 200, w.request("GET", allocation+"/context", "reader", "").Code)
	for _, actor := range []string{"foreign", "sibling"} {
		require.Equal(t, 404, w.request("GET", allocation+"/context", actor, "").Code)
	}
	other := recordSupplierCreditFixture(t, w, source, "TEST-OTHER-CONTEXT", 100)
	require.Equal(t, 404, w.request("GET", other+"/allocations/"+observed.Allocation.ID.String()+"/context", "reader", "").Code)
	require.Equal(t, 400, w.request("GET", allocation+"/context?unknown=1", "reader", "").Code)
}
