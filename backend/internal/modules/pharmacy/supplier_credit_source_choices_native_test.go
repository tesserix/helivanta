package pharmacy_test

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestSupplierReturnCreditSourceChoicesUseCurrentOwnerBalancesAndDate(t *testing.T) {
	w, source := dispatchedCreditSource(t, "independent")
	credit := recordSupplierCreditFixture(t, w, source, "TEST-SOURCE-CHOICES", 750)
	bill := futureSupplierCreditBill(t, w)
	newestBill := futureSupplierCreditBill(t, w, "TEST-FUTURE-BILL-2")
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
		Data []pharmacy.SupplierCreditPayableChoice `json:"data"`
		Page struct {
			HasMore    bool    `json:"has_more"`
			NextCursor *string `json:"next_cursor"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(choices.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, newestBill, page.Data[0].ID.String())
	require.Equal(t, "TEST-FUTURE-BILL-2", page.Data[0].DocumentReference)
	require.Equal(t, "TEST External Supplier", page.Data[0].PayeeName)
	require.Equal(t, int64(1180), page.Data[0].OutstandingMinor)
	require.Equal(t, int64(1), page.Data[0].Version)
	require.True(t, page.Page.HasMore)
	require.NotNil(t, page.Page.NextCursor)
	next := w.request("GET", credit+"/eligible-payables?limit=1&cursor="+url.QueryEscape(*page.Page.NextCursor), "reader", "")
	require.Equal(t, 200, next.Code, next.Body.String())
	require.NoError(t, json.Unmarshal(next.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, bill, page.Data[0].ID.String())
	require.True(t, page.Page.HasMore)
	require.NotNil(t, page.Page.NextCursor)
	third := w.request("GET", credit+"/eligible-payables?limit=1&cursor="+url.QueryEscape(*page.Page.NextCursor), "reader", "")
	require.Equal(t, 200, third.Code, third.Body.String())
	require.NoError(t, json.Unmarshal(third.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, w.bill, page.Data[0].ID.String())
	require.False(t, page.Page.HasMore)
	require.Nil(t, page.Page.NextCursor)
	payment := do(w.r, "PUT", payableRoot()+"/payables/"+w.bill+"/payments/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"kind":"payment","amount_minor":11800,"currency":"INR","paid_by":"TEST Finance operator","method":"bank","source_reference":"TEST-CHOICE-SETTLED-ORIGINAL","occurred_on":"2026-11-15","reason":"TEST paid bill excluded from choices","reversal_of":null}`)
	require.Equal(t, 200, payment.Code, payment.Body.String())
	filtered := w.request("GET", credit+"/eligible-payables?limit=50", "reader", "")
	require.Equal(t, 200, filtered.Code, filtered.Body.String())
	require.NoError(t, json.Unmarshal(filtered.Body.Bytes(), &page))
	require.Len(t, page.Data, 2)
	require.ElementsMatch(t, []string{bill, newestBill}, []string{page.Data[0].ID.String(), page.Data[1].ID.String()})
	require.False(t, page.Page.HasMore)
	require.Nil(t, page.Page.NextCursor)
	allocated := w.request("PUT", credit+"/allocations/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, allocationIntent(newestBill, 1, 750, 1, 1180, 500)))
	require.Equal(t, 200, allocated.Code, allocated.Body.String())
	refreshed := w.request("GET", credit+"/eligible-payables?limit=1", "reader", "")
	require.Equal(t, 200, refreshed.Code, refreshed.Body.String())
	require.NoError(t, json.Unmarshal(refreshed.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, newestBill, page.Data[0].ID.String())
	require.Equal(t, int64(2), page.Data[0].Version)
	require.Equal(t, int64(680), page.Data[0].OutstandingMinor)
	require.Equal(t, int64(500), page.Data[0].CreditMinor)

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
	require.Positive(t, returnPage.Data[0].Number)
	require.NotEmpty(t, page.Data[0].DueOn)
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

func TestSupplierReturnCreditSourceChoicesAuditFailureDoesNotDisclose(t *testing.T) {
	w, source := dispatchedCreditSource(t, "independent")
	credit := recordSupplierCreditFixture(t, w, source, "TEST-CHOICE-PRIVATE", 750)
	futureSupplierCreditBill(t, w)
	apply := func(sql string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		require.NoError(t, w.db.Migrate(ctx, []tenantdb.Migration{{ID: "test_source_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}
	clear := func() {
		apply("ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS synthetic_credit_source_read_failure")
	}
	t.Cleanup(clear)
	apply("ALTER TABLE audit_events ADD CONSTRAINT synthetic_credit_source_read_failure CHECK(resource_type NOT IN('pharmacy_supplier_credit_context','pharmacy_supplier_credit_payable_choices','pharmacy_supplier_credit_return_choices')) NOT VALID")
	for _, suffix := range []string{"/context", "/eligible-payables", "/eligible-returns"} {
		failed := w.request("GET", credit+suffix, "reader", "")
		require.Equal(t, 503, failed.Code, failed.Body.String())
		require.NotContains(t, failed.Body.String(), "TEST-CHOICE-PRIVATE")
		require.NotContains(t, failed.Body.String(), "TEST External Supplier")
		require.NotContains(t, failed.Body.String(), "TEST-FUTURE-BILL")
	}
	clear()
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Raw("SELECT count(*) FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE c.relname IN('pharmacy_supplier_credit_payable_choices','pharmacy_supplier_credit_return_choices') AND i.indisvalid AND i.indisready").Scan(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 2, count)
		return nil
	}))
	for _, suffix := range []string{"/context", "/eligible-payables", "/eligible-returns"} {
		read := w.request("GET", credit+suffix, "reader", "")
		require.Equal(t, 200, read.Code, read.Body.String())
	}
}
