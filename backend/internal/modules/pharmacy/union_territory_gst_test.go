package pharmacy_test

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/countryprofile"
	"github.com/tesserix/helivanta/pkg/tax"
	"gorm.io/gorm"
)

func syntheticJurisdictionRegistration(t *testing.T, w saleWorld, code string) {
	t.Helper()
	scheme, ok := countryprofile.Tax("IN")
	require.True(t, ok)
	for _, check := range "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		identifier := code + "PQRST5678K2Z" + string(check)
		if tax.ValidateRegistration(scheme, code, "gstin", identifier) != nil {
			continue
		}
		res := do(w.r, "PUT", taxCommandPath(registrationsPath(pharmaOutlet)), "editor", registrationBody(code, identifier, "2026-11-01", "TEST ONLY synthetic jurisdiction fixture"))
		require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
		return
	}
	t.Fatal("no valid synthetic registration for " + code)
}

func TestAllIndiaSellersQuoteLocalAndInterstateComponents(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-ALL-INDIA-1689", "none")
	w.shelve(t, product, "2028-06-30", 12500, 100)
	scheme, ok := countryprofile.Tax("IN")
	require.True(t, ok)
	for i, seller := range scheme.Jurisdictions {
		t.Run(seller.Code, func(t *testing.T) {
			syntheticJurisdictionRegistration(t, w, seller.Code)
			for _, destination := range []string{seller.Code, scheme.Jurisdictions[(i+1)%len(scheme.Jurisdictions)].Code} {
				res := do(w.r, "POST", salePath("/quote"), "pharmacist", fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":10}],"place_of_supply":%q}`, product, destination))
				require.Equal(t, http.StatusOK, res.Code, res.Body.String())
				var quote interstateDocument
				require.NoError(t, json.Unmarshal(res.Body.Bytes(), &quote))
				require.EqualValues(t, 12500, quote.Total)
				require.Equal(t, destination, quote.PlaceOfSupply)
				if destination != seller.Code {
					require.EqualValues(t, 1339, quote.IGST)
					require.Zero(t, quote.CGST+quote.SGST+quote.UTGST)
				} else {
					require.EqualValues(t, 670, quote.CGST)
					require.Zero(t, quote.IGST)
					if seller.Kind == "union_territory" {
						require.EqualValues(t, 670, quote.UTGST)
						require.Zero(t, quote.SGST)
					} else {
						require.EqualValues(t, 670, quote.SGST)
						require.Zero(t, quote.UTGST)
					}
				}
			}
		})
	}
}

func TestUnionTerritorySaleCreditsReportsAndDatabaseGuard(t *testing.T) {
	w := newSaleWorld(t, true)
	syntheticJurisdictionRegistration(t, w, "04")
	product := w.product(t, "TEST-UTGST-1689", "none")
	w.shelve(t, product, "2028-06-30", 12500, 30)
	id := uuid.Must(uuid.NewV7()).String()
	body := cashSale(product, 10, 12500, 12500)
	posted := do(w.r, "PUT", salePath("/"+id), "pharmacist", body)
	require.Equal(t, http.StatusCreated, posted.Code, posted.Body.String())
	var invoice interstateDocument
	require.NoError(t, json.Unmarshal(posted.Body.Bytes(), &invoice))
	require.EqualValues(t, 670, invoice.UTGST)
	require.EqualValues(t, 670, invoice.CGST)
	require.Zero(t, invoice.SGST+invoice.IGST)
	replay := do(w.r, "PUT", salePath("/"+id), "pharmacist", body)
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	require.JSONEq(t, posted.Body.String(), replay.Body.String())
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", salePath("/"+id), "foreign", "").Code)
	syntheticJurisdictionRegistration(t, w, "27")
	var credited interstateDocument
	for _, part := range []struct{ units, amount int }{{3, 3750}, {7, 8750}} {
		res := do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", returnBody(id, part.units, w.quarantine, int64(part.amount)))
		require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
		var note interstateDocument
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &note))
		require.Zero(t, note.SGST+note.IGST)
		credited.Total += note.Total
		credited.UTGST += note.UTGST
	}
	require.Equal(t, invoice.Total, credited.Total)
	require.Equal(t, invoice.UTGST, credited.UTGST)
	for _, route := range []string{"daily", "items"} {
		rows := csvRecords(t, do(w.r, "GET", licencePath("/reports/"+route+".csv?from=2026-11-01&to=2026-11-01"), "clerk", ""))
		require.Len(t, rows, 1)
		require.Equal(t, "6.70", rows[0]["sales_utgst"])
		require.Equal(t, "6.70", rows[0]["credits_utgst"])
		require.Equal(t, "0.00", rows[0]["sales_sgst"])
	}
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		var valid bool
		require.NoError(t, tx.Raw(`SELECT pharmacy_sale_jurisdiction_tax_matches(12500,'taxable',120000,false,true,670,0,0,670)`).Scan(&valid).Error)
		require.True(t, valid)
		require.NoError(t, tx.Raw(`SELECT pharmacy_sale_jurisdiction_tax_matches(12500,'taxable',120000,false,true,670,670,0,0)`).Scan(&valid).Error)
		require.False(t, valid)
		return nil
	}))
}

func TestUnionTerritoryTallyRequiresUTGSTMapping(t *testing.T) {
	w := newTallyWorld(t)
	syntheticJurisdictionRegistration(t, w, "04")
	product := w.product(t, "TEST-UTGST-TALLY", "none")
	w.shelve(t, product, "2028-06-30", 12500, 20)
	id, _ := w.recordedSale(t, cashSale(product, 10, 12500, 12500))
	code, _, out := w.returnUnits(t, returnBody(id, 10, w.quarantine, 12500))
	require.Equal(t, http.StatusCreated, code, out)
	unmapped := do(w.r, "GET", tallyPath("/vouchers.xml"+tallyPeriod), "exporter", "")
	require.Equal(t, http.StatusConflict, unmapped.Code, unmapped.Body.String())
	w.putTallyMap(t, uuid.Must(uuid.NewV7()).String(), nil, map[string]string{
		"voucher_type:sale": "Sales", "voucher_type:credit_note": "Credit Note", "tender:cash": "Counter Cash",
		"sales_goods:taxable:120000": "Sales 12%", "cgst:120000": "Output CGST 12%", "utgst:120000": "Output UTGST 12%",
	})
	exported := do(w.r, "GET", tallyPath("/vouchers.xml"+tallyPeriod), "exporter", "")
	require.Equal(t, http.StatusOK, exported.Code, exported.Body.String())
	var file tallyFile
	require.NoError(t, xml.Unmarshal(exported.Body.Bytes(), &file))
	require.Len(t, file.Vouchers, 2)
	for _, voucher := range file.Vouchers {
		found := false
		for _, entry := range voucher.Entries {
			if entry.Ledger == "Output UTGST 12%" {
				found = true
				require.Contains(t, []string{"6.70", "-6.70"}, entry.Amount)
			}
		}
		require.True(t, found)
	}
}

func TestUnionTerritoryServiceChargesKeepSeparateOriginalComponents(t *testing.T) {
	w := newSaleWorld(t, true)
	syntheticJurisdictionRegistration(t, w, "04")
	product := w.product(t, "TEST-UTGST-CHARGES", "none")
	w.shelve(t, product, "2028-06-30", 12500, 20)
	delivery, handling := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	w.declareCharge(t, delivery, ownCharge("TEST-DELIVERY", "counter", 1180, "2026-11-01"), "approved")
	w.declareCharge(t, handling, principalCharge("TEST-HANDLING", "counter", 1120, "2026-11-01"), "approved")
	id := uuid.Must(uuid.NewV7()).String()
	posted := do(w.r, "PUT", salePath("/"+id), "pharmacist", w.chargedCashSale(t, product, 10, 14800, delivery, handling))
	require.Equal(t, http.StatusCreated, posted.Code, posted.Body.String())
	type document struct {
		interstateDocument
		Charges []struct {
			UTGST int64 `json:"utgst_minor"`
			SGST  int64 `json:"sgst_minor"`
		} `json:"charges"`
	}
	var invoice document
	require.NoError(t, json.Unmarshal(posted.Body.Bytes(), &invoice))
	require.EqualValues(t, 820, invoice.UTGST)
	require.EqualValues(t, 820, invoice.CGST)
	require.Zero(t, invoice.SGST+invoice.IGST)
	require.Len(t, invoice.Charges, 2)
	require.EqualValues(t, 90, invoice.Charges[0].UTGST)
	require.EqualValues(t, 60, invoice.Charges[1].UTGST)
	require.Zero(t, invoice.Charges[0].SGST+invoice.Charges[1].SGST)
	lines := fmt.Sprintf(`{"sale_line":1,"quantity_units":10,"quarantine_location_id":%q}`, w.quarantine)
	credit := do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", chargeReturn(id, []int{1, 2}, lines, 14800, "cash"))
	require.Equal(t, http.StatusCreated, credit.Code, credit.Body.String())
	var note document
	require.NoError(t, json.Unmarshal(credit.Body.Bytes(), &note))
	require.Equal(t, invoice.Total, note.Total)
	require.Equal(t, invoice.UTGST, note.UTGST)
	require.Equal(t, invoice.Charges, note.Charges)
}
