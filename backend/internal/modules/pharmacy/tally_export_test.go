package pharmacy_test

import (
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
)

// #1690: Tally vouchers for sales, credit notes and supplier payments (ADR-0076).

const tallyPeriod = "?purchase_bills=true&from=2026-10-01&to=2026-11-30"

func tallyPath(suffix string) string { return payableRoot() + "/tally" + suffix }

func newTallyWorld(t *testing.T) saleWorld {
	t.Helper()
	w := newSaleWorld(t, true)
	grantCatalogue(t, w.r, "user-exporter", "editor")
	grantCatalogue(t, w.r, "user-finance", "editor")
	grantCatalogue(t, w.r, "user-auditor", "reader")
	return w
}

func (w saleWorld) recordedSale(t *testing.T, body string) (string, saleResp) {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", salePath("/"+id), "pharmacist", body)
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	var s saleResp
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &s))
	return id, s
}

func tallyMapBody(replaces *string, ledgers map[string]string) string {
	type entry struct {
		Head       string `json:"head"`
		LedgerName string `json:"ledger_name"`
	}
	entries := []entry{}
	for _, head := range slices.Sorted(maps.Keys(ledgers)) {
		entries = append(entries, entry{head, ledgers[head]})
	}
	body, _ := json.Marshal(struct {
		Replaces *string `json:"replaces"`
		Entries  []entry `json:"entries"`
	}{replaces, entries})
	return string(body)
}

type tallyReceipt struct {
	ID       string  `json:"id"`
	OutletID string  `json:"outlet_id"`
	Replaces *string `json:"replaces"`
	Entries  []struct {
		Head       string `json:"head"`
		Label      string `json:"label"`
		LedgerName string `json:"ledger_name"`
	} `json:"entries"`
	RecordedBy string `json:"recorded_by"`
	RecordedAt string `json:"recorded_at"`
}

func (w saleWorld) putTallyMap(t *testing.T, id string, replaces *string, ledgers map[string]string) tallyReceipt {
	t.Helper()
	res := do(w.r, "PUT", tallyPath("/ledger-maps/"+id), "exporter", tallyMapBody(replaces, ledgers))
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var receipt tallyReceipt
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &receipt))
	return receipt
}

type tallyPreview struct {
	OutletID    string  `json:"outlet_id"`
	From        string  `json:"from"`
	To          string  `json:"to"`
	LedgerMapID *string `json:"ledger_map_id"`
	Sales       int64   `json:"sale_count"`
	CreditNotes int64   `json:"credit_note_count"`
	Payments    int64   `json:"supplier_payment_count"`
	Reversals   int64   `json:"payment_reversal_count"`
	Purchases   int64   `json:"purchase_count"`
	Heads       []struct {
		Head         string  `json:"head"`
		Label        string  `json:"label"`
		LedgerName   *string `json:"ledger_name"`
		SupplierName *string `json:"supplier_name"`
	} `json:"heads"`
}

func (w saleWorld) tallyPreview(t *testing.T, period string) tallyPreview {
	t.Helper()
	res := do(w.r, "GET", tallyPath("/preview"+period), "exporter", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var view tallyPreview
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &view))
	return view
}

type tallyFile struct {
	XMLName  xml.Name `xml:"ENVELOPE"`
	Request  string   `xml:"HEADER>TALLYREQUEST"`
	Report   string   `xml:"BODY>IMPORTDATA>REQUESTDESC>REPORTNAME"`
	Vouchers []struct {
		RemoteID  string `xml:"REMOTEID,attr"`
		Type      string `xml:"VCHTYPE,attr"`
		Action    string `xml:"ACTION,attr"`
		Date      string `xml:"DATE"`
		TypeName  string `xml:"VOUCHERTYPENAME"`
		Number    string `xml:"VOUCHERNUMBER"`
		Narration string `xml:"NARRATION"`
		Entries   []struct {
			Ledger   string `xml:"LEDGERNAME"`
			Positive string `xml:"ISDEEMEDPOSITIVE"`
			Party    string `xml:"ISPARTYLEDGER"`
			Amount   string `xml:"AMOUNT"`
		} `xml:"LEDGERENTRIES.LIST"`
	} `xml:"BODY>IMPORTDATA>REQUESTDATA>TALLYMESSAGE>VOUCHER"`
}

// voucherText is a voucher's identity line, then one line per ledger entry.
func voucherText(f tallyFile, i int) []string {
	v := f.Vouchers[i]
	out := []string{strings.Join([]string{v.RemoteID, v.Type, v.TypeName, v.Action, v.Date, v.Number, v.Narration}, " | ")}
	for _, e := range v.Entries {
		out = append(out, strings.Join([]string{e.Positive, e.Party, e.Ledger, e.Amount}, " | "))
	}
	return out
}

func tallyDebit(ledger string, party bool, minor int64) string {
	return strings.Join([]string{"Yes", yesNo(party), ledger, "-" + rupees(minor)}, " | ")
}

func tallyCredit(ledger string, party bool, minor int64) string {
	return strings.Join([]string{"No", yesNo(party), ledger, rupees(minor)}, " | ")
}

func yesNo(v bool) string {
	if v {
		return "Yes"
	}
	return "No"
}

func tallyAuditRows(t *testing.T, w saleWorld, action string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM audit_events WHERE resource_type='pharmacy_tally_vouchers' AND action=? AND actor_subject='user-exporter'`, action).Scan(&n).Error
	}))
	return n
}

var tallyLedgers = map[string]string{
	"voucher_type:supplier_purchase":         "Purchase",
	"purchases:utilities:taxable:180000":     "Utilities 18%",
	"input_cgst:180000":                      "Input CGST 9%",
	"input_sgst:180000":                      "Input SGST 9%",
	"voucher_type:sale":                      "Sales",
	"voucher_type:credit_note":               "Credit Note",
	"voucher_type:supplier_payment":          "Payment",
	"voucher_type:supplier_payment_reversal": "Receipt",
	"tender:cash":                            "Counter Cash",
	"sales_goods:taxable:120000":             "Sales & Returns 12%",
	"sales_goods:taxable:50000":              "Sales 5%",
	"cgst:120000":                            "Output CGST 6%",
	"sgst:120000":                            "Output SGST 6%",
	"cgst:50000":                             "Output CGST 2.5%",
	"sgst:50000":                             "Output SGST 2.5%",
	"payment_method:bank":                    "HDFC Current",
	"supplier:TEST-SUPPLIER":                 "Sundry Creditors - Acme",
}

func TestTallyExportPostsEachDocumentByRateFromOneLedgerMap(t *testing.T) {
	w := newTallyWorld(t)
	twelve := w.productAt(t, "TEST-PCM-500", "none", "12")
	w.shelve(t, twelve, "2028-06-30", 12500, 30)
	five := w.productAt(t, "TEST-ORS-200", "none", "5")
	w.shelve(t, five, "2028-06-30", 10500, 30)
	mixedID, mixed := w.recordedSale(t, fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":2},{"product_id":%q,"quantity_units":1}],"tenders":[{"kind":"cash","amount_minor":3550,"tendered_minor":3550}]}`, twelve, five))
	returnedID, returned := w.recordedSale(t, cashSale(twelve, 10, 12500, 12500))
	code, cn, out := w.returnUnits(t, returnBody(returnedID, 4, w.quarantine, 5000))
	require.Equal(t, http.StatusCreated, code, out)
	bill := payableRoot() + "/payables/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, do(w.r, "PUT", bill, "finance", expenseBody("TEST-BILL-001")).Code)
	recordTallyPurchaseEvidence(t, w, bill)
	payment := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, do(w.r, "PUT", bill+"/payments/"+payment, "finance", settlementBody("payment", "TEST-BANK-001", 3000, nil)).Code)
	require.Equal(t, 200, do(w.r, "PUT", bill+"/adjustments/"+uuid.Must(uuid.NewV7()).String(), "finance", settlementBody("credit", "TEST-CREDIT", 800, nil)).Code)
	reversal := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, do(w.r, "PUT", bill+"/adjustments/"+reversal, "finance", settlementBody("payment_reversal", "TEST-REVERSAL", 3000, &payment)).Code)

	view := w.tallyPreview(t, tallyPeriod)
	require.Equal(t, pharmaOutlet, view.OutletID)
	require.Equal(t, "2026-10-01", view.From)
	require.Equal(t, "2026-11-30", view.To)
	require.Nil(t, view.LedgerMapID)
	require.Equal(t, [4]int64{2, 1, 1, 1}, [4]int64{view.Sales, view.CreditNotes, view.Payments, view.Reversals}, "a supplier credit is not a payment")
	labels := map[string]string{}
	for _, h := range view.Heads {
		require.Nil(t, h.LedgerName, h.Head)
		labels[h.Head] = h.Label
		if h.Head == "supplier:TEST-SUPPLIER" {
			require.Equal(t, "TEST Supplier", *h.SupplierName)
		} else {
			require.Nil(t, h.SupplierName, h.Head)
		}
	}
	require.Equal(t, slices.Sorted(maps.Keys(tallyLedgers)), slices.Collect(func(yield func(string) bool) {
		for _, h := range view.Heads {
			if !yield(h.Head) {
				return
			}
		}
	}), "heads come sorted, exactly those the period needs")
	require.Equal(t, "Output CGST on 5% supplies", labels["cgst:50000"], "a tax head names its supplies' combined rate")
	require.Equal(t, "Sales of goods taxable at 12%", labels["sales_goods:taxable:120000"])

	unmapped := do(w.r, "GET", tallyPath("/vouchers.xml"+tallyPeriod), "exporter", "")
	require.Equal(t, http.StatusConflict, unmapped.Code, unmapped.Body.String())
	require.Contains(t, unmapped.Body.String(), `"error":"tally_ledgers_unmapped"`)
	require.Empty(t, unmapped.Header().Get("Content-Disposition"))

	mapID := uuid.Must(uuid.NewV7()).String()
	w.putTallyMap(t, mapID, nil, tallyLedgers)
	view = w.tallyPreview(t, tallyPeriod)
	require.Equal(t, mapID, *view.LedgerMapID)
	for _, h := range view.Heads {
		require.Equal(t, tallyLedgers[h.Head], *h.LedgerName, h.Head)
	}

	export := do(w.r, "GET", tallyPath("/vouchers.xml"+tallyPeriod), "exporter", "")
	require.Equal(t, http.StatusOK, export.Code, export.Body.String())
	require.Equal(t, "application/xml; charset=utf-8", export.Header().Get("Content-Type"))
	require.Equal(t, "private, no-store", export.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", export.Header().Get("X-Content-Type-Options"))
	require.Equal(t, `attachment; filename="pharmacy-tally-vouchers-`+pharmaOutlet+`-2026-10-01-2026-11-30.xml"`, export.Header().Get("Content-Disposition"))
	require.Equal(t, "6", export.Header().Get("X-Export-Voucher-Count"))
	require.Equal(t, mapID, export.Header().Get("X-Export-Ledger-Map"))
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(export.Body.Bytes())), export.Header().Get("X-Export-Sha256"))
	require.Contains(t, export.Body.String(), "<LEDGERNAME>Sales &amp; Returns 12%</LEDGERNAME>")
	var file tallyFile
	require.NoError(t, xml.Unmarshal(export.Body.Bytes(), &file))
	require.Equal(t, "Import Data", file.Request)
	require.Equal(t, "Vouchers", file.Report)
	require.Len(t, file.Vouchers, 6)

	require.EqualValues(t, 1, view.Purchases)
	require.Equal(t, []string{
		"helivanta-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://helivanta.com/tally/purchase/"+testutil.TenantA+"/"+pharmaOutlet+"/"+strings.TrimPrefix(bill, payableRoot()+"/payables/"))).String() + " | Purchase | Purchase | Create | 20261001 | TEST-BILL-001 | Purchase bill TEST-BILL-001",
		tallyDebit("Input CGST 9%", false, 900), tallyDebit("Input SGST 9%", false, 900), tallyDebit("Utilities 18%", false, 10000),
		tallyCredit("Sundry Creditors - Acme", true, 11800),
	}, voucherText(file, 0))
	require.Equal(t, []string{
		"helivanta-" + payment + " | Payment | Payment | Create | 20261015 | TEST-BANK-001 | Payment TEST-BANK-001 against bill TEST-BILL-001",
		tallyDebit("Sundry Creditors - Acme", true, 3000),
		tallyCredit("HDFC Current", false, 3000),
	}, voucherText(file, 1))
	require.Equal(t, []string{
		"helivanta-" + reversal + " | Receipt | Receipt | Create | 20261015 | TEST-REVERSAL | Reversal TEST-REVERSAL of payment TEST-BANK-001 against bill TEST-BILL-001",
		tallyDebit("HDFC Current", false, 3000),
		tallyCredit("Sundry Creditors - Acme", true, 3000),
	}, voucherText(file, 2))

	at12, at5 := mixed.Lines[0], mixed.Lines[1]
	require.EqualValues(t, 2500, at12.AmountMinor)
	require.EqualValues(t, 1050, at5.AmountMinor)
	sgst := func(amount, taxable, cgst int64) int64 { return amount - taxable - cgst }
	for _, v := range []int64{at12.CGSTMinor, at5.CGSTMinor, sgst(at12.AmountMinor, at12.TaxableMinor, at12.CGSTMinor), sgst(at5.AmountMinor, at5.TaxableMinor, at5.CGSTMinor)} {
		require.Positive(t, v, "each rate carries both components")
	}
	require.Equal(t, []string{
		"helivanta-" + mixedID + " | Sales | Sales | Create | 20261101 | " + mixed.InvoiceNumber + " | Invoice " + mixed.InvoiceNumber,
		tallyDebit("Counter Cash", false, 3550),
		tallyCredit("Output CGST 2.5%", false, at5.CGSTMinor),
		tallyCredit("Output CGST 6%", false, at12.CGSTMinor),
		tallyCredit("Output SGST 2.5%", false, sgst(at5.AmountMinor, at5.TaxableMinor, at5.CGSTMinor)),
		tallyCredit("Output SGST 6%", false, sgst(at12.AmountMinor, at12.TaxableMinor, at12.CGSTMinor)),
		tallyCredit("Sales & Returns 12%", false, at12.TaxableMinor),
		tallyCredit("Sales 5%", false, at5.TaxableMinor),
	}, voucherText(file, 3))
	require.Equal(t, []string{
		"helivanta-" + returnedID + " | Sales | Sales | Create | 20261101 | " + returned.InvoiceNumber + " | Invoice " + returned.InvoiceNumber,
		tallyDebit("Counter Cash", false, 12500),
		tallyCredit("Output CGST 6%", false, returned.CGSTMinor),
		tallyCredit("Output SGST 6%", false, returned.SGSTMinor),
		tallyCredit("Sales & Returns 12%", false, returned.TaxableMinor),
	}, voucherText(file, 4))
	require.EqualValues(t, 5000, cn.TotalMinor)
	require.Equal(t, []string{
		"helivanta-" + cn.ID + " | Credit Note | Credit Note | Create | 20261101 | " + cn.CreditNoteNumber + " | Credit note " + cn.CreditNoteNumber + " against invoice " + returned.InvoiceNumber,
		tallyDebit("Output CGST 6%", false, cn.CGSTMinor),
		tallyDebit("Output SGST 6%", false, cn.TotalMinor-cn.TaxableMinor-cn.CGSTMinor),
		tallyDebit("Sales & Returns 12%", false, cn.TaxableMinor),
		tallyCredit("Counter Cash", false, 5000),
	}, voucherText(file, 5))
	for _, private := range []string{"TEST Ravi Kumar", "TEST 4 Station Road", "TEST prescription changed", "TEST Finance operator", "TEST settlement evidence", "TEST Supplier", "TEST-CREDIT", "TEST electricity", "IGST"} {
		require.NotContains(t, export.Body.String(), private)
	}

	again := do(w.r, "GET", tallyPath("/vouchers.xml"+tallyPeriod), "exporter", "")
	require.Equal(t, http.StatusOK, again.Code, again.Body.String())
	require.Equal(t, export.Body.Bytes(), again.Body.Bytes(), "the same records and map give the same bytes")
	require.Equal(t, export.Header().Get("X-Export-Sha256"), again.Header().Get("X-Export-Sha256"))

	require.EqualValues(t, 2, tallyAuditRows(t, w, "accounts_export"), "every delivered file is recorded; the refused one is not")
	var retained []struct {
		SHA256       string `gorm:"column:sha256"`
		SourceSHA256 string `gorm:"column:source_sha256"`
		VoucherCount int
		LedgerMapID  string
	}
	require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-exporter',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT sha256,source_sha256,voucher_count,ledger_map_id::text FROM pharmacy_tally_export_receipts WHERE tenant_id=? AND outlet_id=?`, testutil.TenantA, pharmaOutlet).Scan(&retained).Error
	}))
	require.Len(t, retained, 2, "each delivered file retains its exact digest in immutable storage")
	for _, receipt := range retained {
		require.Equal(t, export.Header().Get("X-Export-Sha256"), receipt.SHA256)
		require.Len(t, receipt.SourceSHA256, 64)
		require.Equal(t, 6, receipt.VoucherCount)
		require.Equal(t, mapID, receipt.LedgerMapID)
	}

	require.EqualValues(t, 2, tallyAuditRows(t, w, "accounts_preview"))
	var fields []string
	require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT DISTINCT array_to_string(changed_fields,',') FROM audit_events WHERE resource_type='pharmacy_tally_vouchers' AND action='accounts_export'`).Scan(&fields).Error
	}))
	require.Equal(t, []string{"from,to,ledger_map_id,sha256,voucher_count,purchase_bills"}, fields)

	empty := w.tallyPreview(t, "?from=2026-12-01&to=2026-12-31")
	require.Equal(t, [4]int64{0, 0, 0, 0}, [4]int64{empty.Sales, empty.CreditNotes, empty.Payments, empty.Reversals})
	require.Empty(t, empty.Heads)
	nothing := do(w.r, "GET", tallyPath("/vouchers.xml?from=2026-12-01&to=2026-12-31"), "exporter", "")
	require.Equal(t, http.StatusOK, nothing.Code, nothing.Body.String())
	require.Equal(t, "0", nothing.Header().Get("X-Export-Voucher-Count"))
	var none tallyFile
	require.NoError(t, xml.Unmarshal(nothing.Body.Bytes(), &none))
	require.Equal(t, "Vouchers", none.Report)
	require.Empty(t, none.Vouchers)
}

func TestTallyExportPostsChargesByTreatmentAndCreditsThemBack(t *testing.T) {
	w := newTallyWorld(t)
	p := w.productAt(t, "TEST-PCM-500", "none", "12")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	delivery := uuid.Must(uuid.NewV7()).String()
	w.declareCharge(t, delivery, ownCharge("DELIVERY", "counter", 5000, "2026-11-01"), "approved")
	handling := uuid.Must(uuid.NewV7()).String()
	w.declareCharge(t, handling, principalCharge("HANDLING", "counter", 2000, "2026-11-01"), "approved")
	saleID, sale := w.recordedSale(t, w.chargedCashSale(t, p, 2, 9500, delivery, handling))
	res := do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", chargeReturn(saleID, []int{1}, "", 5000, "cash"))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	var note struct {
		ID     string `json:"id"`
		Number string `json:"credit_note_number"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &note))

	ledgers := map[string]string{
		"voucher_type:sale":            "Sales",
		"voucher_type:credit_note":     "Credit Note",
		"tender:cash":                  "Counter Cash",
		"sales_goods:taxable:120000":   "Sales 12%",
		"sales_service:taxable:180000": "Delivery Income 18%",
		"cgst:120000":                  "Output CGST 6%",
		"sgst:120000":                  "Output SGST 6%",
		"cgst:180000":                  "Output CGST 9%",
		"sgst:180000":                  "Output SGST 9%",
	}
	view := w.tallyPreview(t, tallyPeriod)
	labels := map[string]string{}
	for _, h := range view.Heads {
		labels[h.Head] = h.Label
	}
	require.Equal(t, slices.Sorted(maps.Keys(ledgers)), slices.Sorted(maps.Keys(labels)), "an own-service charge has its own head; a principal one joins the goods")
	require.Equal(t, "Sales of own services taxable at 18%", labels["sales_service:taxable:180000"])
	w.putTallyMap(t, uuid.Must(uuid.NewV7()).String(), nil, ledgers)

	export := do(w.r, "GET", tallyPath("/vouchers.xml"+tallyPeriod), "exporter", "")
	require.Equal(t, http.StatusOK, export.Code, export.Body.String())
	var file tallyFile
	require.NoError(t, xml.Unmarshal(export.Body.Bytes(), &file))
	require.Len(t, file.Vouchers, 2)
	require.Len(t, sale.Lines, 1)
	line := sale.Lines[0]
	lineSGST := line.AmountMinor - line.TaxableMinor - line.CGSTMinor
	// 5000 at 18% inside is 4238 + 381 + 381; 2000 at 12% inside is 1786 + 107 + 107.
	require.Equal(t, []string{
		"helivanta-" + saleID + " | Sales | Sales | Create | 20261101 | " + sale.InvoiceNumber + " | Invoice " + sale.InvoiceNumber,
		tallyDebit("Counter Cash", false, 9500),
		tallyCredit("Delivery Income 18%", false, 4238),
		tallyCredit("Output CGST 6%", false, line.CGSTMinor+107),
		tallyCredit("Output CGST 9%", false, 381),
		tallyCredit("Output SGST 6%", false, lineSGST+107),
		tallyCredit("Output SGST 9%", false, 381),
		tallyCredit("Sales 12%", false, line.TaxableMinor+1786),
	}, voucherText(file, 0), "the medicine and the principal charge merge into one entry per head")
	require.Equal(t, []string{
		"helivanta-" + note.ID + " | Credit Note | Credit Note | Create | 20261101 | " + note.Number + " | Credit note " + note.Number + " against invoice " + sale.InvoiceNumber,
		tallyDebit("Delivery Income 18%", false, 4238),
		tallyDebit("Output CGST 9%", false, 381),
		tallyDebit("Output SGST 9%", false, 381),
		tallyCredit("Counter Cash", false, 5000),
	}, voucherText(file, 1), "a credited charge reverses its own head and rate")
	require.NotContains(t, export.Body.String(), "DELIVERY", "charge codes stay out")
}

func TestTallyLedgerMapVersionsAreImmutableAndSingleSuccessor(t *testing.T) {
	w := newTallyWorld(t)
	first := uuid.Must(uuid.NewV7()).String()
	path := tallyPath("/ledger-maps/" + first)
	body := tallyMapBody(nil, map[string]string{"tender:cash": "Cash"})
	put := do(w.r, "PUT", path, "exporter", body)
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	var receipt tallyReceipt
	require.NoError(t, json.Unmarshal(put.Body.Bytes(), &receipt))
	require.Equal(t, first, receipt.ID)
	require.Equal(t, pharmaOutlet, receipt.OutletID)
	require.Nil(t, receipt.Replaces)
	require.Len(t, receipt.Entries, 1)
	require.Equal(t, "tender:cash", receipt.Entries[0].Head)
	require.Equal(t, "Counter cash", receipt.Entries[0].Label)
	require.Equal(t, "Cash", receipt.Entries[0].LedgerName)
	require.Equal(t, "user-exporter", receipt.RecordedBy)
	_, err := time.Parse(time.RFC3339Nano, receipt.RecordedAt)
	require.NoError(t, err)

	replay := do(w.r, "PUT", path, "exporter", body)
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	require.JSONEq(t, put.Body.String(), replay.Body.String())
	read := do(w.r, "GET", path, "auditor", "")
	require.Equal(t, http.StatusOK, read.Code, read.Body.String())
	require.JSONEq(t, put.Body.String(), read.Body.String())

	conflict := func(id string, replaces *string, ledgers map[string]string, why string) {
		t.Helper()
		res := do(w.r, "PUT", tallyPath("/ledger-maps/"+id), "exporter", tallyMapBody(replaces, ledgers))
		require.Equal(t, http.StatusConflict, res.Code, why+": "+res.Body.String())
		require.Contains(t, res.Body.String(), `"error":"tally_ledger_map_conflict"`, why)
	}
	conflict(first, nil, map[string]string{"tender:cash": "Petty Cash"}, "same id, another intent")
	conflict(uuid.Must(uuid.NewV7()).String(), nil, map[string]string{"tender:card": "Card"}, "a second first map")
	unknown := uuid.Must(uuid.NewV7()).String()
	conflict(uuid.Must(uuid.NewV7()).String(), &unknown, map[string]string{"tender:card": "Card"}, "replacing an unknown map")

	second := uuid.Must(uuid.NewV7()).String()
	next := w.putTallyMap(t, second, &first, map[string]string{"payment_method:bank": "Bank", "tender:cash": "Cash in hand"})
	require.Equal(t, first, *next.Replaces)
	require.Len(t, next.Entries, 2)
	require.Equal(t, "payment_method:bank", next.Entries[0].Head)
	require.Equal(t, "Bank", next.Entries[0].LedgerName)
	require.Equal(t, "tender:cash", next.Entries[1].Head)
	require.Equal(t, "Cash in hand", next.Entries[1].LedgerName)
	conflict(uuid.Must(uuid.NewV7()).String(), &first, map[string]string{"tender:card": "Card"}, "a stale replaces")
	third := w.putTallyMap(t, uuid.Must(uuid.NewV7()).String(), &second, map[string]string{"tender:card": "Card"})
	require.Len(t, third.Entries, 3, "a version keeps every head it replaces")

	history := do(w.r, "GET", path, "exporter", "")
	require.JSONEq(t, put.Body.String(), history.Body.String(), "an earlier version is unchanged")
	require.Equal(t, third.ID, *w.tallyPreview(t, tallyPeriod).LedgerMapID)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", tallyPath("/ledger-maps/"+unknown), "exporter", "").Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", tallyPath("/ledger-maps/not-a-map"), "exporter", "").Code)

	require.ErrorContains(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE pharmacy_tally_ledger_map_entries SET ledger_name='TEST rewritten' WHERE map_id=?`, first).Error
	}), "cannot be removed or changed", "a version is immutable")
	dropped := uuid.Must(uuid.NewV7())
	require.ErrorContains(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO pharmacy_tally_ledger_maps(id,tenant_id,outlet_id,replaces,entry_count,intent_sha256,actor_subject) VALUES(?,?,?,?,1,?,'user-editor')`, dropped, testutil.TenantA, pharmaOutlet, third.ID, strings.Repeat("a", 64)).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_tally_ledger_map_entries(tenant_id,outlet_id,map_id,head,ledger_name) VALUES(?,?,?,'tender:cash','Cash')`, testutil.TenantA, pharmaOutlet, dropped).Error
	}), "every head it replaces", "the database refuses a version that drops a head")
	copyThird := `INSERT INTO pharmacy_tally_ledger_map_entries(tenant_id,outlet_id,map_id,head,ledger_name) SELECT tenant_id,outlet_id,?,head,ledger_name FROM pharmacy_tally_ledger_map_entries WHERE map_id=?`
	header := func(tx *gorm.DB, id uuid.UUID, count int) error {
		return tx.Exec(`INSERT INTO pharmacy_tally_ledger_maps(id,tenant_id,outlet_id,replaces,entry_count,intent_sha256,actor_subject) VALUES(?,?,?,?,?,?,'user-editor')`, id, testutil.TenantA, pharmaOutlet, third.ID, count, strings.Repeat("a", 64)).Error
	}
	require.ErrorContains(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		short := uuid.Must(uuid.NewV7())
		if err := header(tx, short, 4); err != nil {
			return err
		}
		return tx.Exec(copyThird, short, third.ID).Error
	}), "counted entries", "the database refuses a version short of its count")
	require.ErrorContains(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return header(tx, uuid.Must(uuid.NewV7()), 3)
	}), "counted entries", "the database refuses, at commit, a version written without entries")
	byEditor := uuid.Must(uuid.NewV7())
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		if err := header(tx, byEditor, 3); err != nil {
			return err
		}
		return tx.Exec(copyThird, byEditor, third.ID).Error
	}), "a complete version written by its own author is accepted")
	require.ErrorContains(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_tally_ledger_map_entries(tenant_id,outlet_id,map_id,head,ledger_name) VALUES(?,?,?,'tender:upi','UPI')`, testutil.TenantA, pharmaOutlet, byEditor).Error
	}), "counted entries", "a recorded version takes no further entries, even from its author")
	edited := byEditor.String()
	require.Len(t, w.putTallyMap(t, uuid.Must(uuid.NewV7()).String(), &edited, map[string]string{"tender:upi": "UPI"}).Entries, 4, "refused writes left the chain intact")
}

func TestTallyLedgerMapRefusesHeadsAndNamesTallyCannotTake(t *testing.T) {
	w := newTallyWorld(t)
	path := tallyPath("/ledger-maps/" + uuid.Must(uuid.NewV7()).String())
	entry := func(head, name string) string {
		return tallyMapBody(nil, map[string]string{head: name})
	}
	for why, body := range map[string]string{
		"no entries":        `{"replaces":null,"entries":[]}`,
		"unknown field":     `{"replaces":null,"entries":[{"head":"tender:cash","ledger_name":"Cash"}],"actor_subject":"user-owner"}`,
		"two objects":       `{"replaces":null,"entries":[{"head":"tender:cash","ledger_name":"Cash"}]}{}`,
		"duplicate head":    `{"replaces":null,"entries":[{"head":"tender:cash","ledger_name":"Cash"},{"head":"tender:cash","ledger_name":"Till"}]}`,
		"invalid IGST rate": entry("igst:1000001", "Output IGST"),
		"cheque tender":     entry("tender:cheque", "Cheques"),
		"lower supplier":    entry("supplier:acme", "Acme"),
		"padded rate":       entry("sales_goods:taxable:012", "Sales"),
		"empty name":        entry("tender:cash", ""),
		"leading space":     entry("tender:cash", " Cash"),
		"trailing space":    entry("tender:cash", "Cash "),
		"control character": entry("tender:cash", "Ca\u0007sh"),
		"C1 control":        entry("tender:cash", "Cash\u0085"),
		"101 characters":    entry("tender:cash", strings.Repeat("A", 101)),
	} {
		res := do(w.r, "PUT", path, "exporter", body)
		require.Equal(t, http.StatusBadRequest, res.Code, why+": "+res.Body.String())
	}
	for _, id := range []string{"not-a-uuid", uuid.NewString()} {
		res := do(w.r, "PUT", tallyPath("/ledger-maps/"+id), "exporter", entry("tender:cash", "Cash"))
		require.Equal(t, http.StatusNotFound, res.Code, id)
	}
	require.Nil(t, w.tallyPreview(t, tallyPeriod).LedgerMapID, "no refused body stored a map")
	longest := w.putTallyMap(t, uuid.Must(uuid.NewV7()).String(), nil, map[string]string{"tender:cash": strings.Repeat("Ä", 100)})
	require.Equal(t, strings.Repeat("Ä", 100), longest.Entries[0].LedgerName)
}

func TestTallyLedgerMapHoldsAtMostAThousandEntries(t *testing.T) {
	w := newTallyWorld(t)
	suppliers := func(n int) map[string]string {
		out := map[string]string{}
		for i := range n {
			out[fmt.Sprintf("supplier:TEST-%04d%s", i, strings.Repeat("Ä", 91))] = strings.Repeat("क", 100)
		}
		return out
	}
	tooMany := do(w.r, "PUT", tallyPath("/ledger-maps/"+uuid.Must(uuid.NewV7()).String()), "exporter", tallyMapBody(nil, suppliers(1001)))
	require.Equal(t, http.StatusBadRequest, tooMany.Code, tooMany.Body.String())

	escaped := func(s string) string {
		var b strings.Builder
		for _, u := range utf16.Encode([]rune(s)) {
			fmt.Fprintf(&b, `\u%04x`, u)
		}
		return b.String()
	}
	var entries []string
	for i := range 1000 {
		head := "supplier:" + string(rune(0x20000+i)) + strings.Repeat("\U00020000", 99)
		entries = append(entries, `{"`+escaped("head")+`":"`+escaped(head)+`","`+escaped("ledger_name")+`":"`+escaped(strings.Repeat("\U0001D11E", 100))+`"}`)
	}
	widest := `{"replaces":null,"entries":[` + strings.Join(entries, ",") + `]}`
	require.Greater(t, len(widest), 2<<20, "the widest full map outgrows a 2 MiB body")
	res := do(w.r, "PUT", tallyPath("/ledger-maps/"+uuid.Must(uuid.NewV7()).String()), "exporter", widest)
	require.Equal(t, http.StatusOK, res.Code, "a full map of escaped four-byte characters fits the body cap")
	var full tallyReceipt
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &full))
	require.Len(t, full.Entries, 1000)
	require.Equal(t, strings.Repeat("\U0001D11E", 100), full.Entries[0].LedgerName)
	padded := `{"replaces":null,` + strings.Repeat(" ", 3<<20) + `"entries":[{"head":"tender:cash","ledger_name":"Cash"}]}`
	res = do(w.r, "PUT", tallyPath("/ledger-maps/"+uuid.Must(uuid.NewV7()).String()), "exporter", padded)
	require.Equal(t, http.StatusBadRequest, res.Code, "a body over the cap is refused unread")
	overflow := do(w.r, "PUT", tallyPath("/ledger-maps/"+uuid.Must(uuid.NewV7()).String()), "exporter", tallyMapBody(&full.ID, map[string]string{"tender:cash": "Cash"}))
	require.Equal(t, http.StatusConflict, overflow.Code, overflow.Body.String())
	require.Contains(t, overflow.Body.String(), `"error":"tally_ledger_map_full"`)
	renamed := w.putTallyMap(t, uuid.Must(uuid.NewV7()).String(), &full.ID, map[string]string{full.Entries[0].Head: "Renamed creditor"})
	require.Len(t, renamed.Entries, 1000, "renaming a head keeps a full map within bounds")
	require.Equal(t, "Renamed creditor", renamed.Entries[0].LedgerName)
}

func TestTallyExportIsOwnerOnlyAndOutletScoped(t *testing.T) {
	w := newTallyWorld(t)
	mapPath := tallyPath("/ledger-maps/" + uuid.Must(uuid.NewV7()).String())
	body := tallyMapBody(nil, map[string]string{"tender:cash": "TEST Owner cash"})
	routes := []struct{ method, path, body string }{
		{"PUT", mapPath, body},
		{"GET", mapPath, ""},
		{"GET", tallyPath("/preview" + tallyPeriod), ""},
		{"GET", tallyPath("/vouchers.xml" + tallyPeriod), ""},
	}
	denied := func(token string, status int) {
		t.Helper()
		for _, route := range routes {
			res := do(w.r, route.method, route.path, token, route.body)
			require.Equal(t, status, res.Code, token+" "+route.method+" "+route.path+": "+res.Body.String())
			require.NotContains(t, res.Body.String(), "TEST Owner cash")
		}
	}
	denied("finance", http.StatusForbidden)
	require.Equal(t, http.StatusNotFound, do(w.r, "PUT", mapPath, "auditor", body).Code, "a reader grant cannot write a map")
	require.Equal(t, http.StatusOK, do(w.r, "PUT", mapPath, "exporter", body).Code)
	require.Equal(t, http.StatusOK, do(w.r, "GET", mapPath, "auditor", "").Code)
	require.Equal(t, http.StatusOK, do(w.r, "GET", tallyPath("/preview"+tallyPeriod), "auditor", "").Code)
	denied("finance", http.StatusForbidden)
	denied("stranger", http.StatusNotFound)
	denied("outsider", http.StatusNotFound)
	other := strings.Replace(tallyPath("/preview"+tallyPeriod), pharmaOutlet, uuid.Must(uuid.NewV7()).String(), 1)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", other, "exporter", "").Code)
}

func TestTallyLedgerMapRefusesAChangedSession(t *testing.T) {
	w := newTallyWorld(t)
	put := func(id string, subjects ...string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("PUT", tallyPath("/ledger-maps/"+id), strings.NewReader(tallyMapBody(nil, map[string]string{"tender:cash": "Cash"})))
		req.Header.Set("Authorization", "Bearer exporter")
		req.Header.Set("Content-Type", "application/json")
		for _, s := range subjects {
			req.Header.Add("X-Helivanta-Expected-Subject", s)
		}
		res := httptest.NewRecorder()
		w.r.ServeHTTP(res, req)
		return res
	}
	for _, subjects := range [][]string{{"user-finance"}, {""}, {"user-exporter", "user-exporter"}} {
		id := uuid.Must(uuid.NewV7()).String()
		res := put(id, subjects...)
		require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
		require.Contains(t, res.Body.String(), `"error":"session_changed"`)
		require.Contains(t, res.Body.String(), "Reload before saving ledger names.")
		require.Equal(t, http.StatusNotFound, do(w.r, "GET", tallyPath("/ledger-maps/"+id), "exporter", "").Code, "a refused session records no map")
	}
	require.Nil(t, w.tallyPreview(t, tallyPeriod).LedgerMapID)
	id := uuid.Must(uuid.NewV7()).String()
	res := put(id, "user-exporter")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Equal(t, id, *w.tallyPreview(t, tallyPeriod).LedgerMapID)
}

func TestTallyExportRefusesALedgerMapOtherThanThePreviewedOne(t *testing.T) {
	w := newTallyWorld(t)
	product := w.productAt(t, "TEST-PCM-500", "none", "12")
	w.shelve(t, product, "2028-06-30", 12500, 30)
	w.recordedSale(t, cashSale(product, 1, 1250, 1250))
	export := func(query string) (int, http.Header, string) {
		t.Helper()
		res := do(w.r, "GET", tallyPath("/vouchers.xml"+tallyPeriod+query), "exporter", "")
		return res.Code, res.Header(), res.Body.String()
	}
	refused := func(query string, status int, code string) {
		t.Helper()
		got, header, body := export(query)
		require.Equal(t, status, got, query+": "+body)
		require.Contains(t, body, `"error":"`+code+`"`)
		require.Empty(t, header.Get("Content-Disposition"))
	}

	refused("&ledger_map="+uuid.Must(uuid.NewV7()).String(), http.StatusConflict, "tally_ledger_map_changed")
	first := w.putTallyMap(t, uuid.Must(uuid.NewV7()).String(), nil, map[string]string{"voucher_type:sale": "Sales", "tender:cash": "Cash", "sales_goods:taxable:120000": "Sales 12%", "cgst:120000": "Output CGST 6%", "sgst:120000": "Output SGST 6%"})
	refused("&ledger_map=", http.StatusConflict, "tally_ledger_map_changed")
	got, header, _ := export("&ledger_map=" + strings.ToUpper(first.ID))
	require.Equal(t, http.StatusOK, got)
	require.Equal(t, first.ID, header.Get("X-Export-Ledger-Map"))

	second := w.putTallyMap(t, uuid.Must(uuid.NewV7()).String(), &first.ID, map[string]string{"tender:cash": "Counter Cash"})
	refused("&ledger_map="+first.ID, http.StatusConflict, "tally_ledger_map_changed")
	for _, malformed := range []string{"&ledger_map=cash", "&ledger_map=" + uuid.NewString(), "&ledger_map=" + second.ID + "&ledger_map=" + second.ID} {
		refused(malformed, http.StatusBadRequest, "invalid_request")
	}
	got, header, body := export("&ledger_map=" + second.ID)
	require.Equal(t, http.StatusOK, got, body)
	require.Equal(t, second.ID, header.Get("X-Export-Ledger-Map"))
	require.Contains(t, body, "<LEDGERNAME>Counter Cash</LEDGERNAME>")
	got, _, _ = export("")
	require.Equal(t, http.StatusOK, got, "without a fence the current map is used")
	require.EqualValues(t, 3, tallyAuditRows(t, w, "accounts_export"), "a refused file is never recorded as delivered")
}

func TestTallyExportRefusesWhatItCannotWriteFaithfully(t *testing.T) {
	w := newTallyWorld(t)
	product := w.productAt(t, "TEST-PCM-500", "none", "12")
	w.shelve(t, product, "2028-06-30", 12500, 30)
	w.recordedSale(t, cashSale(product, 1, 1250, 1250))
	w.recordedSale(t, cashSale(product, 2, 2500, 2500))
	export := tallyPath("/vouchers.xml" + tallyPeriod)
	preview := tallyPath("/preview" + tallyPeriod)
	refused := func(path string, status int, code string) {
		t.Helper()
		res := do(w.r, "GET", path, "exporter", "")
		require.Equal(t, status, res.Code, path+": "+res.Body.String())
		require.Contains(t, res.Body.String(), `"error":"`+code+`"`)
		require.Empty(t, res.Header().Get("Content-Disposition"))
		require.Empty(t, res.Header().Get("X-Export-Sha256"))
	}

	for _, period := range []string{"", "?from=2026-11-01", "?from=2026-11-02&to=2026-11-01", "?from=2025-11-01&to=2026-11-02", "?from=2026-11-01&to=2026-11-31"} {
		refused(tallyPath("/preview"+period), http.StatusBadRequest, "invalid_request")
		refused(tallyPath("/vouchers.xml"+period), http.StatusBadRequest, "invalid_request")
	}

	ledgers := map[string]string{"voucher_type:sale": "Sales", "tender:cash": "Cash", "sales_goods:taxable:120000": "Sales 12%", "cgst:120000": "Output CGST 6%"}
	partial := w.putTallyMap(t, uuid.Must(uuid.NewV7()).String(), nil, ledgers)
	refused(export, http.StatusConflict, "tally_ledgers_unmapped")
	w.putTallyMap(t, uuid.Must(uuid.NewV7()).String(), &partial.ID, map[string]string{"sgst:120000": "Output SGST 6%"})

	t.Run("too many vouchers", func(t *testing.T) {
		pharmacy.SetTallyExportVouchers(t, 1)
		refused(preview, http.StatusRequestEntityTooLarge, "accounting_export_too_large")
		refused(export, http.StatusRequestEntityTooLarge, "accounting_export_too_large")
	})
	t.Run("too many bytes", func(t *testing.T) {
		pharmacy.SetTallyExportBytes(t, 512)
		refused(export, http.StatusRequestEntityTooLarge, "accounting_export_too_large")
	})
	t.Run("more heads than a map holds", func(t *testing.T) {
		pharmacy.SetTallyMapEntries(t, 4)
		refused(preview, http.StatusRequestEntityTooLarge, "accounting_export_too_large")
		refused(export, http.StatusRequestEntityTooLarge, "accounting_export_too_large")
	})
	t.Run("unrecorded read", func(t *testing.T) {
		pharmacy.SetReadRecordFault(t)
		refused(preview, http.StatusInternalServerError, "internal")
		refused(export, http.StatusInternalServerError, "internal")
	})
	require.Zero(t, tallyAuditRows(t, w, "accounts_export"), "no refused export is recorded as delivered")
	at := do(w.r, "GET", export, "exporter", "")
	require.Equal(t, http.StatusOK, at.Code, at.Body.String())
	require.Equal(t, "2", at.Header().Get("X-Export-Voucher-Count"))

	payable := uuid.Must(uuid.NewV7())
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO pharmacy_payables(id,tenant_id,outlet_id,payee_reference,payee_name,document_reference,issued_on,due_on,currency,line_count,total_minor,actor_subject) VALUES(?,?,?,'TEST-OVERSEAS','TEST Overseas supplier','TEST-USD-1','2026-10-01','2026-10-31','USD',1,1000,'user-editor')`, payable, testutil.TenantA, pharmaOutlet).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO pharmacy_payable_lines(id,tenant_id,outlet_id,payable_id,position,category,description,net_minor,tax_minor,recoverable_tax_minor,landed_minor) VALUES(?,?,?,?,1,'other','TEST import fee',1000,0,0,0)`, uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, payable).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_settlements(id,tenant_id,outlet_id,payable_id,kind,amount_minor,currency,paid_by,method,source_reference,occurred_on,reason,actor_subject) VALUES(?,?,?,?,'payment',1000,'USD','TEST','bank','TEST-USD-PAY','2026-10-15','TEST','user-editor')`, uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, payable).Error
	}), "a currency the API does not yet admit, written directly")
	refused(preview, http.StatusConflict, "tally_currency_unsupported")
	refused(export, http.StatusConflict, "tally_currency_unsupported")
}

func recordTallyPurchaseEvidence(t *testing.T, w saleWorld, bill string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	body := purchaseTaxBody()
	response := do(w.r, "PUT", bill+"/purchase-tax/"+id, "exporter", body)
	require.Equal(t, 200, response.Code, response.Body.String())
	return body
}
