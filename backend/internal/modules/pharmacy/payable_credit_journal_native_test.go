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

type payableCreditJournalPage struct {
	Data []pharmacy.SupplierCreditAllocationView `json:"data"`
	Page struct {
		HasMore    bool    `json:"has_more"`
		NextCursor *string `json:"next_cursor"`
	} `json:"page"`
}

func TestSupplierReturnCreditPayableJournalSeparatesDocumentsAndRetainsPaidBillReversal(t *testing.T) {
	for _, ownership := range []string{"hospital_owned", "independent", "chain"} {
		t.Run(ownership, func(t *testing.T) {
			w, source := dispatchedCreditSource(t, ownership)
			bill := futureSupplierCreditBill(t, w)
			first := recordSupplierCreditFixture(t, w, source, "TEST-JOURNAL-ONE", 750)
			firstAllocation := uuid.Must(uuid.NewV7()).String()
			result := w.request("PUT", first+"/allocations/"+firstAllocation, "editor", creditJSON(t, allocationIntent(bill, 1, 750, 1, 1180, 500)))
			require.Equal(t, 200, result.Code, result.Body.String())
			second := recordSupplierCreditFixture(t, w, source, "TEST-JOURNAL-TWO", 750)
			secondAllocation := uuid.Must(uuid.NewV7()).String()
			result = w.request("PUT", second+"/allocations/"+secondAllocation, "editor", creditJSON(t, allocationIntent(bill, 1, 750, 2, 680, 100)))
			require.Equal(t, 200, result.Code, result.Body.String())
			root := payableRoot() + "/payables/" + bill + "/supplier-credit-allocations"
			read := func(path, actor string) payableCreditJournalPage {
				t.Helper()
				response := w.request("GET", path, actor, "")
				require.Equal(t, 200, response.Code, response.Body.String())
				var page payableCreditJournalPage
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
				return page
			}
			grantCatalogue(t, w.r, "user-finance", "editor")
			page := read(root+"?limit=1", "finance")
			require.Len(t, page.Data, 1)
			require.True(t, page.Page.HasMore)
			require.NotNil(t, page.Page.NextCursor)
			require.Equal(t, "TEST-JOURNAL-TWO", page.Data[0].CreditDocumentReference)
			require.Equal(t, secondAllocation, page.Data[0].ID.String())
			require.Nil(t, page.Data[0].ReversalID)
			cursor := *page.Page.NextCursor
			payment := do(w.r, "PUT", payableRoot()+"/payables/"+bill+"/payments/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"kind":"payment","amount_minor":580,"currency":"INR","paid_by":"TEST Finance operator","method":"bank","source_reference":"TEST-JOURNAL-PAID","occurred_on":"2026-11-19","reason":"TEST remaining bill liability","reversal_of":null}`)
			require.Equal(t, 200, payment.Code, payment.Body.String())
			reversal := uuid.Must(uuid.NewV7()).String()
			reversed := w.request("PUT", first+"/allocations/"+firstAllocation+"/reversals/"+reversal, "editor", creditJSON(t, map[string]any{"expected_version": 2, "expected_available_minor": 250, "expected_payable_version": 4, "expected_outstanding_minor": 0, "occurred_on": "2026-11-20", "reason": "TEST journal credit reversal preserves payment"}))
			require.Equal(t, 200, reversed.Code, reversed.Body.String())
			page = read(root+"?limit=1&cursor="+url.QueryEscape(cursor), "finance")
			require.Len(t, page.Data, 1)
			require.False(t, page.Page.HasMore)
			require.Equal(t, firstAllocation, page.Data[0].ID.String())
			require.NotNil(t, page.Data[0].ReversalID)
			require.Equal(t, reversal, page.Data[0].ReversalID.String())
			require.NotNil(t, page.Data[0].AmountMinor)
			require.EqualValues(t, 500, *page.Data[0].AmountMinor)
			require.Equal(t, "TEST-JOURNAL-ONE", page.Data[0].CreditDocumentReference)
			empty := payableRoot() + "/payables/" + w.bill + "/supplier-credit-allocations"
			require.Empty(t, read(empty, "reader").Data)
			require.Len(t, read(root, "reader").Data, 2)
			require.Equal(t, 400, w.request("GET", empty+"?limit=1&cursor="+url.QueryEscape(cursor), "finance", "").Code)
			for _, actor := range []string{"foreign", "sibling"} {
				require.Equal(t, 404, w.request("GET", root, actor, "").Code)
			}
			require.Equal(t, 403, w.request("GET", root, "denied", "").Code)
			require.Equal(t, 400, w.request("GET", root+"?unknown=1", "reader", "").Code)
			require.Equal(t, 400, w.request("GET", root+"?limit=51", "reader", "").Code)
			require.Equal(t, 404, w.request("GET", payableRoot()+"/payables/"+uuid.Must(uuid.NewV7()).String()+"/supplier-credit-allocations", "reader", "").Code)
			require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
				var valid bool
				if err := tx.Raw(`SELECT i.indisvalid AND i.indisready FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE c.relname='pharmacy_payable_credit_journal'`).Scan(&valid).Error; err != nil {
					return err
				}
				require.True(t, valid)
				var plan string
				if err := tx.Raw(`EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT id FROM pharmacy_supplier_credit_allocations WHERE tenant_id=current_setting('app.tenant_id')::uuid AND outlet_id=? AND payable_id=? ORDER BY created_at DESC,id DESC LIMIT 50`, pharmaOutlet, bill).Scan(&plan).Error; err != nil {
					return err
				}
				var explained []struct {
					Plan struct {
						ActualRows float64 `json:"Actual Rows"`
					} `json:"Plan"`
				}
				require.NoError(t, json.Unmarshal([]byte(plan), &explained))
				require.Len(t, explained, 1)
				require.Equal(t, float64(2), explained[0].Plan.ActualRows)
				t.Log("Small synthetic payable journal plan:", plan)
				return nil
			}))
			revoked := do(w.r, "DELETE", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+pharmaOutlet+"/access/user-reader", "editor", "")
			require.Equal(t, 200, revoked.Code, revoked.Body.String())
			require.Equal(t, 404, w.request("GET", root, "reader", "").Code)
		})
	}
}
func TestSupplierReturnCreditPayableJournalAuditFailureDoesNotDisclose(t *testing.T) {
	w, source := dispatchedCreditSource(t, "independent")
	bill := futureSupplierCreditBill(t, w)
	credit := recordSupplierCreditFixture(t, w, source, "TEST-JOURNAL-PRIVATE", 750)
	result := w.request("PUT", credit+"/allocations/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, allocationIntent(bill, 1, 750, 1, 1180, 500)))
	require.Equal(t, 200, result.Code, result.Body.String())
	apply := func(sql string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		require.NoError(t, w.db.Migrate(ctx, []tenantdb.Migration{{ID: "test_journal_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}
	t.Cleanup(func() {
		apply("ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS synthetic_payable_credit_read_failure")
	})
	apply("ALTER TABLE audit_events ADD CONSTRAINT synthetic_payable_credit_read_failure CHECK(resource_type<>'pharmacy_payable_supplier_credit_allocations') NOT VALID")
	failed := w.request("GET", payableRoot()+"/payables/"+bill+"/supplier-credit-allocations", "reader", "")
	require.Equal(t, 503, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "TEST-JOURNAL-PRIVATE")
	require.NotContains(t, failed.Body.String(), "TEST External Supplier")
}
