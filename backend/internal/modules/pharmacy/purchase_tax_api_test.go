package pharmacy_test

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

func newPurchaseTaxBill(t *testing.T, w saleWorld) string {
	t.Helper()
	bill := payableRoot() + "/payables/" + uuid.Must(uuid.NewV7()).String()
	response := do(w.r, "PUT", bill, "finance", expenseBody("TEST-PURCHASE-TAX"))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	return bill
}
func purchaseTaxBody() string {
	return `{"replaces":null,"supplier_gstin":"07ABCDE1234F1Z2","supplier_state":"07","place_of_supply":"07","reason":"TEST supplier invoice","lines":[{"position":1,"supply_nature":"taxable","rate_ppm":180000,"cgst_minor":900,"sgst_minor":900,"igst_minor":0,"utgst_minor":0,"cgst_recoverable_minor":900,"sgst_recoverable_minor":900,"igst_recoverable_minor":0,"utgst_recoverable_minor":0}]}`
}
func TestPurchaseTaxEvidenceVersionsReplayAndPreserveThePostedBill(t *testing.T) {
	w := newTallyWorld(t)
	bill := newPurchaseTaxBill(t, w)
	before := do(w.r, "GET", bill, "finance", "")
	absent := do(w.r, "GET", bill+"/purchase-tax", "exporter", "")
	require.Equal(t, 200, absent.Code, absent.Body.String())
	require.JSONEq(t, `{"evidence":null}`, absent.Body.String())
	id := uuid.Must(uuid.NewV7()).String()
	path := bill + "/purchase-tax/" + id
	first := do(w.r, "PUT", path, "exporter", purchaseTaxBody())
	require.Equal(t, 200, first.Code, first.Body.String())
	replay := do(w.r, "PUT", path, "exporter", purchaseTaxBody())
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, first.Body.String(), replay.Body.String())
	changed := do(w.r, "PUT", path, "exporter", strings.Replace(purchaseTaxBody(), "TEST supplier invoice", "TEST different source", 1))
	require.Equal(t, 409, changed.Code, changed.Body.String())
	stale := do(w.r, "PUT", bill+"/purchase-tax/"+uuid.Must(uuid.NewV7()).String(), "exporter", purchaseTaxBody())
	require.Equal(t, 409, stale.Code, stale.Body.String())
	successor := strings.Replace(purchaseTaxBody(), `"replaces":null`, `"replaces":"`+id+`"`, 1)
	nextID := uuid.Must(uuid.NewV7()).String()
	next := do(w.r, "PUT", bill+"/purchase-tax/"+nextID, "exporter", successor)
	require.Equal(t, 200, next.Code, next.Body.String())
	current := do(w.r, "GET", bill+"/purchase-tax", "exporter", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	var view struct {
		Evidence struct {
			ID string `json:"id"`
		}
	}
	require.NoError(t, json.Unmarshal(current.Body.Bytes(), &view))
	require.Equal(t, nextID, view.Evidence.ID)
	require.JSONEq(t, before.Body.String(), do(w.r, "GET", bill, "finance", "").Body.String(), "annotating evidence must not rewrite the posted bill or valuation")
	err := w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE pharmacy_purchase_tax_evidence SET reason='rewrite' WHERE id=?`, id).Error
	})
	require.ErrorContains(t, err, "immutable")
}
func TestPurchaseTaxRejectsWrongSourceTotalsAndScope(t *testing.T) {
	w := newTallyWorld(t)
	bill := newPurchaseTaxBill(t, w)
	path := bill + "/purchase-tax/" + uuid.Must(uuid.NewV7()).String()
	for _, actor := range []string{"stranger", "outsider", "auditor"} {
		result := do(w.r, "PUT", path, actor, purchaseTaxBody())
		require.Equal(t, 404, result.Code, actor+result.Body.String())
	}
	require.Equal(t, 403, do(w.r, "PUT", path, "finance", purchaseTaxBody()).Code)
	for _, body := range []string{
		strings.Replace(purchaseTaxBody(), `"cgst_minor":900`, `"cgst_minor":901`, 1),
		strings.Replace(purchaseTaxBody(), `"cgst_recoverable_minor":900`, `"cgst_recoverable_minor":899`, 1),
		strings.Replace(purchaseTaxBody(), `"supplier_state":"07"`, `"supplier_state":"27"`, 1),
		strings.Replace(purchaseTaxBody(), `"place_of_supply":"07"`, `"place_of_supply":"21"`, 1),
	} {
		result := do(w.r, "PUT", path, "exporter", body)
		require.Equal(t, 400, result.Code, result.Body.String())
	}
	wrong := strings.Replace(path, pharmaOutlet, uuid.Must(uuid.NewV7()).String(), 1)
	require.Equal(t, 404, do(w.r, "PUT", wrong, "exporter", purchaseTaxBody()).Code)
	request := httptest.NewRequest("PUT", path, strings.NewReader(purchaseTaxBody()))
	request.Header.Set("Authorization", "Bearer exporter")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Helivanta-Expected-Subject", "user-finance")
	result := httptest.NewRecorder()
	w.r.ServeHTTP(result, request)
	require.Equal(t, 409, result.Code, result.Body.String())
}
func TestTallyPurchaseMissingEvidenceRefusesPreviewAndFile(t *testing.T) {
	w := newTallyWorld(t)
	bill := newPurchaseTaxBill(t, w)
	for _, suffix := range []string{"/preview", "/vouchers.xml"} {
		result := do(w.r, "GET", tallyPath(suffix+tallyPeriod), "exporter", "")
		require.Equal(t, 409, result.Code, result.Body.String())
		require.Contains(t, result.Body.String(), "tally_purchase_tax_missing")
		require.Empty(t, result.Header().Get("Content-Disposition"))
	}
	require.Zero(t, tallyAuditRows(t, w, "accounts_export"))
	recordTallyPurchaseEvidence(t, w, bill)
	preview := w.tallyPreview(t, tallyPeriod)
	require.EqualValues(t, 1, preview.Purchases)
}
func TestPurchaseTaxConcurrentSuccessorsDoNotLoseEvidence(t *testing.T) {
	w := newTallyWorld(t)
	bill := newPurchaseTaxBill(t, w)
	recordTallyPurchaseEvidence(t, w, bill)
	current := do(w.r, "GET", bill+"/purchase-tax", "exporter", "")
	var view struct {
		Evidence struct {
			ID string `json:"id"`
		}
	}
	require.NoError(t, json.Unmarshal(current.Body.Bytes(), &view))
	body := strings.Replace(purchaseTaxBody(), `"replaces":null`, `"replaces":"`+view.Evidence.ID+`"`, 1)
	start := make(chan struct{})
	codes := make(chan int, 2)
	var group sync.WaitGroup
	for range 2 {
		path := bill + "/purchase-tax/" + uuid.Must(uuid.NewV7()).String()
		group.Go(func() { <-start; codes <- do(w.r, "PUT", path, "exporter", body).Code })
	}
	close(start)
	group.Wait()
	close(codes)
	results := map[int]int{}
	for code := range codes {
		results[code]++
	}
	require.Equal(t, map[int]int{200: 1, 409: 1}, results)
}

func TestTallyPurchaseOptInPreservesLegacyPreviewAndExport(t *testing.T) {
	w := newTallyWorld(t)
	newPurchaseTaxBill(t, w)
	for _, option := range []string{"", "&purchase_bills=false"} {
		preview := do(w.r, "GET", tallyPath("/preview?from=2026-10-01&to=2026-11-30"+option), "exporter", "")
		require.Equal(t, 200, preview.Code, preview.Body.String())
		require.NotContains(t, preview.Body.String(), "purchase_count")
		file := do(w.r, "GET", tallyPath("/vouchers.xml?from=2026-10-01&to=2026-11-30"+option), "exporter", "")
		require.Equal(t, 200, file.Code, file.Body.String())
		require.Equal(t, "false", file.Header().Get("X-Export-Purchase-Bills"))
		require.Equal(t, "0", file.Header().Get("X-Export-Voucher-Count"))
	}
}

func TestTallyPurchaseDoesNotAdoptPartsOrIdentityOfASaleWithTheSameUUID(t *testing.T) {
	w := newTallyWorld(t)
	product := w.productAt(t, "TEST-TALLY-KIND", "none", "12")
	w.shelve(t, product, "2028-06-30", 12500, 20)
	id, _ := w.recordedSale(t, cashSale(product, 10, 12500, 12500))
	bill := payableRoot() + "/payables/" + id
	result := do(w.r, "PUT", bill, "finance", expenseBody("TEST-SAME-UUID"))
	require.Equal(t, 200, result.Code, result.Body.String())
	recordTallyPurchaseEvidence(t, w, bill)
	w.putTallyMap(t, uuid.Must(uuid.NewV7()).String(), nil, tallyLedgers)
	file := do(w.r, "GET", tallyPath("/vouchers.xml"+tallyPeriod), "exporter", "")
	require.Equal(t, 200, file.Code, file.Body.String())
	var decoded tallyFile
	require.NoError(t, xml.Unmarshal(file.Body.Bytes(), &decoded))
	require.Len(t, decoded.Vouchers, 2)
	require.NotEqual(t, decoded.Vouchers[0].RemoteID, decoded.Vouchers[1].RemoteID)
	require.Equal(t, "helivanta-"+id, decoded.Vouchers[1].RemoteID, "existing sales retain their original import identity")
	require.Len(t, decoded.Vouchers[0].Entries, 4, "purchase has only its own source entries")
	require.Len(t, decoded.Vouchers[1].Entries, 4, "sale has only its own source entries")
}
