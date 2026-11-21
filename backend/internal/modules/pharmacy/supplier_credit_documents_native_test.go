package pharmacy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func creditJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return string(raw)
}
func actualCreditInput(returnID, reference string, amount int64, correction *uuid.UUID) pharmacy.SupplierCreditDocumentInput {
	return pharmacy.SupplierCreditDocumentInput{ReturnID: uuid.MustParse(returnID), DocumentReference: reference, IssuedOn: "2026-11-16", AmountMinor: &amount, Currency: "INR", Reason: "TEST received supplier credit document", CorrectionOf: correction}
}
func dispatchedCreditSource(t *testing.T, ownership string) (supplierReturnWorld, string) {
	t.Helper()
	w := supplierReturnWorldWithDispatch(t, ownership, true, pharmacy.PermAccountsRead)
	id := uuid.Must(uuid.NewV7()).String()
	parent := supplierReturnPath(id)
	created := w.request("PUT", parent, "editor", w.body(4))
	require.Equal(t, 200, created.Code, created.Body.String())
	posted := w.request("PUT", parent+"/dispatches/"+uuid.Must(uuid.NewV7()).String(), "writer", dispatchInput(t, w))
	require.Equal(t, 200, posted.Code, posted.Body.String())
	return w, id
}
func creditPath(id string) string { return payableRoot() + "/supplier-credits/" + id }

func TestSupplierReturnCreditRetainsActualPartialAmountAgainstPaidOriginalBill(t *testing.T) {
	for _, ownership := range []string{"hospital_owned", "independent", "chain"} {
		t.Run(ownership, func(t *testing.T) {
			w, source := dispatchedCreditSource(t, ownership)
			payment := do(w.r, "PUT", payableRoot()+"/payables/"+w.bill+"/payments/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"kind":"payment","amount_minor":11800,"currency":"INR","paid_by":"TEST Finance operator","method":"bank","source_reference":"TEST-PAID-ORIGINAL","occurred_on":"2026-11-15","reason":"TEST settled original bill","reversal_of":null}`)
			require.Equal(t, 200, payment.Code, payment.Body.String())
			path := creditPath(uuid.Must(uuid.NewV7()).String())
			body := creditJSON(t, actualCreditInput(source, " test-cn-paid ", 750, nil))
			created := w.request("PUT", path, "editor", body)
			require.Equal(t, 200, created.Code, created.Body.String())
			var receipt pharmacy.SupplierCreditDocumentReceipt
			require.NoError(t, json.Unmarshal(created.Body.Bytes(), &receipt))
			require.Equal(t, "TEST-CN-PAID", receipt.DocumentReference)
			require.Equal(t, "user-editor", receipt.RecordedBy)
			require.Equal(t, int64(750), *receipt.AmountMinor)
			current := w.request("GET", path, "reader", "")
			require.Equal(t, 200, current.Code, current.Body.String())
			var view pharmacy.SupplierCreditDocumentView
			require.NoError(t, json.Unmarshal(current.Body.Bytes(), &view))
			require.Equal(t, "active", view.State)
			require.Equal(t, int64(1), view.Version)
			require.Zero(t, view.MatchedMinor)
			require.Zero(t, view.AllocatedMinor)
			require.Equal(t, int64(750), view.AvailableMinor)
			bill := do(w.r, "GET", payableRoot()+"/payables/"+w.bill, "editor", "")
			require.Equal(t, 200, bill.Code, bill.Body.String())
			var amounts struct {
				PaidMinor        int64 `json:"paid_minor"`
				CreditMinor      int64 `json:"credit_minor"`
				OutstandingMinor int64 `json:"outstanding_minor"`
			}
			require.NoError(t, json.Unmarshal(bill.Body.Bytes(), &amounts))
			require.Equal(t, int64(11800), amounts.PaidMinor)
			require.Zero(t, amounts.CreditMinor)
			require.Zero(t, amounts.OutstandingMinor)
			replayed := w.request("PUT", path, "editor", body)
			require.Equal(t, 200, replayed.Code, replayed.Body.String())
			require.JSONEq(t, created.Body.String(), replayed.Body.String())
			recovered := w.request("GET", path+"/recovery", "editor", "")
			require.Equal(t, 200, recovered.Code, recovered.Body.String())
			require.JSONEq(t, created.Body.String(), recovered.Body.String())
			require.Equal(t, 404, w.request("GET", path+"/recovery", "writer", "").Code)
			require.Equal(t, 409, w.request("PUT", path, "writer", body).Code)
			require.Equal(t, 409, w.request("PUT", path, "editor", creditJSON(t, actualCreditInput(source, "TEST-CN-PAID", 751, nil))).Code)
			require.Equal(t, 409, w.request("PUT", creditPath(uuid.Must(uuid.NewV7()).String()), "editor", body).Code)
		})
	}
}

func TestSupplierReturnCreditVoidCorrectionAndOriginalRecoveryPreserveHistory(t *testing.T) {
	w, source := dispatchedCreditSource(t, "independent")
	id := uuid.Must(uuid.NewV7())
	path := creditPath(id.String())
	body := creditJSON(t, actualCreditInput(source, "TEST-CN-CORRECT", 1500, nil))
	created := w.request("PUT", path, "editor", body)
	require.Equal(t, 200, created.Code, created.Body.String())
	version := int64(1)
	voidBody := creditJSON(t, pharmacy.SupplierCreditVoidInput{ExpectedVersion: &version, Reason: "TEST supplied value needs correction"})
	voidPath := path + "/voids/" + uuid.Must(uuid.NewV7()).String()
	voided := w.request("PUT", voidPath, "editor", voidBody)
	require.Equal(t, 200, voided.Code, voided.Body.String())
	var view pharmacy.SupplierCreditDocumentView
	get := w.request("GET", path, "reader", "")
	require.Equal(t, 200, get.Code, get.Body.String())
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &view))
	require.Equal(t, "voided", view.State)
	require.Equal(t, int64(2), view.Version)
	require.Zero(t, view.AvailableMinor)
	readVoid := w.request("GET", path+"/void", "reader", "")
	require.Equal(t, 200, readVoid.Code, readVoid.Body.String())
	require.JSONEq(t, voided.Body.String(), readVoid.Body.String())
	require.Equal(t, 404, w.request("GET", voidPath+"/recovery", "writer", "").Code)
	correctedPath := creditPath(uuid.Must(uuid.NewV7()).String())
	corrected := w.request("PUT", correctedPath, "editor", creditJSON(t, actualCreditInput(source, "TEST-CN-CORRECT", 1250, &id)))
	require.Equal(t, 200, corrected.Code, corrected.Body.String())
	get = w.request("GET", path, "reader", "")
	require.Equal(t, 200, get.Code, get.Body.String())
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &view))
	require.Equal(t, "corrected", view.State)
	require.Equal(t, int64(3), view.Version)
	require.Zero(t, view.AvailableMinor)
	recovered := w.request("GET", path+"/recovery", "editor", "")
	require.Equal(t, 200, recovered.Code, recovered.Body.String())
	require.JSONEq(t, created.Body.String(), recovered.Body.String())
	replayed := w.request("PUT", voidPath, "editor", voidBody)
	require.Equal(t, 200, replayed.Code, replayed.Body.String())
	require.JSONEq(t, voided.Body.String(), replayed.Body.String())
	recovered = w.request("GET", voidPath+"/recovery", "editor", "")
	require.Equal(t, 200, recovered.Code, recovered.Body.String())
	require.JSONEq(t, voided.Body.String(), recovered.Body.String())
	require.Equal(t, 409, w.request("PUT", creditPath(uuid.Must(uuid.NewV7()).String()), "editor", creditJSON(t, actualCreditInput(source, "TEST-CN-OTHER", 1200, &id))).Code)
	require.Equal(t, 409, w.request("PUT", path+"/voids/"+uuid.Must(uuid.NewV7()).String(), "editor", voidBody).Code)
}

func TestSupplierReturnCreditScopePaginationAndReadonlyFinancialAccess(t *testing.T) {
	w, source := dispatchedCreditSource(t, "hospital_owned")
	path := creditPath(uuid.Must(uuid.NewV7()).String())
	body := creditJSON(t, actualCreditInput(source, "TEST-CN-ACCESS", 1500, nil))
	created := w.request("PUT", path, "editor", body)
	require.Equal(t, 200, created.Code, created.Body.String())
	for _, actor := range []string{"foreign", "sibling"} {
		require.Equal(t, 404, w.request("GET", path, actor, "").Code)
		require.Equal(t, 404, w.request("PUT", creditPath(uuid.Must(uuid.NewV7()).String()), actor, body).Code)
	}
	require.Equal(t, 403, w.request("PUT", creditPath(uuid.Must(uuid.NewV7()).String()), "reader", body).Code)
	require.Equal(t, 403, w.request("GET", path, "denied", "").Code)
	require.Equal(t, 400, w.request("GET", path+"?unexpected=all", "reader", "").Code)
	for i := 0; i < 2; i++ {
		result := w.request("PUT", creditPath(uuid.Must(uuid.NewV7()).String()), "editor", creditJSON(t, actualCreditInput(source, fmt.Sprintf("TEST-CN-PAGE-%d", i), 750, nil)))
		require.Equal(t, 200, result.Code, result.Body.String())
	}
	first := w.request("GET", payableRoot()+"/supplier-credits?limit=2", "reader", "")
	require.Equal(t, 200, first.Code, first.Body.String())
	var page struct {
		Data []pharmacy.SupplierCreditDocumentView `json:"data"`
		Page struct {
			HasMore    bool    `json:"has_more"`
			NextCursor *string `json:"next_cursor"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &page))
	require.Len(t, page.Data, 2)
	require.True(t, page.Page.HasMore)
	require.NotNil(t, page.Page.NextCursor)
	second := w.request("GET", payableRoot()+"/supplier-credits?limit=2&cursor="+url.QueryEscape(*page.Page.NextCursor), "reader", "")
	require.Equal(t, 200, second.Code, second.Body.String())
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.False(t, page.Page.HasMore)
	revoked := do(w.r, "DELETE", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+pharmaOutlet+"/access/user-reader", "editor", "")
	require.Equal(t, 200, revoked.Code, revoked.Body.String())
	require.Equal(t, 404, w.request("GET", path, "reader", "").Code)
}

func TestSupplierReturnCreditDuplicateDocumentReferenceSerializes(t *testing.T) {
	w, source := dispatchedCreditSource(t, "independent")
	body := creditJSON(t, actualCreditInput(source, "TEST-CN-RACE", 1500, nil))
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		path := creditPath(uuid.Must(uuid.NewV7()).String())
		wg.Go(func() { results <- w.request("PUT", path, "editor", body).Code })
	}
	wg.Wait()
	close(results)
	statuses := map[int]int{}
	for status := range results {
		statuses[status]++
	}
	require.Equal(t, 1, statuses[200])
	require.Equal(t, 1, statuses[409])
}

func TestSupplierReturnCreditRefusesDraftSourceInvalidDatesAndSpoofedActor(t *testing.T) {
	w := supplierReturnWorldWithDispatch(t, "independent", true, pharmacy.PermAccountsRead)
	source := uuid.Must(uuid.NewV7()).String()
	parent := supplierReturnPath(source)
	draft := w.request("PUT", parent, "editor", w.body(4))
	require.Equal(t, 200, draft.Code, draft.Body.String())
	in := actualCreditInput(source, "TEST-CN-VALIDATION", 1500, nil)
	result := w.request("PUT", creditPath(uuid.Must(uuid.NewV7()).String()), "editor", creditJSON(t, in))
	require.Equal(t, 409, result.Code, result.Body.String())
	dispatched := w.request("PUT", parent+"/dispatches/"+uuid.Must(uuid.NewV7()).String(), "writer", dispatchInput(t, w))
	require.Equal(t, 200, dispatched.Code, dispatched.Body.String())
	for _, date := range []string{"2026-10-01", "2099-01-01"} {
		bad := in
		bad.IssuedOn = date
		result = w.request("PUT", creditPath(uuid.Must(uuid.NewV7()).String()), "editor", creditJSON(t, bad))
		require.Equal(t, 409, result.Code, result.Body.String())
	}
	var spoof map[string]any
	require.NoError(t, json.Unmarshal([]byte(creditJSON(t, in)), &spoof))
	spoof["recorded_by"] = "TEST other actor"
	result = w.request("PUT", creditPath(uuid.Must(uuid.NewV7()).String()), "editor", creditJSON(t, spoof))
	require.Equal(t, 400, result.Code, result.Body.String())
	unknown := uuid.Must(uuid.NewV7())
	in.CorrectionOf = &unknown
	result = w.request("PUT", creditPath(uuid.Must(uuid.NewV7()).String()), "editor", creditJSON(t, in))
	require.Equal(t, 404, result.Code, result.Body.String())
}

func TestSupplierReturnCreditAuditAndOutboxFailureNeverCreatePartialCredit(t *testing.T) {
	w, source := dispatchedCreditSource(t, "independent")
	path := creditPath(uuid.Must(uuid.NewV7()).String())
	body := creditJSON(t, actualCreditInput(source, "TEST-CN-FAULT", 1500, nil))
	apply := func(sql string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		require.NoError(t, w.db.Migrate(ctx, []tenantdb.Migration{{ID: "test_credit_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}
	for _, fault := range []struct{ table, check string }{
		{"audit_events", "resource_type<>'pharmacy_supplier_credit_documents'"},
		{"audit_events", "resource_type<>'pharmacy_supplier_credit_positions'"},
		{"outbox_events", "subject<>'helivanta.in.pharmacy.supplier_credit_changed.v1'"},
	} {
		clear := func() { apply("ALTER TABLE " + fault.table + " DROP CONSTRAINT IF EXISTS synthetic_credit_failure") }
		t.Cleanup(clear)
		apply("ALTER TABLE " + fault.table + " ADD CONSTRAINT synthetic_credit_failure CHECK(" + fault.check + ") NOT VALID")
		failed := w.request("PUT", path, "editor", body)
		require.Equal(t, 503, failed.Code, failed.Body.String())
		require.NotContains(t, failed.Body.String(), "synthetic_credit_failure")
		require.Equal(t, 404, w.request("GET", path+"/recovery", "editor", "").Code)
		require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
			var n int64
			if err := tx.Table("pharmacy_supplier_credit_documents").Count(&n).Error; err != nil {
				return err
			}
			require.Zero(t, n)
			return nil
		}))
		clear()
	}
	posted := w.request("PUT", path, "editor", body)
	require.Equal(t, 200, posted.Code, posted.Body.String())
	apply("ALTER TABLE audit_events ADD CONSTRAINT synthetic_credit_read_failure CHECK(resource_type NOT IN('pharmacy_supplier_credit','pharmacy_supplier_credit_receipt','pharmacy_supplier_credits')) NOT VALID")
	t.Cleanup(func() { apply("ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS synthetic_credit_read_failure") })
	for _, read := range []string{path, path + "/recovery", payableRoot() + "/supplier-credits?limit=2"} {
		result := w.request("GET", read, "editor", "")
		require.Equal(t, 503, result.Code, result.Body.String())
		require.False(t, strings.Contains(result.Body.String(), "TEST-CN-FAULT"))
	}
}
