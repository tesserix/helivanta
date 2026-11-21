package pharmacy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSupplierReturnPreparationReadsPaidOriginalBillsAndCurrentDispatchedUnits(t *testing.T) {
	for _, ownership := range []string{"hospital_owned", "independent", "chain"} {
		t.Run(ownership, func(t *testing.T) {
			w := supplierReturnWorldWithDispatch(t, ownership, true)
			paid := do(w.r, "PUT", payableRoot()+"/payables/"+w.bill+"/payments/"+uuid.Must(uuid.NewV7()).String(), "editor", strings.Replace(settlementBody("payment", "TEST fully paid original bill", 11800, nil), "2026-10-15", "2026-11-15", 1))
			require.Equal(t, 200, paid.Code, paid.Body.String())
			path := payableRoot() + "/supplier-returns/preparation/" + w.bill
			choices := w.request("GET", payableRoot()+"/supplier-returns/original-bills?limit=1", "editor", "")
			require.Equal(t, 200, choices.Code, choices.Body.String())
			var bills struct {
				Data []pharmacy.SupplierReturnOriginalBill `json:"data"`
			}
			require.NoError(t, json.Unmarshal(choices.Body.Bytes(), &bills))
			require.Len(t, bills.Data, 1)
			require.Equal(t, w.bill, bills.Data[0].ID.String())

			read := w.request("GET", path, "editor", "")
			require.Equal(t, 200, read.Code, read.Body.String())
			require.Equal(t, "private, no-store", read.Header().Get("Cache-Control"))
			var result pharmacy.SupplierReturnPreparation
			require.NoError(t, json.Unmarshal(read.Body.Bytes(), &result))
			require.Len(t, result.Lines, 1)
			require.Equal(t, w.bill, result.PayableID.String())
			require.Equal(t, "TEST External Supplier", result.PayeeName)
			require.Equal(t, "TEST-BILL-1", result.DocumentReference)
			require.NotEmpty(t, result.StockAsOf)
			positions := do(w.r, "GET", stockPath("positions?batch_id="+w.batch+"&as_of="+url.QueryEscape(result.StockAsOf)), "editor", "")
			require.Equal(t, 200, positions.Code, positions.Body.String())

			source := result.Lines[0]
			require.Equal(t, w.receipt, source.ReceiptID.String())
			require.Equal(t, w.batch, source.BatchID.String())
			require.EqualValues(t, 80, source.ReceiptUnits)
			require.EqualValues(t, 0, source.DispatchedUnits)
			require.Equal(t, "external", source.Origin)
			require.NotEmpty(t, source.ProductName)
			require.NotEmpty(t, source.UnitCode)
			require.NotEmpty(t, source.ExpiresOn)
			parent := supplierReturnPath(uuid.Must(uuid.NewV7()).String())
			created := w.request("PUT", parent, "editor", w.body(48))
			require.Equal(t, 200, created.Code, created.Body.String())
			posted := w.request("PUT", parent+"/dispatches/"+uuid.Must(uuid.NewV7()).String(), "writer", dispatchInput(t, w))
			require.Equal(t, 200, posted.Code, posted.Body.String())
			recovered := w.request("GET", parent+"/preparation", "editor", "")
			require.Equal(t, 200, recovered.Code, recovered.Body.String())
			require.JSONEq(t, created.Body.String(), recovered.Body.String())
			require.Equal(t, 404, w.request("GET", parent+"/preparation", "writer", "").Code)

			read = w.request("GET", path, "editor", "")
			require.Equal(t, 200, read.Code, read.Body.String())
			require.NoError(t, json.Unmarshal(read.Body.Bytes(), &result))
			require.EqualValues(t, 48, result.Lines[0].DispatchedUnits)
			added := do(w.r, "PUT", stockPath("movements/"+uuid.Must(uuid.NewV7()).String()), "editor", stockBody(w.location, w.batch, "receipt", "TEST new unrelated receipt", 40))
			require.Equal(t, 200, added.Code, added.Body.String())
			refused := w.request("PUT", supplierReturnPath(uuid.Must(uuid.NewV7()).String()), "editor", w.body(40))
			require.Equal(t, 409, refused.Code, refused.Body.String())
			for _, actor := range []string{"sibling", "foreign"} {
				require.Equal(t, 404, w.request("GET", path, actor, "").Code)
			}
			require.Equal(t, 403, w.request("GET", path, "reader", "").Code)
			require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
				var count int64
				if err := tx.Table("audit_events").Where("resource_type='pharmacy_supplier_return_preparation' AND resource_id=?", w.bill).Count(&count).Error; err != nil {
					return err
				}
				require.EqualValues(t, 2, count)
				return nil
			}))
		})
	}
}
func TestSupplierReturnPreparationFailsClosedWhenReadAuditUnavailable(t *testing.T) {
	w := newSupplierReturnWorld(t, "independent")
	path := payableRoot() + "/supplier-returns/preparation/" + w.bill
	parent := supplierReturnPath(uuid.Must(uuid.NewV7()).String())
	created := w.request("PUT", parent, "editor", w.body(4))
	require.Equal(t, 200, created.Code, created.Body.String())
	apply := func(sql string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		require.NoError(t, w.db.Migrate(ctx, []tenantdb.Migration{{ID: "test_return_preparation_audit_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}
	t.Cleanup(func() { apply("ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS synthetic_preparation_read_failure") })
	apply("ALTER TABLE audit_events ADD CONSTRAINT synthetic_preparation_read_failure CHECK(resource_type NOT IN('pharmacy_supplier_return_preparation','pharmacy_supplier_return_preparation_recovery','pharmacy_supplier_return_original_bills')) NOT VALID")
	failed := w.request("GET", path, "editor", "")
	require.Equal(t, 503, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "TEST External Supplier")
	require.NotContains(t, failed.Body.String(), "synthetic_preparation_read_failure")
	failed = w.request("GET", payableRoot()+"/supplier-returns/original-bills", "editor", "")
	require.Equal(t, 503, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "TEST External Supplier")
	failed = w.request("GET", parent+"/preparation", "editor", "")
	require.Equal(t, 503, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "TEST External Supplier")
}

func TestSupplierReturnOriginalBillsAreScopedInventoryOnlyAndPaginated(t *testing.T) {
	w := newSupplierReturnWorld(t, "independent")
	utility := do(w.r, "PUT", payableRoot()+"/payables/"+uuid.Must(uuid.NewV7()).String(), "editor", expenseBody("TEST utility"))
	require.Equal(t, 200, utility.Code, utility.Body.String())
	for range 2 {
		receipt := uuid.Must(uuid.NewV7()).String()
		received := do(w.r, "PUT", stockPath("movements/"+receipt), "editor", stockBody(w.location, w.batch, "receipt", "TEST further purchase "+receipt, 20))
		require.Equal(t, 200, received.Code, received.Body.String())
		body := fmt.Sprintf(`{"payee_reference":"TEST-EXT","payee_name":"TEST External Supplier","document_reference":%q,"issued_on":"2026-11-01","due_on":"2026-11-30","currency":"INR","lines":[{"category":"inventory","receipt_id":%q,"description":"TEST received cartons","net_minor":10000,"tax_minor":1800,"recoverable_tax_minor":1800,"landed_minor":0}]}`, "TEST inventory "+receipt, receipt)
		bill := do(w.r, "PUT", payableRoot()+"/payables/"+uuid.Must(uuid.NewV7()).String(), "editor", body)
		require.Equal(t, 200, bill.Code, bill.Body.String())
	}
	path := payableRoot() + "/supplier-returns/original-bills?limit=1"
	seen := map[uuid.UUID]bool{}
	for i := range 3 {
		response := w.request("GET", path, "editor", "")
		require.Equal(t, 200, response.Code, response.Body.String())
		require.Equal(t, "private, no-store", response.Header().Get("Cache-Control"))
		var page struct {
			Data []pharmacy.SupplierReturnOriginalBill `json:"data"`
			Page struct {
				More bool    `json:"has_more"`
				Next *string `json:"next_cursor"`
			} `json:"page"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
		require.Len(t, page.Data, 1)
		require.False(t, seen[page.Data[0].ID])
		seen[page.Data[0].ID] = true
		require.NotEqual(t, "TEST utility", page.Data[0].DocumentReference)
		if i < 2 {
			require.True(t, page.Page.More)
			require.NotNil(t, page.Page.Next)
			path = payableRoot() + "/supplier-returns/original-bills?limit=1&cursor=" + url.QueryEscape(*page.Page.Next)
		} else {
			require.False(t, page.Page.More)
			require.Nil(t, page.Page.Next)
		}
	}
	require.Len(t, seen, 3)
	for _, actor := range []string{"sibling", "foreign"} {
		require.Equal(t, 404, w.request("GET", payableRoot()+"/supplier-returns/original-bills", actor, "").Code)
	}
	require.Equal(t, 403, w.request("GET", payableRoot()+"/supplier-returns/original-bills", "reader", "").Code)
	for _, query := range []string{"?limit=51", "?other=all", "?limit=1&limit=2"} {
		require.Equal(t, 400, w.request("GET", payableRoot()+"/supplier-returns/original-bills"+query, "editor", "").Code)
	}
}
