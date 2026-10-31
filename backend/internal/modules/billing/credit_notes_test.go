package billing_test

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type creditNoteBody struct {
	ID            string `json:"id"`
	Number        string `json:"number"`
	InvoiceID     string `json:"invoice_id"`
	InvoiceNumber string `json:"invoice_number"`
	InvoiceDate   string `json:"invoice_date"`
	GSTIN         string `json:"gstin"`
	RecipientName string `json:"recipient_name"`
	IssueDate     string `json:"issue_date"`
	Lines         int    `json:"lines"`
	ValueMinor    int64  `json:"value_minor"`
	TaxableMinor  int64  `json:"taxable_minor"`
	ExemptMinor   int64  `json:"exempt_minor"`
	CGSTMinor     int64  `json:"cgst_minor"`
	SGSTMinor     int64  `json:"sgst_minor"`
	TotalMinor    int64  `json:"total_minor"`
}

func (w *bedWorld) creditNote(admission, invoice, id string) (int, creditNoteBody, string) {
	res := do(w.r, "PUT", w.root+"/admissions/"+admission+"/tax-invoices/"+invoice+"/credit-notes/"+id, "clerk", "")
	var b creditNoteBody
	_ = json.Unmarshal(res.Body.Bytes(), &b)
	return res.Code, b, res.Body.String()
}

func (w *bedWorld) creditableLines(t *testing.T, admission, invoice string) int {
	t.Helper()
	res := do(w.r, "GET", w.root+"/admissions/"+admission+"/tax-invoices/"+invoice, "nurse", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var b struct {
		CreditableLines int `json:"creditable_lines"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &b))
	return b.CreditableLines
}

// invoicedThenReversed invoices one private day and then has a late, earlier
// general stay arrive, so the census reverses the invoiced day and its tax.
func (w *bedWorld) invoicedThenReversed(t *testing.T, day string) (admission, invoice string, inv invoiceBody) {
	t.Helper()
	at, err := time.ParseInLocation(time.DateTime, day+" 10:00:00", ist)
	require.NoError(t, err)
	admission, patient := uuid.NewString(), uuid.NewString()
	first := stay{uuid.NewString(), uuid.NewString(), "private"}
	require.NoError(t, w.start(admission, patient, first, at))
	require.NoError(t, w.end(admission, first, at.Add(time.Minute), "transfer"))
	require.NoError(t, w.release(admission, patient, at.Add(2*time.Minute)))
	w.census()
	status, body := w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	invoice = v7()
	status, inv, body = w.invoice(admission, invoice, "Asha Rao")
	require.Equal(t, http.StatusCreated, status, body)
	earlier := stay{uuid.NewString(), uuid.NewString(), "general"}
	require.NoError(t, w.start(admission, patient, earlier, at.Add(-time.Minute)))
	require.NoError(t, w.end(admission, earlier, at, "transfer"))
	w.census()
	return admission, invoice, inv
}

func TestACreditNoteCreditsTheReversedDaysAndUnblocksInvoicing(t *testing.T) {
	w, day := gstWorld(t, true)
	today := time.Now().In(ist).Format(time.DateOnly)
	admission, invoice, inv := w.invoicedThenReversed(t, day)
	other, otherInvoice, _ := w.invoicedThenReversed(t, day)
	sameDay(t, day)
	require.Equal(t, 1, w.creditableLines(t, admission, invoice))
	status, body := w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	status, _, body = w.invoice(admission, v7(), "Asha Rao")
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "credit_note_required", "uncredited, the reversal still blocks")

	id := v7()
	status, note, body := w.creditNote(admission, invoice, id)
	require.Equal(t, http.StatusCreated, status, body)
	require.Equal(t, creditNoteBody{ID: id, Number: "CN/" + financialYear(today) + "/1", InvoiceID: invoice, InvoiceNumber: inv.Number, InvoiceDate: today,
		GSTIN: gstin, RecipientName: "Asha Rao", IssueDate: today, Lines: 1, ValueMinor: 600000, TaxableMinor: 600000, CGSTMinor: 15000, SGSTMinor: 15000, TotalMinor: 630000}, note,
		"exactly what was invoiced, to the invoice's recipient")
	status, again, _ := w.creditNote(admission, invoice, id)
	require.Equal(t, http.StatusOK, status, "a retried issue replays")
	require.Equal(t, note, again)
	status, _, body = w.creditNote(other, otherInvoice, id)
	require.Equal(t, http.StatusConflict, status, "the same id for another invoice: %s", body)
	status, _, body = w.creditNote(admission, invoice, v7())
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "nothing_to_credit", "a charge is credited once")
	require.Zero(t, w.creditableLines(t, admission, invoice))

	status, next, body := w.invoice(admission, v7(), "Asha Rao")
	require.Equal(t, http.StatusCreated, status, "credited, the admission is invoiced again: %s", body)
	require.EqualValues(t, 500000, next.ExemptMinor, "the general day, exempt")
	require.EqualValues(t, 0, next.TaxableMinor)
	status, _, body = w.creditNote(admission, next.ID, id)
	require.Equal(t, http.StatusConflict, status, "the same id for another invoice of the admission: %s", body)

	res := do(w.r, "GET", w.root+"/admissions/"+admission+"/credit-notes/"+id+"/lines", "nurse", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	for _, want := range []string{`"census_date":"` + day + `"`, `"bed_class":"private"`, `"sac":"9993"`, `"taxable":true`, `"cgst_bp":250`, `"cgst_minor":15000`} {
		require.Contains(t, res.Body.String(), want)
	}
	res = do(w.r, "GET", w.root+"/admissions/"+admission+"/credit-notes", "nurse", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), note.Number)
	res = do(w.r, "GET", w.root+"/admissions/"+other+"/credit-notes", "nurse", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.NotContains(t, res.Body.String(), note.Number, "an admission lists its own notes")
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.root+"/admissions/"+admission+"/credit-notes/"+id, "outsider", "").Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.root+"/admissions/"+other+"/credit-notes/"+id, "nurse", "").Code, "a note is read under its own admission")
	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", w.root+"/admissions/"+other+"/tax-invoices/"+otherInvoice+"/credit-notes/"+v7(), "nurse", "").Code)
	status, _, _ = w.creditNote(other, invoice, v7())
	require.Equal(t, http.StatusNotFound, status, "an invoice is credited under its own admission")
	status, _, _ = w.creditNote(other, otherInvoice, "not-a-uuid")
	require.Equal(t, http.StatusBadRequest, status)
}

func TestConcurrentCreditNotesAreNumberedInOrder(t *testing.T) {
	w, day := gstWorld(t, true)
	type pair struct{ admission, invoice string }
	var pairs []pair
	for range 4 {
		a, i, _ := w.invoicedThenReversed(t, day)
		pairs = append(pairs, pair{a, i})
	}
	sameDay(t, day)
	numbers := make(chan string, len(pairs))
	var wg sync.WaitGroup
	for _, p := range pairs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, note, body := w.creditNote(p.admission, p.invoice, v7())
			if status != http.StatusCreated {
				numbers <- "error " + body
				return
			}
			numbers <- note.Number
		}()
	}
	wg.Wait()
	close(numbers)
	got := map[string]bool{}
	for n := range numbers {
		got[n] = true
	}
	fy := financialYear(time.Now().In(ist).Format(time.DateOnly))
	require.Equal(t, map[string]bool{"CN/" + fy + "/1": true, "CN/" + fy + "/2": true, "CN/" + fy + "/3": true, "CN/" + fy + "/4": true}, got)
}

func TestCreditNoteRulesHoldInTheDatabase(t *testing.T) {
	w, day := gstWorld(t, true)
	admission, invoice, _ := w.invoicedThenReversed(t, day)
	private, patient := w.dayStay(t, day, "private")
	w.census()
	sameDay(t, day)
	status, body := w.post("/admissions/" + private + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	kept := v7()
	status, _, body = w.invoice(private, kept, "Ravi Kumar")
	require.Equal(t, http.StatusCreated, status, body)
	_ = patient

	var open []bool
	require.NoError(t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "system:billing-bed-census"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT billing_credit_note_open('2627',d::date) FROM unnest(ARRAY['2027-11-30','2027-12-01','2026-04-01']) d`).Scan(&open).Error
	}))
	require.Equal(t, []bool{true, false, true}, open, "open until 30 November after the invoice's year")

	asClerk := func(fn func(tx *gorm.DB) error) error {
		return w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-clerk"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.billing_subject','user-clerk',true)`).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}
	// A note with no line never commits.
	err := asClerk(func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_credit_notes (tenant_id,hospital_id,admission_id,id,invoice_id,issued_by) VALUES (?,?,?,?,?,'user-clerk')`,
			testutil.TenantA, w.hospital, private, v7(), kept).Error
	})
	requireConstraint(t, err, "billing_credit_note_complete", "a credit note carries at least one line")

	// A line is added to an issued note only as it is issued.
	issued := v7()
	status, _, body = w.creditNote(admission, invoice, issued)
	require.Equal(t, http.StatusCreated, status, body)
	var entry string
	require.NoError(t, asClerk(func(tx *gorm.DB) error {
		return tx.Raw(`SELECT entry_id FROM billing_tax_invoice_lines WHERE invoice_id=?`, invoice).Scan(&entry).Error
	}))
	err = asClerk(func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_credit_note_lines (tenant_id,hospital_id,admission_id,credit_note_id,invoice_id,entry_id) VALUES (?,?,?,?,?,?)`,
			testutil.TenantA, w.hospital, admission, issued, invoice, entry).Error
	})
	require.ErrorContains(t, err, "written only as it is issued", "no line outside its note's issue")

	// A credited charge is not credited again, even by a note issued properly.
	err = asClerk(func(tx *gorm.DB) error {
		again := v7()
		if err := tx.Exec(`INSERT INTO billing_credit_notes (tenant_id,hospital_id,admission_id,id,invoice_id,issued_by) VALUES (?,?,?,?,?,'user-clerk')`,
			testutil.TenantA, w.hospital, admission, again, invoice).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO billing_credit_note_lines (tenant_id,hospital_id,admission_id,credit_note_id,invoice_id,entry_id) VALUES (?,?,?,?,?,?)`,
			testutil.TenantA, w.hospital, admission, again, invoice, entry).Error
	})
	requireConstraint(t, err, "billing_credit_note_line", "a charge is credited once")

	// A charge still standing is not creditable.
	note := v7()
	err = asClerk(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO billing_credit_notes (tenant_id,hospital_id,admission_id,id,invoice_id,issued_by) VALUES (?,?,?,?,?,'user-clerk')`,
			testutil.TenantA, w.hospital, private, note, kept).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO billing_credit_note_lines (tenant_id,hospital_id,admission_id,credit_note_id,invoice_id,entry_id)
 SELECT tenant_id,hospital_id,admission_id,?,invoice_id,entry_id FROM billing_tax_invoice_lines WHERE invoice_id=?`, note, kept).Error
	})
	requireConstraint(t, err, "billing_credit_note_line", "only a reversed charge is credited")
}
