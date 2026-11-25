package pharmacy_test

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

func withDestination(body, state string) string {
	return strings.TrimSuffix(body, "}") + fmt.Sprintf(`,"place_of_supply":%q}`, state)
}

type interstateDocument struct {
	PlaceOfSupply string `json:"place_of_supply"`
	Total         int64  `json:"total_minor"`
	Taxable       int64  `json:"taxable_minor"`
	CGST          int64  `json:"cgst_minor"`
	SGST          int64  `json:"sgst_minor"`
	IGST          int64  `json:"igst_minor"`
	Lines         []struct {
		IGST int64 `json:"igst_minor"`
	} `json:"lines"`
}

func TestInterstateSaleQuotePostReplayAndPartialCredits(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-IGST-1689", "none")
	w.shelve(t, product, "2028-06-30", 12500, 30)
	quote := do(w.r, "POST", salePath("/quote"), "pharmacist", fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":10}],"place_of_supply":"21"}`, product))
	require.Equal(t, http.StatusOK, quote.Code, quote.Body.String())
	var q interstateDocument
	require.NoError(t, json.Unmarshal(quote.Body.Bytes(), &q))
	require.EqualValues(t, 12500, q.Total)
	require.EqualValues(t, 11161, q.Taxable)
	require.EqualValues(t, 1339, q.IGST)
	require.Zero(t, q.CGST)
	require.Zero(t, q.SGST)
	require.Equal(t, "21", q.PlaceOfSupply)
	id := uuid.Must(uuid.NewV7()).String()
	body := withDestination(cashSale(product, 10, 12500, 12500), "21")
	post := do(w.r, "PUT", salePath("/"+id), "pharmacist", body)
	require.Equal(t, http.StatusCreated, post.Code, post.Body.String())
	var invoice interstateDocument
	require.NoError(t, json.Unmarshal(post.Body.Bytes(), &invoice))
	require.Equal(t, q, invoice)
	replay := do(w.r, "PUT", salePath("/"+id), "pharmacist", body)
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	require.JSONEq(t, post.Body.String(), replay.Body.String())
	conflict := do(w.r, "PUT", salePath("/"+id), "pharmacist", withDestination(cashSale(product, 10, 12500, 12500), "27"))
	require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", salePath("/"+id), "foreign", "").Code)
	var credited interstateDocument
	for _, r := range []struct {
		units  int
		amount int64
	}{{3, 3749}, {7, 8751}} {
		res := do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", returnBody(id, r.units, w.quarantine, r.amount))
		require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
		var note interstateDocument
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &note))
		require.Zero(t, note.CGST)
		require.Zero(t, note.SGST)
		credited.Total += note.Total
		credited.Taxable += note.Taxable
		credited.IGST += note.IGST
	}
	require.Equal(t, invoice.Total, credited.Total)
	require.Equal(t, invoice.Taxable, credited.Taxable)
	require.Equal(t, invoice.IGST, credited.IGST)
	code, _, out := w.returnUnits(t, returnBody(id, 1, w.quarantine, 1250))
	require.Equal(t, http.StatusConflict, code, out)
	// Both JSON and CSV read the persisted IGST, on each document's own date.
	daily := do(w.r, "GET", licencePath("/reports/daily?from=2026-11-01&to=2026-11-01"), "clerk", "")
	require.Equal(t, http.StatusOK, daily.Code, daily.Body.String())
	var report struct {
		Days []struct {
			SalesIGST   int64 `json:"sales_igst_minor"`
			CreditsIGST int64 `json:"credits_igst_minor"`
		} `json:"days"`
	}
	require.NoError(t, json.Unmarshal(daily.Body.Bytes(), &report))
	require.Len(t, report.Days, 1)
	require.EqualValues(t, 1339, report.Days[0].SalesIGST)
	require.EqualValues(t, 1339, report.Days[0].CreditsIGST)
	rows := csvRecords(t, do(w.r, "GET", licencePath("/reports/daily.csv?from=2026-11-01&to=2026-11-01"), "clerk", ""))
	require.Len(t, rows, 1)
	require.Equal(t, "13.39", rows[0]["sales_igst"])
	require.Equal(t, "13.39", rows[0]["credits_igst"])
	items := csvRecords(t, do(w.r, "GET", licencePath("/reports/items.csv?from=2026-11-01&to=2026-11-01"), "clerk", ""))
	require.Len(t, items, 1)
	require.Equal(t, "13.39", items[0]["sales_igst"])
	require.Equal(t, "13.39", items[0]["credits_igst"])
}

func TestInterstateSaleRejectsUnknownDestinationAndWrongComponents(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-IGST-GUARD", "none")
	w.shelve(t, product, "2028-06-30", 12500, 30)
	res := do(w.r, "PUT", salePath("/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", withDestination(cashSale(product, 10, 12500, 12500), "99"))
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	// Exercise the database authority, independently of the HTTP calculator.
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		var valid bool
		require.NoError(t, tx.Raw(`SELECT pharmacy_sale_tax_matches(12500,'taxable',120000,true,0,0,1339)`).Scan(&valid).Error)
		require.True(t, valid)
		require.NoError(t, tx.Raw(`SELECT pharmacy_sale_tax_matches(12500,'taxable',120000,true,670,670,0)`).Scan(&valid).Error)
		require.False(t, valid)
		require.NoError(t, tx.Raw(`SELECT pharmacy_sale_tax_matches(12500,'exempt',0,true,0,0,1)`).Scan(&valid).Error)
		require.False(t, valid)
		return nil
	}))
}

func TestInterstateTallyMapsIGSTAndBalancesSaleAndCredit(t *testing.T) {
	w := newTallyWorld(t)
	product := w.product(t, "TEST-IGST-TALLY", "none")
	w.shelve(t, product, "2028-06-30", 12500, 20)
	id, sale := w.recordedSale(t, withDestination(cashSale(product, 10, 12500, 12500), "21"))
	code, credit, out := w.returnUnits(t, returnBody(id, 10, w.quarantine, 12500))
	require.Equal(t, http.StatusCreated, code, out)
	unmapped := do(w.r, "GET", tallyPath("/vouchers.xml"+tallyPeriod), "exporter", "")
	require.Equal(t, http.StatusConflict, unmapped.Code, unmapped.Body.String())
	w.putTallyMap(t, uuid.Must(uuid.NewV7()).String(), nil, map[string]string{
		"voucher_type:sale": "Sales", "voucher_type:credit_note": "Credit Note", "tender:cash": "Counter Cash",
		"sales_goods:taxable:120000": "Sales 12%", "igst:120000": "Output IGST 12%",
	})
	exported := do(w.r, "GET", tallyPath("/vouchers.xml"+tallyPeriod), "exporter", "")
	require.Equal(t, http.StatusOK, exported.Code, exported.Body.String())
	var file tallyFile
	require.NoError(t, xml.Unmarshal(exported.Body.Bytes(), &file))
	require.Len(t, file.Vouchers, 2)
	require.Equal(t, []string{
		"helivanta-" + id + " | Sales | Sales | Create | 20261101 | " + sale.InvoiceNumber + " | Invoice " + sale.InvoiceNumber,
		tallyDebit("Counter Cash", false, 12500), tallyCredit("Output IGST 12%", false, 1339), tallyCredit("Sales 12%", false, 11161),
	}, voucherText(file, 0))
	require.Equal(t, []string{
		"helivanta-" + credit.ID + " | Credit Note | Credit Note | Create | 20261101 | " + credit.CreditNoteNumber + " | Credit note " + credit.CreditNoteNumber + " against invoice " + sale.InvoiceNumber,
		tallyDebit("Output IGST 12%", false, 1339), tallyDebit("Sales 12%", false, 11161), tallyCredit("Counter Cash", false, 12500),
	}, voucherText(file, 1))
}

func TestInterstateOdishaSellerAndChangedRegistrationDoNotRepriceCredit(t *testing.T) {
	w := newSaleWorld(t, true)
	registration := do(w.r, "PUT", taxCommandPath(registrationsPath(pharmaOutlet)), "editor", registrationBody("21", "21PQRST5678K2ZD", "2026-11-01", "TEST correct synthetic seller state"))
	require.Equal(t, http.StatusCreated, registration.Code, registration.Body.String())
	product := w.product(t, "TEST-ODISHA-IGST", "none")
	w.shelve(t, product, "2028-06-30", 12500, 20)
	id := uuid.Must(uuid.NewV7()).String()
	posted := do(w.r, "PUT", salePath("/"+id), "pharmacist", withDestination(cashSale(product, 10, 12500, 12500), "27"))
	require.Equal(t, http.StatusCreated, posted.Code, posted.Body.String())
	var invoice interstateDocument
	require.NoError(t, json.Unmarshal(posted.Body.Bytes(), &invoice))
	require.Equal(t, "27", invoice.PlaceOfSupply)
	require.EqualValues(t, 1339, invoice.IGST)
	// A later same-day registration correction must not change the original tax.
	registration = do(w.r, "PUT", taxCommandPath(registrationsPath(pharmaOutlet)), "editor", registrationBody("27", "27PQRST5678K2Z1", "2026-11-01", "TEST later synthetic registration correction"))
	require.Equal(t, http.StatusCreated, registration.Code, registration.Body.String())
	credit := do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", returnBody(id, 10, w.quarantine, 12500))
	require.Equal(t, http.StatusCreated, credit.Code, credit.Body.String())
	var note interstateDocument
	require.NoError(t, json.Unmarshal(credit.Body.Bytes(), &note))
	require.EqualValues(t, 1339, note.IGST)
	require.Zero(t, note.CGST)
	require.Zero(t, note.SGST)
	require.Equal(t, invoice.PlaceOfSupply, note.PlaceOfSupply)
}

func TestInterstateDatabaseRejectsWrongHeaderAndGoodsComponents(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-IGST-TAMPER", "none")
	w.shelve(t, product, "2028-06-30", 12500, 20)
	id, _ := w.recordedSale(t, withDestination(cashSale(product, 10, 12500, 12500), "21"))
	copyHeader := func(tx *gorm.DB, newID string, wrong bool) error {
		patch := fmt.Sprintf(`{"id":%q,"invoice_number":"TS/2627/2","sequence_number":2,"actor_subject":"user-editor"}`, newID)
		if wrong {
			patch = fmt.Sprintf(`{"id":%q,"invoice_number":"TS/2627/2","sequence_number":2,"actor_subject":"user-editor","taxable_minor":11160,"cgst_minor":670,"sgst_minor":670,"igst_minor":0}`, newID)
		}
		return tx.Exec(`INSERT INTO pharmacy_sales SELECT (jsonb_populate_record(NULL::pharmacy_sales,to_jsonb(s)||?::jsonb||jsonb_build_object('posted_xact',pg_current_xact_id()::text))).* FROM pharmacy_sales s WHERE s.id=? AND s.tenant_id=?`, patch, id, testutil.TenantA).Error
	}
	err := catalogueTx(t, w.db, func(tx *gorm.DB) error { return copyHeader(tx, uuid.Must(uuid.NewV7()).String(), true) })
	require.ErrorContains(t, err, "pharmacy_sale_gst_destination")
	err = catalogueTx(t, w.db, func(tx *gorm.DB) error {
		newID := uuid.Must(uuid.NewV7()).String()
		if err := copyHeader(tx, newID, false); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_sale_lines (tenant_id,outlet_id,sale_id,line_number,product_id,batch_id,location_id,lot_number,expires_on,
            regulatory_class,schedule_declaration_id,classification_code,supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor,igst_minor,
            prescription_id,prescription_line,discount_bps,discount_minor,price_declaration_id,ceiling_minor,price_limit)
            SELECT tenant_id,outlet_id,?,line_number,product_id,batch_id,location_id,lot_number,expires_on,
            regulatory_class,schedule_declaration_id,classification_code,supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,amount_minor,11160,670,670,0,
            prescription_id,prescription_line,discount_bps,discount_minor,price_declaration_id,ceiling_minor,price_limit
            FROM pharmacy_sale_lines WHERE sale_id=? AND tenant_id=?`, newID, id, testutil.TenantA).Error
	})
	var constraint *pgconn.PgError
	require.ErrorAs(t, err, &constraint)
	require.Equal(t, "pharmacy_sale_line_gst", constraint.ConstraintName)
}
