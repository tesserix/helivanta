package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
)

// #1386 B-2b: selling against a prescription (Drugs Rules 1945, Rule 65).

const (
	prescriberAddress = "TEST 12 Clinic Lane, Pune"
	patientAddress    = "TEST 4 Station Road, Pune"
)

// prescribe records a prescription for product: units in total over
// dispensings, with both addresses unless bare.
func (w saleWorld) prescribe(t *testing.T, product string, units, dispensings int, bare bool) string {
	t.Helper()
	addresses := fmt.Sprintf(`,"prescriber_address":%q,"patient_address":%q`, prescriberAddress, patientAddress)
	if bare {
		addresses = ""
	}
	id := uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"prescriber_name":"TEST Dr Asha Rao","prescriber_registration":"MMC/2011/04567","prescriber_council":"Maharashtra Medical Council","prescribed_on":"2026-10-30","patient_name":"TEST Ravi Kumar"%s,"lines":[{"prescribed_as":"TEST as written","product_id":%q,"quantity_units":%d,"dispensings_allowed":%d}]}`,
		addresses, product, units, dispensings)
	res := do(w.r, "PUT", licencePath("/prescriptions/"+id), "pharmacist", body)
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	return id
}

// rxSale is a cash sale of one product citing a prescription line, if rx is set.
func rxSale(product string, units, amount int, rx string, buyer string) string {
	cite := ""
	if rx != "" {
		cite = fmt.Sprintf(`,"prescription":{"prescription_id":%q,"line_number":1}`, rx)
	}
	return fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":%d%s}],"tenders":[{"kind":"cash","amount_minor":%d,"tendered_minor":%d}]%s}`, product, units, cite, amount, amount, buyer)
}

type remaining struct {
	Lines []struct {
		UnitsRemaining       int64 `json:"units_remaining"`
		DispensingsRemaining int   `json:"dispensings_remaining"`
	} `json:"lines"`
}

func (w saleWorld) remaining(t *testing.T, rx string) (int64, int) {
	t.Helper()
	res := do(w.r, "GET", licencePath("/prescriptions/"+rx), "pharmacist", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var r remaining
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &r))
	require.Len(t, r.Lines, 1)
	return r.Lines[0].UnitsRemaining, r.Lines[0].DispensingsRemaining
}

// 10 units of a pack of 10 at an MRP of 125.00 come to 125.00.
const tenUnits = 12500

func TestAScheduleHSaleDispensesAgainstItsPrescription(t *testing.T) {
	w := newSaleWorld(t, true)
	h := w.product(t, "TEST-AMOX-500", "H")
	w.shelve(t, h, "2028-06-30", 12500, 30)
	rx := w.prescribe(t, h, 20, 1, false)

	open := do(w.r, "GET", licencePath("/prescriptions/open-lines?product_id="+h), "pharmacist", "")
	require.Equal(t, http.StatusOK, open.Code, open.Body.String())
	require.Contains(t, open.Body.String(), rx)
	require.Contains(t, open.Body.String(), `"addresses_on_file":true`)

	code, sale, out := w.sell(t, rxSale(h, 10, tenUnits, rx, ""))
	require.Equal(t, http.StatusCreated, code, out)
	require.Len(t, sale.Lines, 1)
	require.Contains(t, out, `"prescription_id":"`+rx+`"`)
	require.Contains(t, out, `"prescription_line":1`)
	require.Contains(t, out, `"regulatory_class":"H"`)

	units, dispensings := w.remaining(t, rx)
	require.EqualValues(t, 10, units)
	require.Equal(t, 0, dispensings)
	empty := do(w.r, "GET", licencePath("/prescriptions/open-lines?product_id="+h), "pharmacist", "")
	require.NotContains(t, empty.Body.String(), rx, "a fully dispensed line is not offered again")
	require.Equal(t, http.StatusBadRequest, do(w.r, "GET", licencePath("/prescriptions/open-lines"), "pharmacist", "").Code)
}

func TestAPrescriptionIsDispensedOnlyAsOftenAsAllowed(t *testing.T) {
	w := newSaleWorld(t, true)
	h := w.product(t, "TEST-AMOX-500", "H")
	w.shelve(t, h, "2028-06-30", 12500, 60)

	once := w.prescribe(t, h, 30, 1, false)
	code, _, out := w.sell(t, rxSale(h, 10, tenUnits, once, ""))
	require.Equal(t, http.StatusCreated, code, out)
	code, _, out = w.sell(t, rxSale(h, 10, tenUnits, once, ""))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "has been dispensed as often as the prescriber allowed")

	thrice := w.prescribe(t, h, 20, 3, false)
	for range 2 {
		code, _, out := w.sell(t, rxSale(h, 10, tenUnits, thrice, ""))
		require.Equal(t, http.StatusCreated, code, out)
	}
	code, _, out = w.sell(t, rxSale(h, 1, 1250, thrice, ""))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "the prescription allows 0 more units of TEST-AMOX-500")
}

func TestAPrescriptionLineIsNeverSubstitutedOrExceeded(t *testing.T) {
	w := newSaleWorld(t, true)
	h := w.product(t, "TEST-AMOX-500", "H")
	other := w.product(t, "TEST-AZI-250", "H")
	w.shelve(t, h, "2028-06-30", 12500, 40)
	w.shelve(t, other, "2028-06-30", 12500, 40)
	rx := w.prescribe(t, h, 20, 2, false)

	code, _, out := w.sell(t, rxSale(other, 10, tenUnits, rx, ""))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "a prescribed medicine is not substituted")
	code, _, out = w.sell(t, rxSale(h, 30, 3*tenUnits, rx, ""))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "the prescription allows 20 more units of TEST-AMOX-500")
	code, _, out = w.sell(t, rxSale(h, 10, tenUnits, uuid.Must(uuid.NewV7()).String(), ""))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "is not recorded at this outlet")

	// A quote runs the same checks.
	quote := do(w.r, "POST", salePath("/quote"), "pharmacist", fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":10}]}`, h))
	require.Equal(t, http.StatusConflict, quote.Code)
	require.Contains(t, quote.Body.String(), "is Schedule H and needs a prescription")

	units, dispensings := w.remaining(t, rx)
	require.EqualValues(t, 20, units, "a refused sale dispenses nothing")
	require.Equal(t, 2, dispensings)
}

func TestAnOTCLineCitesNoPrescription(t *testing.T) {
	w := newSaleWorld(t, true)
	otc := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, otc, "2028-06-30", 12500, 20)
	rx := w.prescribe(t, otc, 20, 1, false)
	code, _, out := w.sell(t, rxSale(otc, 10, tenUnits, rx, ""))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "is sold without a prescription")
}

func TestHAndH1NeedBothAddressesButGDoesNot(t *testing.T) {
	w := newSaleWorld(t, true)
	for _, class := range []string{"H", "H1"} {
		p := w.product(t, "TEST-"+class+"-DRUG", class)
		w.shelve(t, p, "2028-06-30", 12500, 10)
		bare := w.prescribe(t, p, 10, 1, true)
		code, _, out := w.sell(t, rxSale(p, 10, tenUnits, bare, ""))
		require.Equal(t, http.StatusConflict, code, class)
		require.Contains(t, out, "needs the prescriber's and the patient's address")
		open := do(w.r, "GET", licencePath("/prescriptions/open-lines?product_id="+p), "pharmacist", "")
		require.Contains(t, open.Body.String(), `"addresses_on_file":false`)
	}
	// Decision 5: Schedule G needs a prescription, but Rule 65's address
	// requirements are for H and H1.
	g := w.product(t, "TEST-G-DRUG", "G")
	w.shelve(t, g, "2028-06-30", 12500, 20)
	code, _, out := w.sell(t, rxSale(g, 10, tenUnits, "", ""))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "is Schedule G and needs a prescription")
	code, _, out = w.sell(t, rxSale(g, 10, tenUnits, w.prescribe(t, g, 10, 1, true), ""))
	require.Equal(t, http.StatusCreated, code, out)
}

func TestTwoTillsCannotOverDispenseOnePrescription(t *testing.T) {
	w := newSaleWorld(t, true)
	h := w.product(t, "TEST-AMOX-500", "H")
	w.shelve(t, h, "2028-06-30", 12500, 40)
	rx := w.prescribe(t, h, 20, 1, false)
	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _, _ = w.sell(t, rxSale(h, 10, tenUnits, rx, ""))
		}()
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		if c == http.StatusCreated {
			created++
		} else {
			require.Equal(t, http.StatusConflict, c, "%v", codes)
		}
	}
	require.Equal(t, 1, created, "one prescription, dispensed once: %v", codes)
	units, dispensings := w.remaining(t, rx)
	require.EqualValues(t, 10, units)
	require.Equal(t, 0, dispensings)
}

func TestASplitAllocationIsOneDispensingAndAReplayIsNone(t *testing.T) {
	w := newSaleWorld(t, true)
	h := w.product(t, "TEST-AMOX-500", "H")
	w.shelve(t, h, "2027-06-30", 12500, 4)
	w.shelve(t, h, "2028-06-30", 12500, 20)
	rx := w.prescribe(t, h, 20, 2, false)

	id := uuid.Must(uuid.NewV7()).String()
	body := rxSale(h, 10, tenUnits, rx, "")
	res := do(w.r, "PUT", salePath("/"+id), "pharmacist", body)
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	var sale saleResp
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &sale))
	require.Len(t, sale.Lines, 2, "earliest expiry first, over two batches")
	require.Equal(t, 2, strings.Count(res.Body.String(), `"prescription_id":"`+rx+`"`), "each batch line cites the prescription")
	units, dispensings := w.remaining(t, rx)
	require.EqualValues(t, 10, units)
	require.Equal(t, 1, dispensings, "two batch lines of one sale are one dispensing")

	require.Equal(t, http.StatusOK, do(w.r, "PUT", salePath("/"+id), "pharmacist", body).Code)
	units, dispensings = w.remaining(t, rx)
	require.EqualValues(t, 10, units, "a replay dispenses nothing more")
	require.Equal(t, 1, dispensings)
	changed := strings.Replace(body, `"line_number":1`, `"line_number":2`, 1)
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", salePath("/"+id), "pharmacist", changed).Code, "a different citation is a different intent")
}

func TestAWalkInBuyerIsOptionalAndNeverPublished(t *testing.T) {
	w := newSaleWorld(t, true)
	otc := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, otc, "2028-06-30", 12500, 30)
	buyer := `,"buyer":{"name":"TEST Walk In Buyer","phone":"+919800000001"}`
	code, _, out := w.sell(t, rxSale(otc, 10, tenUnits, "", buyer))
	require.Equal(t, http.StatusCreated, code, out)
	require.Contains(t, out, `"buyer_name":"TEST Walk In Buyer"`)
	require.Contains(t, out, `"buyer_phone":"+919800000001"`)
	code, _, out = w.sell(t, rxSale(otc, 10, tenUnits, "", ""))
	require.Equal(t, http.StatusCreated, code, out)
	require.Contains(t, out, `"buyer_name":null`)
	for _, bad := range []string{`,"buyer":{"phone":"12"}`, `,"buyer":{"name":" TEST"}`, `,"buyer":{}`} {
		code, _, out := w.sell(t, rxSale(otc, 1, 1250, "", bad))
		require.Equal(t, http.StatusBadRequest, code, bad+out)
	}
	var payloads []string
	require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT payload::text FROM outbox_events WHERE subject='helivanta.in.pharmacy.sale_posted.v1'`).Scan(&payloads).Error
	}))
	require.NotEmpty(t, payloads)
	for _, p := range payloads {
		require.NotContains(t, p, "TEST Walk In Buyer")
		require.NotContains(t, p, "9800000001")
	}
}

// Review focus 4: the database alone holds a line to its prescription.
func TestTheDatabaseHoldsPrescriptionSalesToTheirPrescription(t *testing.T) {
	w := newSaleWorld(t, true)
	h := w.product(t, "TEST-AMOX-500", "H")
	other := w.product(t, "TEST-AZI-250", "H")
	w.shelve(t, h, "2028-06-30", 12500, 40)
	w.shelve(t, other, "2028-06-30", 12500, 40)
	rx := w.prescribe(t, h, 10, 1, false)
	bare := w.prescribe(t, h, 10, 1, true)
	otherRx := w.prescribe(t, other, 10, 1, false)
	_, posted, out := w.sell(t, rxSale(h, 5, 6250, rx, ""))
	require.NotEmpty(t, posted.InvoiceNumber, out)
	var postedID string
	require.NoError(t, json.Unmarshal([]byte(out), &struct {
		ID *string `json:"id"`
	}{&postedID}))

	// forge copies the posted header and its line into a new sale, changing
	// the line by change, and returns the error the line insert raises.
	forge := func(seq int, change string) error {
		return catalogueTx(t, w.db, func(tx *gorm.DB) error {
			sale := uuid.Must(uuid.NewV7())
			if err := tx.Exec(`INSERT INTO pharmacy_sales (id,tenant_id,outlet_id,financial_year,sequence_number,invoice_number,sale_date,invoice_profile_id,seller_legal_name,
				seller_address,seller_gstin,place_of_supply,currency,total_minor,taxable_minor,cgst_minor,sgst_minor,intent_sha256,actor_subject)
				SELECT ?,tenant_id,outlet_id,financial_year,?,?,sale_date,invoice_profile_id,seller_legal_name,seller_address,seller_gstin,place_of_supply,
				currency,total_minor,taxable_minor,cgst_minor,sgst_minor,intent_sha256,'user-editor' FROM pharmacy_sales WHERE id=?`, sale, seq, fmt.Sprintf("TS/2627/%d", seq), postedID).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO pharmacy_sale_lines (tenant_id,outlet_id,sale_id,line_number,product_id,batch_id,location_id,lot_number,expires_on,regulatory_class,
				schedule_declaration_id,classification_code,supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor,prescription_id,prescription_line)
				SELECT tenant_id,outlet_id,?,1,product_id,batch_id,location_id,lot_number,expires_on,regulatory_class,schedule_declaration_id,classification_code,
				supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor,prescription_id,prescription_line
				FROM pharmacy_sale_lines WHERE sale_id=?`+change, sale, postedID).Error
		})
	}
	// rx allowed one dispensing and has had it.
	require.ErrorContains(t, forge(97, ""), "a prescription line is dispensed as written")

	// Rewriting the copied line in the SELECT: another product's prescription,
	// a bare H prescription, and an OTC class carrying a citation.
	rewrite := func(seq int, class string, prescription any) error {
		return catalogueTx(t, w.db, func(tx *gorm.DB) error {
			sale := uuid.Must(uuid.NewV7())
			if err := tx.Exec(`INSERT INTO pharmacy_sales (id,tenant_id,outlet_id,financial_year,sequence_number,invoice_number,sale_date,invoice_profile_id,seller_legal_name,
				seller_address,seller_gstin,place_of_supply,currency,total_minor,taxable_minor,cgst_minor,sgst_minor,intent_sha256,actor_subject)
				SELECT ?,tenant_id,outlet_id,financial_year,?,?,sale_date,invoice_profile_id,seller_legal_name,seller_address,seller_gstin,place_of_supply,
				currency,total_minor,taxable_minor,cgst_minor,sgst_minor,intent_sha256,'user-editor' FROM pharmacy_sales WHERE id=?`, sale, seq, fmt.Sprintf("TS/2627/%d", seq), postedID).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO pharmacy_sale_lines (tenant_id,outlet_id,sale_id,line_number,product_id,batch_id,location_id,lot_number,expires_on,regulatory_class,
				schedule_declaration_id,classification_code,supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor,prescription_id,prescription_line)
				SELECT tenant_id,outlet_id,?,1,product_id,batch_id,location_id,lot_number,expires_on,?,schedule_declaration_id,classification_code,
				supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor,?::uuid,CASE WHEN ?::uuid IS NULL THEN NULL ELSE 1 END
				FROM pharmacy_sale_lines WHERE sale_id=?`, sale, class, prescription, prescription, postedID).Error
		})
	}
	require.ErrorContains(t, rewrite(96, "H", otherRx), "a prescription line is dispensed as written", "another product's prescription is a substitution")
	require.ErrorContains(t, rewrite(95, "H", bare), "a prescription line is dispensed as written", "an H line needs both addresses")
	require.ErrorContains(t, rewrite(94, "none", rx), "SQLSTATE 23514", "an OTC line cites no prescription")
	require.ErrorContains(t, rewrite(93, "H", nil), "pharmacy_sale_line_prescription_cited", "an H line cites a prescription")

	// A prescription line is never changed after it is recorded.
	err := catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE pharmacy_prescription_lines SET quantity_units=1000 WHERE prescription_id=?`, rx).Error
	})
	require.Error(t, err)
	// Nor is a line added to it later.
	err = catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_prescription_lines (tenant_id,outlet_id,prescription_id,line_number,prescribed_as,product_id,quantity_units,dispensings_allowed)
			VALUES (?,?,?,2,'TEST late',?,10,1)`, testutil.TenantA, pharmaOutlet, rx, h).Error
	})
	require.ErrorContains(t, err, "prescription lines are recorded with their prescription")
}
