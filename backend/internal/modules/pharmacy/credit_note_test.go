package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
)

// #1386 C-1: returns at the counter as GST credit notes.

const recipient = `"recipient":{"name":"TEST Ravi Kumar","address":"TEST 4 Station Road, Pune"}`

type creditResp struct {
	ID               string `json:"id"`
	CreditNoteNumber string `json:"credit_note_number"`
	InvoiceNumber    string `json:"invoice_number"`
	InvoiceDate      string `json:"invoice_date"`
	TotalMinor       int64  `json:"total_minor"`
	TaxableMinor     int64  `json:"taxable_minor"`
	CGSTMinor        int64  `json:"cgst_minor"`
	Lines            []struct {
		SaleLine      int    `json:"sale_line"`
		QuantityUnits int64  `json:"quantity_units"`
		AmountMinor   int64  `json:"amount_minor"`
		TaxableMinor  int64  `json:"taxable_minor"`
		CGSTMinor     int64  `json:"cgst_minor"`
		LocationID    string `json:"location_id"`
	} `json:"lines"`
}

// returnBody returns units of sale line 1 into location, refunded in cash.
func returnBody(sale string, units int, location string, refund int64) string {
	return fmt.Sprintf(`{"sale_id":%q,"lines":[{"sale_line":1,"quantity_units":%d,"quarantine_location_id":%q}],"refunds":[{"kind":"cash","amount_minor":%d}],"reason":"TEST prescription changed",%s}`,
		sale, units, location, refund, recipient)
}

func (w saleWorld) returnUnits(t *testing.T, body string) (int, creditResp, string) {
	t.Helper()
	res := do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", body)
	var cr creditResp
	if res.Code < 300 {
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &cr))
	}
	return res.Code, cr, res.Body.String()
}

// soldSale sells units of one batch line and returns the sale's id.
func (w saleWorld) soldSale(t *testing.T, product string, units, amount int) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", salePath("/"+id), "pharmacist", cashSale(product, units, amount, amount))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	return id
}

func (w saleWorld) onHand(t *testing.T, location string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT COALESCE(sum(quantity_units),0) FROM pharmacy_stock_movements WHERE location_id=?`, location).Scan(&n).Error
	}))
	return n
}

func TestAReturnIssuesACreditNoteAndQuarantinesTheStock(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	sale := w.soldSale(t, p, 10, 12500)
	before := w.onHand(t, w.quarantine)

	code, cn, out := w.returnUnits(t, returnBody(sale, 4, w.quarantine, 5000))
	require.Equal(t, http.StatusCreated, code, out)
	require.Equal(t, "TS/2627/C1", cn.CreditNoteNumber)
	require.Equal(t, "TS/2627/1", cn.InvoiceNumber, "Rule 53(1A)(g): the corresponding invoice")
	require.Equal(t, "2026-11-01", cn.InvoiceDate)
	require.EqualValues(t, 5000, cn.TotalMinor)
	require.Len(t, cn.Lines, 1)
	// The sale line charged 125.00 with each tax half h; 4 of 10 units credit
	// floor(4h/10) of each half and the rest of 50.00 as taxable value.
	_, _, half := pharmacy.PriceLine(12500, 10, 10, "taxable", 120000)
	require.EqualValues(t, half*4/10, cn.CGSTMinor, "tax reverses at the sale's rate, pro rata")
	require.EqualValues(t, 5000-2*(half*4/10), cn.TaxableMinor, "taxable is the rest of the step")
	require.Contains(t, out, `"recipient_name":"TEST Ravi Kumar"`)
	require.Equal(t, before+4, w.onHand(t, w.quarantine), "returned units are in quarantine")

	again := do(w.r, "GET", salePath("/"+sale), "clerk", "")
	require.Contains(t, again.Body.String(), `"returned_units":4`, "the sale shows what has come back")
	got := do(w.r, "GET", licencePath("/credit-notes/"+cn.ID), "clerk", "")
	require.Equal(t, http.StatusOK, got.Code, got.Body.String())
	list := do(w.r, "GET", licencePath("/credit-notes?sale_id="+sale), "clerk", "")
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), "TS/2627/C1")
	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "clerk", returnBody(sale, 1, w.quarantine, 1250)).Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", licencePath("/credit-notes/"+cn.ID), "foreign", "").Code)

	var payloads []string
	require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT payload::text FROM outbox_events WHERE subject='helivanta.in.pharmacy.credit_note_issued.v1'`).Scan(&payloads).Error
	}))
	require.Len(t, payloads, 1)
	require.Contains(t, payloads[0], "TS/2627/C1")
	require.NotContains(t, payloads[0], "TEST Ravi Kumar", "the event carries no recipient")
	require.NotContains(t, payloads[0], "Station Road")
}

func TestPartialReturnsCreditExactlyTheLine(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	// 7 units of a pack of 10 at 9.99, 6.99: a line whose pro rata does not divide evenly.
	w.shelve(t, p, "2028-06-30", 999, 20)
	sale := w.soldSale(t, p, 7, 699)
	var saleLine struct {
		Lines []struct {
			AmountMinor  int64 `json:"amount_minor"`
			TaxableMinor int64 `json:"taxable_minor"`
			CGSTMinor    int64 `json:"cgst_minor"`
		} `json:"lines"`
	}
	got := do(w.r, "GET", salePath("/"+sale), "clerk", "")
	require.NoError(t, json.Unmarshal(got.Body.Bytes(), &saleLine))
	sold := saleLine.Lines[0]

	var amount, taxable, cgst int64
	cumulative := []int64{2, 5, 7}
	for i, units := range []int{2, 3, 2} {
		step := sold.AmountMinor*cumulative[i]/7 - amount
		code, cn, out := w.returnUnits(t, returnBody(sale, units, w.quarantine, step))
		require.Equal(t, http.StatusCreated, code, out)
		amount, taxable, cgst = amount+cn.TotalMinor, taxable+cn.TaxableMinor, cgst+cn.CGSTMinor
		require.Equal(t, fmt.Sprintf("TS/2627/C%d", i+1), cn.CreditNoteNumber, "one gap-free credit note series")
	}
	require.Equal(t, sold.AmountMinor, amount, "three partial returns credit exactly what was charged")
	require.Equal(t, sold.TaxableMinor, taxable)
	require.Equal(t, sold.CGSTMinor, cgst)
	code, _, out := w.returnUnits(t, returnBody(sale, 1, w.quarantine, 1))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "0 of 7 units are left to return")
}

func TestAReturnNeverExceedsWhatWasSoldNorSkipsQuarantine(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	sale := w.soldSale(t, p, 10, 12500)
	code, _, out := w.returnUnits(t, returnBody(sale, 11, w.quarantine, 13750))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "10 of 10 units are left to return")
	code, _, out = w.returnUnits(t, returnBody(sale, 1, w.sellable, 1250))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "goes to a quarantine location")
	code, _, out = w.returnUnits(t, returnBody(sale, 1, w.quarantine, 1000))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "the refunds come to 1000 but the credit note is 1250")
	code, _, _ = w.returnUnits(t, returnBody(uuid.Must(uuid.NewV7()).String(), 1, w.quarantine, 1250))
	require.Equal(t, http.StatusNotFound, code)
	code, _, _ = w.returnUnits(t, strings.Replace(returnBody(sale, 1, w.quarantine, 1250), "TEST prescription changed", "TEST", 1))
	require.Equal(t, http.StatusBadRequest, code, "a reason is at least 5 characters")
	code, _, _ = w.returnUnits(t, strings.Replace(returnBody(sale, 1, w.quarantine, 1250), `"name":"TEST Ravi Kumar"`, `"name":""`, 1))
	require.Equal(t, http.StatusBadRequest, code, "Rule 53(1A)(f): the recipient is named")
}

func TestTwoReturnsCannotTakeTheSameLastUnit(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	sale := w.soldSale(t, p, 10, 12500)
	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _, _ = w.returnUnits(t, returnBody(sale, 10, w.quarantine, 12500))
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
	require.Equal(t, 1, created, "%v", codes)
}

func TestAReplayedReturnIssuesOneCreditNote(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	sale := w.soldSale(t, p, 10, 12500)
	id := uuid.Must(uuid.NewV7()).String()
	body := returnBody(sale, 2, w.quarantine, 2500)
	require.Equal(t, http.StatusCreated, do(w.r, "PUT", licencePath("/credit-notes/"+id), "pharmacist", body).Code)
	before := w.onHand(t, w.quarantine)
	again := do(w.r, "PUT", licencePath("/credit-notes/"+id), "pharmacist", body)
	require.Equal(t, http.StatusOK, again.Code)
	require.Contains(t, again.Body.String(), "TS/2627/C1")
	require.Equal(t, before, w.onHand(t, w.quarantine), "a replay moves no stock")
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", licencePath("/credit-notes/"+id), "pharmacist", returnBody(sale, 3, w.quarantine, 3750)).Code)
	code, cn, out := w.returnUnits(t, returnBody(sale, 1, w.quarantine, 1250))
	require.Equal(t, http.StatusCreated, code, out)
	require.Equal(t, "TS/2627/C2", cn.CreditNoteNumber, "the replay used no number")
}

func TestTheReturnDeadlineIsThirtiethNovember(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2029-06-30", 12500, 30)
	sale := w.soldSale(t, p, 10, 12500)
	// A sale of FY 2026-27: credit notes close 30 November 2027 (IST).
	pharmacy.SetClock(t, func() time.Time { return time.Date(2027, 11, 30, 18, 0, 0, 0, time.UTC) })
	code, cn, out := w.returnUnits(t, returnBody(sale, 1, w.quarantine, 1250))
	require.Equal(t, http.StatusCreated, code, out)
	require.Equal(t, "TS/2728/C1", cn.CreditNoteNumber, "numbered in the credit note's own financial year")
	pharmacy.SetClock(t, func() time.Time { return time.Date(2027, 11, 30, 18, 31, 0, 0, time.UTC) })
	code, _, out = w.returnUnits(t, returnBody(sale, 1, w.quarantine, 1250))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "closed on 2027-11-30")
}

func TestReturnedStockIsSoldOnlyAfterRelease(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	batch := w.shelve(t, p, "2028-06-30", 12500, 10)
	sale := w.soldSale(t, p, 10, 12500)
	units, _ := w.available(t, p)
	require.Zero(t, units)
	code, _, out := w.returnUnits(t, returnBody(sale, 4, w.quarantine, 5000))
	require.Equal(t, http.StatusCreated, code, out)
	units, _ = w.available(t, p)
	require.Zero(t, units, "returned medicine is not sellable until a pharmacist releases it")
	res := do(w.r, "PUT", stockPath("releases/"+uuid.Must(uuid.NewV7()).String()), "pharmacist",
		fmt.Sprintf(`{"batch_id":%q,"from_location_id":%q,"to_location_id":%q,"quantity_units":4,"reason":"TEST checked and fit"}`, batch, w.quarantine, w.sellable))
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	units, _ = w.available(t, p)
	require.EqualValues(t, 4, units)
}

// Review focus 3–4: the database alone keeps a credit note to its sale.
func TestTheDatabaseKeepsCreditNotesHonest(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	batch := w.shelve(t, p, "2028-06-30", 12500, 30)
	sale := w.soldSale(t, p, 10, 12500)
	code, cn, out := w.returnUnits(t, returnBody(sale, 2, w.quarantine, 2500))
	require.Equal(t, http.StatusCreated, code, out)

	require.ErrorContains(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_stock_movements (id,tenant_id,outlet_id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject)
			VALUES (?,?,?,?,?,'return',5,'TEST','return:`+cn.ID+`:1','user-editor')`, uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, w.quarantine, batch).Error
	}), "a return movement receives its credit line's units", "no stock arrives beyond the credit line")
	require.ErrorContains(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_credit_note_lines (tenant_id,outlet_id,credit_note_id,line_number,sale_id,sale_line,location_id,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor)
			VALUES (?,?,?,2,?,1,?,1,1250,1116,67,67)`, testutil.TenantA, pharmaOutlet, cn.ID, sale, w.quarantine).Error
	}), "an issued credit note cannot be added to")
	require.Error(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE pharmacy_credit_notes SET total_minor=1 WHERE id=?`, cn.ID).Error
	}), "a credit note is immutable")
	// A forged return of more units than are left, at amounts that are the
	// exact pro-rata step for those units, is refused on units alone.
	var line struct{ AmountMinor, CGSTMinor int64 }
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT amount_minor, cgst_minor FROM pharmacy_sale_lines WHERE sale_id=? AND line_number=1`, sale).Scan(&line).Error
	}))
	step := func(v int64) int64 { return v*11/10 - v*2/10 }
	require.ErrorContains(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		id := uuid.Must(uuid.NewV7())
		amount, half := step(line.AmountMinor), step(line.CGSTMinor)
		if err := tx.Exec(`INSERT INTO pharmacy_credit_notes (id,tenant_id,outlet_id,sale_id,financial_year,sequence_number,credit_note_number,credit_note_date,currency,
			total_minor,taxable_minor,cgst_minor,sgst_minor,reason,recipient_name,recipient_address,intent_sha256,actor_subject)
			VALUES (?,?,?,?,'2627',98,'TS/2627/C98','2026-11-01','INR',?,?,?,?,'TEST forged','TEST','TEST',repeat('a',64),'user-editor')`,
			id, testutil.TenantA, pharmaOutlet, sale, amount, amount-2*half, half, half).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_credit_note_lines (tenant_id,outlet_id,credit_note_id,line_number,sale_id,sale_line,location_id,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor)
			VALUES (?,?,?,1,?,1,?,9,?,?,?,?)`, testutil.TenantA, pharmaOutlet, id, sale, w.quarantine, amount, amount-2*half, half, half).Error
	}), "a credit line returns at most what was sold", "9 more units after 2 of 10 is beyond what was sold")
	// A forged credit note over-crediting a line is refused by the line guard.
	require.ErrorContains(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		id := uuid.Must(uuid.NewV7())
		if err := tx.Exec(`INSERT INTO pharmacy_credit_notes (id,tenant_id,outlet_id,sale_id,financial_year,sequence_number,credit_note_number,credit_note_date,currency,
			total_minor,taxable_minor,cgst_minor,sgst_minor,reason,recipient_name,recipient_address,intent_sha256,actor_subject)
			VALUES (?,?,?,?,'2627',99,'TS/2627/C99','2026-11-01','INR',2500,2232,134,134,'TEST forged','TEST','TEST',repeat('a',64),'user-editor')`,
			id, testutil.TenantA, pharmaOutlet, sale).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_credit_note_lines (tenant_id,outlet_id,credit_note_id,line_number,sale_id,sale_line,location_id,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor)
			VALUES (?,?,?,1,?,1,?,2,9999,9865,67,67)`, testutil.TenantA, pharmaOutlet, id, sale, w.quarantine).Error
	}), "at its pro-rata amount")
}
