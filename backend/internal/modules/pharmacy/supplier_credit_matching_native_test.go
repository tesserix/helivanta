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

func TestSupplierReturnCreditMatchesPartialCombinedDocumentWithoutSpendingPool(t *testing.T) {
	for _, ownership := range []string{"hospital_owned", "independent", "chain"} {
		t.Run(ownership, func(t *testing.T) {
			w, firstReturn := dispatchedCreditSource(t, ownership)
			secondReturn := uuid.Must(uuid.NewV7()).String()
			created := w.request("PUT", supplierReturnPath(secondReturn), "editor", w.body(4))
			require.Equal(t, 200, created.Code, created.Body.String())
			dispatched := w.request("PUT", supplierReturnPath(secondReturn)+"/dispatches/"+uuid.Must(uuid.NewV7()).String(), "writer", strings.ReplaceAll(dispatchInput(t, w), "TEST-OUT-1", "TEST-OUT-2"))
			require.Equal(t, 200, dispatched.Code, dispatched.Body.String())
			credit := uuid.Must(uuid.NewV7()).String()
			path := creditPath(credit)
			created = w.request("PUT", path, "editor", creditJSON(t, actualCreditInput(firstReturn, "TEST-CN-COMBINED", 750, nil)))
			require.Equal(t, 200, created.Code, created.Body.String())
			for i, target := range []struct {
				id     string
				amount int64
			}{{firstReturn, 500}, {secondReturn, 250}} {
				command := uuid.Must(uuid.NewV7()).String()
				body := creditJSON(t, map[string]any{"return_id": target.id, "expected_version": i + 1, "expected_return_matched_minor": 0, "amount_minor": target.amount, "reason": "TEST supplier combined note attribution", "overage_reason": nil})
				matched := w.request("PUT", path+"/matches/"+command, "editor", body)
				require.Equal(t, 200, matched.Code, matched.Body.String())
				var receipt struct {
					AmountMinor        int64 `json:"amount_minor"`
					Version            int64 `json:"version"`
					ReturnMatchedMinor int64 `json:"return_matched_minor"`
				}
				require.NoError(t, json.Unmarshal(matched.Body.Bytes(), &receipt))
				require.Equal(t, target.amount, receipt.AmountMinor)
				require.EqualValues(t, i+2, receipt.Version)
				require.Equal(t, target.amount, receipt.ReturnMatchedMinor)
				replayed := w.request("PUT", path+"/matches/"+command, "editor", body)
				require.Equal(t, 200, replayed.Code, replayed.Body.String())
				require.JSONEq(t, matched.Body.String(), replayed.Body.String())
			}
			current := w.request("GET", path, "reader", "")
			require.Equal(t, 200, current.Code, current.Body.String())
			var view pharmacy.SupplierCreditDocumentView
			require.NoError(t, json.Unmarshal(current.Body.Bytes(), &view))
			require.Equal(t, int64(3), view.Version)
			require.Equal(t, int64(750), view.MatchedMinor)
			require.Zero(t, view.AllocatedMinor)
			require.Equal(t, int64(750), view.AvailableMinor)
		})
	}
}

func supplierCreditMatchInput(source string, version, observed, amount int64, excess *string) pharmacy.SupplierCreditMatchInput {
	return pharmacy.SupplierCreditMatchInput{ReturnID: uuid.MustParse(source), ExpectedVersion: &version, ExpectedReturnMatchedMinor: &observed, AmountMinor: &amount, Reason: "TEST explicit commercial attribution", OverageReason: excess}
}
func supplierCreditMatchReversalInput(version, observed int64) pharmacy.SupplierCreditMatchReversalInput {
	return pharmacy.SupplierCreditMatchReversalInput{ExpectedVersion: &version, ExpectedReturnMatchedMinor: &observed, Reason: "TEST correct commercial attribution"}
}
func recordSupplierCreditFixture(t *testing.T, w supplierReturnWorld, source, reference string, amount int64) string {
	t.Helper()
	path := creditPath(uuid.Must(uuid.NewV7()).String())
	created := w.request("PUT", path, "editor", creditJSON(t, actualCreditInput(source, reference, amount, nil)))
	require.Equal(t, 200, created.Code, created.Body.String())
	return path
}
func supplierCreditPosition(t *testing.T, w supplierReturnWorld, path string) pharmacy.SupplierCreditDocumentView {
	t.Helper()
	result := w.request("GET", path, "reader", "")
	require.Equal(t, 200, result.Code, result.Body.String())
	var view pharmacy.SupplierCreditDocumentView
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &view))
	return view
}

func TestSupplierReturnCreditMatchReversalPreservesHistoryScopeAndUnusedPool(t *testing.T) {
	w, source := dispatchedCreditSource(t, "independent")
	path := recordSupplierCreditFixture(t, w, source, "TEST-CN-MATCH-REVERSE", 750)
	match := path + "/matches/" + uuid.Must(uuid.NewV7()).String()
	body := creditJSON(t, supplierCreditMatchInput(source, 1, 0, 500, nil))
	created := w.request("PUT", match, "editor", body)
	require.Equal(t, 200, created.Code, created.Body.String())
	for _, actor := range []string{"sibling", "foreign"} {
		require.Equal(t, 404, w.request("GET", match, actor, "").Code)
		require.Equal(t, 404, w.request("GET", path+"/matches", actor, "").Code)
		require.Equal(t, 404, w.request("PUT", match, actor, body).Code)
	}
	require.Equal(t, 403, w.request("PUT", match, "reader", body).Code)
	require.Equal(t, 409, w.request("PUT", match, "writer", body).Code)
	require.Equal(t, 404, w.request("GET", match+"/recovery", "writer", "").Code)
	require.Equal(t, 200, w.request("GET", match, "reader", "").Code)
	require.Equal(t, 409, w.request("PUT", path+"/matches/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, supplierCreditMatchInput(source, 2, 0, 250, nil))).Code)
	require.Equal(t, 409, w.request("PUT", path+"/matches/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, supplierCreditMatchInput(source, 2, 500, 251, nil))).Code)
	v := int64(2)
	require.Equal(t, 409, w.request("PUT", path+"/voids/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, pharmacy.SupplierCreditVoidInput{ExpectedVersion: &v, Reason: "TEST cannot void attributed document"})).Code)
	reversalID := uuid.Must(uuid.NewV7()).String()
	reversal := match + "/reversals/" + reversalID
	reverseBody := creditJSON(t, supplierCreditMatchReversalInput(2, 500))
	reversed := w.request("PUT", reversal, "writer", reverseBody)
	require.Equal(t, 200, reversed.Code, reversed.Body.String())
	view := supplierCreditPosition(t, w, path)
	require.Equal(t, int64(3), view.Version)
	require.Zero(t, view.MatchedMinor)
	require.Equal(t, int64(750), view.AvailableMinor)
	recovered := w.request("GET", reversal+"/recovery", "writer", "")
	require.Equal(t, 200, recovered.Code, recovered.Body.String())
	require.JSONEq(t, reversed.Body.String(), recovered.Body.String())
	require.Equal(t, 404, w.request("GET", reversal+"/recovery", "editor", "").Code)
	require.Equal(t, 200, w.request("GET", match+"/reversal", "reader", "").Code)
	require.Equal(t, 409, w.request("PUT", match+"/reversals/"+uuid.Must(uuid.NewV7()).String(), "writer", creditJSON(t, supplierCreditMatchReversalInput(3, 0))).Code)
	page := w.request("GET", path+"/matches?limit=1", "reader", "")
	require.Equal(t, 200, page.Code, page.Body.String())
	var history struct {
		Data []pharmacy.SupplierCreditMatchView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(page.Body.Bytes(), &history))
	require.Len(t, history.Data, 1)
	require.NotNil(t, history.Data[0].ReversalID)
	require.Equal(t, reversalID, history.Data[0].ReversalID.String())
	original := w.request("GET", match+"/recovery", "editor", "")
	require.Equal(t, 200, original.Code, original.Body.String())
	require.JSONEq(t, created.Body.String(), original.Body.String())
	v = 3
	voided := w.request("PUT", path+"/voids/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, pharmacy.SupplierCreditVoidInput{ExpectedVersion: &v, Reason: "TEST document no longer applicable"}))
	require.Equal(t, 200, voided.Code, voided.Body.String())
	view = supplierCreditPosition(t, w, path)
	require.Equal(t, "voided", view.State)
	require.Equal(t, int64(4), view.Version)
	require.Zero(t, view.AvailableMinor)
	replayed := w.request("PUT", match, "editor", body)
	require.Equal(t, 200, replayed.Code, replayed.Body.String())
	require.JSONEq(t, created.Body.String(), replayed.Body.String())
	replayed = w.request("PUT", reversal, "writer", reverseBody)
	require.Equal(t, 200, replayed.Code, replayed.Body.String())
	require.JSONEq(t, reversed.Body.String(), replayed.Body.String())
}

func TestSupplierReturnCreditMatchesDifferentDocumentsSerializeObservedReturnTotal(t *testing.T) {
	w, source := dispatchedCreditSource(t, "hospital_owned")
	first := recordSupplierCreditFixture(t, w, source, "TEST-CN-ONE", 1000)
	second := recordSupplierCreditFixture(t, w, source, "TEST-CN-TWO", 1000)
	body := creditJSON(t, supplierCreditMatchInput(source, 1, 0, 1000, nil))
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for _, path := range []string{first, second} {
		command := path + "/matches/" + uuid.Must(uuid.NewV7()).String()
		wg.Go(func() { results <- w.request("PUT", command, "editor", body).Code })
	}
	wg.Wait()
	close(results)
	statuses := map[int]int{}
	for code := range results {
		statuses[code]++
	}
	require.Equal(t, 1, statuses[200])
	require.Equal(t, 1, statuses[409])
	a, b := supplierCreditPosition(t, w, first), supplierCreditPosition(t, w, second)
	require.Equal(t, int64(1000), a.MatchedMinor+b.MatchedMinor)
	remaining := first
	if a.MatchedMinor > 0 {
		remaining = second
	}
	path := remaining + "/matches/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 409, w.request("PUT", path, "editor", creditJSON(t, supplierCreditMatchInput(source, 1, 1000, 1000, nil))).Code)
	reason := "TEST supplier accepted commercial excess explicitly"
	accepted := w.request("PUT", path, "editor", creditJSON(t, supplierCreditMatchInput(source, 1, 1000, 1000, &reason)))
	require.Equal(t, 200, accepted.Code, accepted.Body.String())
	view := supplierCreditPosition(t, w, remaining)
	require.Equal(t, int64(1000), view.MatchedMinor)
	require.Equal(t, int64(1000), view.AvailableMinor)
}

func TestSupplierReturnCreditMatchHistoryIsBoundedAndPrivate(t *testing.T) {
	w, source := dispatchedCreditSource(t, "chain")
	path := recordSupplierCreditFixture(t, w, source, "TEST-CN-PAGES", 750)
	for i := int64(0); i < 3; i++ {
		posted := w.request("PUT", path+"/matches/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, supplierCreditMatchInput(source, i+1, i*100, 100, nil)))
		require.Equal(t, 200, posted.Code, posted.Body.String())
	}
	read := w.request("GET", path+"/matches?limit=2", "reader", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	var first struct {
		Data []pharmacy.SupplierCreditMatchView `json:"data"`
		Page struct {
			HasMore    bool    `json:"has_more"`
			NextCursor *string `json:"next_cursor"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(read.Body.Bytes(), &first))
	require.Len(t, first.Data, 2)
	require.True(t, first.Page.HasMore)
	require.NotNil(t, first.Page.NextCursor)
	read = w.request("GET", path+"/matches?limit=2&cursor="+url.QueryEscape(*first.Page.NextCursor), "reader", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	var last struct {
		Data []pharmacy.SupplierCreditMatchView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(read.Body.Bytes(), &last))
	require.Len(t, last.Data, 1)
	require.NotEqual(t, first.Data[0].ID, last.Data[0].ID)
	require.NotEqual(t, first.Data[1].ID, last.Data[0].ID)
	require.Equal(t, 400, w.request("GET", path+"/matches?limit=51", "reader", "").Code)
	require.Equal(t, 400, w.request("GET", path+"/matches?other=1", "reader", "").Code)
	revoked := do(w.r, "DELETE", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+pharmaOutlet+"/access/user-reader", "editor", "")
	require.Equal(t, 200, revoked.Code, revoked.Body.String())
	require.Equal(t, 404, w.request("GET", path+"/matches", "reader", "").Code)
}

func TestSupplierReturnCreditMatchAuditAndOutboxFailureRollbackAttribution(t *testing.T) {
	w, source := dispatchedCreditSource(t, "independent")
	path := recordSupplierCreditFixture(t, w, source, "TEST-CN-MATCH-FAULT", 750)
	match := path + "/matches/" + uuid.Must(uuid.NewV7()).String()
	body := creditJSON(t, supplierCreditMatchInput(source, 1, 0, 500, nil))
	apply := func(sql string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		require.NoError(t, w.db.Migrate(ctx, []tenantdb.Migration{{ID: "test_matching_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}
	for _, fault := range []struct{ table, check string }{
		{"audit_events", "resource_type<>'pharmacy_supplier_credit_matches'"},
		{"audit_events", "resource_type<>'pharmacy_supplier_credit_positions'"},
		{"outbox_events", "subject<>'helivanta.in.pharmacy.supplier_credit_changed.v1'"},
	} {
		clear := func() { apply("ALTER TABLE " + fault.table + " DROP CONSTRAINT IF EXISTS synthetic_match_failure") }
		t.Cleanup(clear)
		apply("ALTER TABLE " + fault.table + " ADD CONSTRAINT synthetic_match_failure CHECK(" + fault.check + ") NOT VALID")
		failed := w.request("PUT", match, "editor", body)
		require.Equal(t, 503, failed.Code, failed.Body.String())
		require.NotContains(t, failed.Body.String(), "synthetic_match_failure")
		require.Equal(t, 404, w.request("GET", match+"/recovery", "editor", "").Code)
		clear()
		view := supplierCreditPosition(t, w, path)
		require.Equal(t, int64(1), view.Version)
		require.Zero(t, view.MatchedMinor)
	}
	posted := w.request("PUT", match, "editor", body)
	require.Equal(t, 200, posted.Code, posted.Body.String())
	reversal := match + "/reversals/" + uuid.Must(uuid.NewV7()).String()
	reverseBody := creditJSON(t, supplierCreditMatchReversalInput(2, 500))
	for _, fault := range []struct{ table, check string }{
		{"audit_events", "resource_type<>'pharmacy_supplier_credit_match_reversals'"},
		{"audit_events", "resource_type<>'pharmacy_supplier_credit_positions'"},
		{"outbox_events", "subject<>'helivanta.in.pharmacy.supplier_credit_changed.v1'"},
	} {
		clear := func() { apply("ALTER TABLE " + fault.table + " DROP CONSTRAINT IF EXISTS synthetic_match_failure") }
		t.Cleanup(clear)
		apply("ALTER TABLE " + fault.table + " ADD CONSTRAINT synthetic_match_failure CHECK(" + fault.check + ") NOT VALID")
		failed := w.request("PUT", reversal, "writer", reverseBody)
		require.Equal(t, 503, failed.Code, failed.Body.String())
		require.Equal(t, 404, w.request("GET", reversal+"/recovery", "writer", "").Code)
		clear()
		view := supplierCreditPosition(t, w, path)
		require.Equal(t, int64(2), view.Version)
		require.Equal(t, int64(500), view.MatchedMinor)
	}
	reversed := w.request("PUT", reversal, "writer", reverseBody)
	require.Equal(t, 200, reversed.Code, reversed.Body.String())
	apply("ALTER TABLE audit_events ADD CONSTRAINT synthetic_match_read_failure CHECK(resource_type NOT IN('pharmacy_supplier_credit_matches','pharmacy_supplier_credit_match')) NOT VALID")
	t.Cleanup(func() { apply("ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS synthetic_match_read_failure") })
	for _, read := range []string{match, match + "/recovery", path + "/matches"} {
		failed := w.request("GET", read, "editor", "")
		require.Equal(t, 503, failed.Code, failed.Body.String())
		require.NotContains(t, failed.Body.String(), "TEST explicit commercial attribution")
	}
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		var n int64
		if err := tx.Table("pharmacy_supplier_credit_matches").Count(&n).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, n)
		if err := tx.Table("outbox_events").Where("subject=?", "helivanta.in.pharmacy.supplier_credit_changed.v1").Count(&n).Error; err != nil {
			return err
		}
		require.EqualValues(t, 3, n)
		return nil
	}))
}

func TestSupplierReturnCreditMatchRejectsOtherSupplierDraftAndSpoofedIntent(t *testing.T) {
	w, source := dispatchedCreditSource(t, "independent")
	path := recordSupplierCreditFixture(t, w, source, "TEST-CN-SCOPE", 750)
	draft := uuid.Must(uuid.NewV7()).String()
	created := w.request("PUT", supplierReturnPath(draft), "editor", w.body(4))
	require.Equal(t, 200, created.Code, created.Body.String())
	put := func(in pharmacy.SupplierCreditMatchInput) int {
		return w.request("PUT", path+"/matches/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, in)).Code
	}
	require.Equal(t, 409, put(supplierCreditMatchInput(draft, 1, 0, 500, nil)))
	require.Equal(t, 404, put(supplierCreditMatchInput(uuid.Must(uuid.NewV7()).String(), 1, 0, 500, nil)))
	other := w
	other.bill = uuid.Must(uuid.NewV7()).String()
	other.receipt = uuid.Must(uuid.NewV7()).String()
	received := do(w.r, "PUT", stockPath("movements/"+other.receipt), "editor", stockBody(other.location, other.batch, "receipt", "TEST-other-supplier-delivery", 20))
	require.Equal(t, 200, received.Code, received.Body.String())
	invoice := fmt.Sprintf(`{"payee_reference":"TEST-OTHER","payee_name":"TEST Other Supplier","document_reference":"TEST-OTHER-BILL","issued_on":"2026-11-01","due_on":"2026-11-30","currency":"INR","lines":[{"category":"inventory","receipt_id":%q,"description":"TEST other supplier cartons","net_minor":1000,"tax_minor":180,"recoverable_tax_minor":180,"landed_minor":0}]}`, other.receipt)
	billed := do(w.r, "PUT", payableRoot()+"/payables/"+other.bill, "editor", invoice)
	require.Equal(t, 200, billed.Code, billed.Body.String())
	otherReturn := uuid.Must(uuid.NewV7()).String()
	created = other.request("PUT", supplierReturnPath(otherReturn), "editor", other.body(4))
	require.Equal(t, 200, created.Code, created.Body.String())
	dispatched := other.request("PUT", supplierReturnPath(otherReturn)+"/dispatches/"+uuid.Must(uuid.NewV7()).String(), "writer", strings.ReplaceAll(dispatchInput(t, other), "TEST-OUT-1", "TEST-OTHER-OUT"))
	require.Equal(t, 200, dispatched.Code, dispatched.Body.String())
	require.Equal(t, 409, put(supplierCreditMatchInput(otherReturn, 1, 0, 500, nil)))
	var spoof map[string]any
	require.NoError(t, json.Unmarshal([]byte(creditJSON(t, supplierCreditMatchInput(source, 1, 0, 500, nil))), &spoof))
	spoof["recorded_by"] = "TEST false actor"
	require.Equal(t, 400, w.request("PUT", path+"/matches/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, spoof)).Code)
	view := supplierCreditPosition(t, w, path)
	require.Equal(t, int64(1), view.Version)
	require.Zero(t, view.MatchedMinor)
	err := catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Table("pharmacy_supplier_credit_positions").Where("id=?", view.ID).Update("matched_minor", 500).Error
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "credit position changes require a ledger entry")
}
