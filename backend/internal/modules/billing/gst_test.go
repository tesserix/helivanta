package billing_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// A published GSTIN whose check character the base-36 algorithm confirms.
const gstin = "27AAPFU0939F1ZV"

// gstWorld sets up a hospital in IST with a rate per class (from 2020) and,
// if register, its GST registration from today.
func gstWorld(t *testing.T, register bool) (*bedWorld, string) {
	t.Helper()
	w := newBedWorld(t)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Asia/Kolkata"}`).Code)
	for class, rate := range map[string]int64{"general": 500000, "semi_private": 500020, "private": 600000, "icu": 800000, "nicu": 800000, "hdu": 600000, "neonatal": 600000} {
		res := do(w.r, "PUT", w.root+"/bed-rates/"+class+"/2020-01-01", "admin", fmt.Sprintf(`{"daily_rate_minor":%d,"currency":"INR"}`, rate))
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	}
	today := time.Now().In(ist).Format(time.DateOnly)
	if register {
		w.register(t, today)
	}
	w.census()
	return w, today
}

func (w *bedWorld) register(t *testing.T, from string) {
	t.Helper()
	res := do(w.r, "PUT", w.root+"/gst-registration", "admin", fmt.Sprintf(`{"gstin":%q,"legal_name":"TEST General Hospital Pvt Ltd","address":"1 MG Road, Pune","effective_from":%q}`, gstin, from))
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
}

// sameDayStay admits to a bed of the class, moves out and releases within
// the last minutes, so its one day is the minimum day, dated today.
func (w *bedWorld) sameDayStay(t *testing.T, class string) (admission, patient string) {
	t.Helper()
	admission, patient = uuid.NewString(), uuid.NewString()
	s := stay{uuid.NewString(), uuid.NewString(), class}
	require.NoError(t, w.start(admission, patient, s, time.Now().Add(-3*time.Minute)))
	require.NoError(t, w.end(admission, s, time.Now().Add(-2*time.Minute), "release"))
	require.NoError(t, w.release(admission, patient, time.Now().Add(-time.Minute)))
	return admission, patient
}

func (w *bedWorld) gstOwed(t *testing.T, admission string) int64 {
	t.Helper()
	a, _ := w.account(admission)
	for _, s := range a.Sources {
		if s.SourceModule == "gst" {
			return s.OutstandingMinor
		}
	}
	return 0
}

type invoiceBody struct {
	ID           string `json:"id"`
	Number       string `json:"number"`
	GSTIN        string `json:"gstin"`
	StateCode    string `json:"state_code"`
	IssueDate    string `json:"issue_date"`
	Lines        int    `json:"lines"`
	ValueMinor   int64  `json:"value_minor"`
	TaxableMinor int64  `json:"taxable_minor"`
	ExemptMinor  int64  `json:"exempt_minor"`
	CGSTMinor    int64  `json:"cgst_minor"`
	SGSTMinor    int64  `json:"sgst_minor"`
	TotalMinor   int64  `json:"total_minor"`
}

func (w *bedWorld) invoice(admission, id, name string) (int, invoiceBody, string) {
	res := do(w.r, "PUT", w.root+"/admissions/"+admission+"/tax-invoices/"+id, "clerk", fmt.Sprintf(`{"recipient_name":%q,"recipient_address":"12 Park Street, Pune"}`, name))
	var b invoiceBody
	_ = json.Unmarshal(res.Body.Bytes(), &b)
	return res.Code, b, res.Body.String()
}

func financialYear(day string) string {
	d, _ := time.Parse(time.DateOnly, day)
	start := d.Year()
	if d.Month() < time.April {
		start--
	}
	return fmt.Sprintf("%02d%02d", start%100, (start+1)%100)
}

func TestRoomDaysAreTaxedByThePolicy(t *testing.T) {
	w, today := gstWorld(t, true)
	want := map[string]int64{
		"general":      0,     // exactly ₹5,000 is not above the threshold
		"semi_private": 25002, // ₹5,000.20: 12500.5 paise each, rounded half up
		"private":      30000, // 5% of ₹6,000
		"icu":          0,     // intensive care is exempt at any rate
		"nicu":         0,
		"hdu":          30000, // not in the instrument's ICU/CCU/ICCU/NICU list
		"neonatal":     30000, // a newborn ward, not a NICU
	}
	admissions := map[string]string{}
	for class := range want {
		admissions[class], _ = w.sameDayStay(t, class)
	}
	w.census()
	if time.Now().In(ist).Format(time.DateOnly) != today {
		t.Skip("the stays crossed midnight in the hospital")
	}
	for class, tax := range want {
		require.EqualValues(t, tax, w.gstOwed(t, admissions[class]), class)
	}
	var lines []struct {
		PolicyVersion string
		Sac           string
		TaxableMinor  int64
		CgstBp        int
		SgstBp        int
	}
	require.NoError(t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "system:billing-bed-census"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT policy_version,sac,taxable_minor,cgst_bp,sgst_bp FROM billing_gst_lines WHERE admission_id=?`, admissions["semi_private"]).Scan(&lines).Error
	}))
	require.Len(t, lines, 1)
	require.Equal(t, "IN-ROOM-2022-07-18", lines[0].PolicyVersion)
	require.Equal(t, "9993", lines[0].Sac)
	require.EqualValues(t, 500020, lines[0].TaxableMinor)
	require.Equal(t, 250, lines[0].CgstBp)
	require.Equal(t, 250, lines[0].SgstBp)

	// Taxed once: the census again posts nothing more.
	w.census()
	require.EqualValues(t, 30000, w.gstOwed(t, admissions["private"]))
}

func TestARegistrationIsSetOnceFromTodayAndNeedsTheTimezone(t *testing.T) {
	w := newBedWorld(t)
	today := time.Now().In(ist).Format(time.DateOnly)
	body := func(g, from string) string {
		return fmt.Sprintf(`{"gstin":%q,"legal_name":"TEST General","address":"Pune","effective_from":%q}`, g, from)
	}
	res := do(w.r, "PUT", w.root+"/gst-registration", "admin", body(gstin, today))
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "not_configured")
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Asia/Kolkata"}`).Code)

	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", w.root+"/gst-registration", "admin", body("27AAPFU0939F1ZX", today)).Code, "a wrong check character")
	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", w.root+"/gst-registration", "admin", body("99AAPFU0939F1ZV", today)).Code, "no such state")
	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", w.root+"/gst-registration", "admin", body("27aapfu0939f1zv", today)).Code)
	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", w.root+"/gst-registration", "clerk", body(gstin, today)).Code, "administrators only")
	yesterday := time.Now().In(ist).AddDate(0, 0, -1).Format(time.DateOnly)
	res = do(w.r, "PUT", w.root+"/gst-registration", "admin", body(gstin, yesterday))
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "registration_backdated")

	res = do(w.r, "PUT", w.root+"/gst-registration", "admin", body(gstin, today))
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), `"applied_at":null`)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/gst-registration", "admin", body(gstin, today)).Code, "the same again replays")
	res = do(w.r, "PUT", w.root+"/gst-registration", "admin", body("29AAGCB7383J1Z4", today))
	require.Equal(t, http.StatusConflict, res.Code)
	require.Contains(t, res.Body.String(), "already_set")
	res = do(w.r, "GET", w.root+"/gst-registration", "nurse", "")
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), gstin)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.root+"/gst-registration", "outsider", "").Code)

	// A registered hospital's rates stay in INR.
	res = do(w.r, "PUT", w.root+"/bed-rates/general/2030-01-01", "admin", `{"daily_rate_minor":100,"currency":"USD"}`)
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
}

func TestATaxInvoiceCarriesTheRoomsAfterTheFinalBill(t *testing.T) {
	w, today := gstWorld(t, true)
	private, patient := w.sameDayStay(t, "private")
	icu, _ := w.sameDayStay(t, "icu")
	w.charge(private, patient, 2500) // a medicine: pharmacy invoices it, not the hospital
	w.census()
	if time.Now().In(ist).Format(time.DateOnly) != today {
		t.Skip("the stays crossed midnight in the hospital")
	}

	status, _, body := w.invoice(private, v7(), "Asha Rao")
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "no_final_bill")
	status, body = w.post("/admissions/" + private + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)

	id := v7()
	status, inv, body := w.invoice(private, id, "Asha Rao")
	require.Equal(t, http.StatusCreated, status, body)
	require.Equal(t, "IP/"+financialYear(today)+"/1", inv.Number)
	require.Equal(t, gstin, inv.GSTIN)
	require.Equal(t, "27", inv.StateCode)
	require.Equal(t, today, inv.IssueDate)
	require.Equal(t, invoiceBody{ID: id, Number: inv.Number, GSTIN: gstin, StateCode: "27", IssueDate: today,
		Lines: 1, ValueMinor: 600000, TaxableMinor: 600000, ExemptMinor: 0, CGSTMinor: 15000, SGSTMinor: 15000, TotalMinor: 630000}, inv, "the medicine is not on it")
	status, again, _ := w.invoice(private, id, "Asha Rao")
	require.Equal(t, http.StatusOK, status, "a retried issue replays")
	require.Equal(t, inv, again)
	status, _, _ = w.invoice(private, id, "Someone Else")
	require.Equal(t, http.StatusConflict, status, "the same id for another recipient")
	status, _, body = w.invoice(private, v7(), "Asha Rao")
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "nothing_to_invoice", "a room charge is on one invoice")

	res := do(w.r, "GET", w.root+"/admissions/"+private+"/tax-invoices/"+id+"/lines", "nurse", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	for _, want := range []string{`"census_date":"` + today + `"`, `"bed_class":"private"`, `"sac":"9993"`, `"taxable":true`, `"cgst_bp":250`, `"cgst_minor":15000`, `"sgst_minor":15000`} {
		require.Contains(t, res.Body.String(), want)
	}
	res = do(w.r, "GET", w.root+"/admissions/"+private+"/tax-invoices", "nurse", "")
	require.Contains(t, res.Body.String(), inv.Number)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.root+"/admissions/"+private+"/tax-invoices/"+id, "outsider", "").Code)
	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", w.root+"/admissions/"+private+"/tax-invoices/"+v7(), "nurse", `{"recipient_name":"A","recipient_address":"B"}`).Code)
	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", w.root+"/admissions/"+private+"/tax-invoices/"+v7(), "clerk", `{"recipient_name":" ","recipient_address":"B"}`).Code, "a recipient is required")

	// The next invoice is the next number; an exempt room is a bill-of-supply line.
	status, body = w.post("/admissions/" + icu + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	status, exempt, body := w.invoice(icu, v7(), "Ravi Kumar")
	require.Equal(t, http.StatusCreated, status, body)
	require.Equal(t, "IP/"+financialYear(today)+"/2", exempt.Number)
	require.EqualValues(t, 800000, exempt.ExemptMinor)
	require.EqualValues(t, 0, exempt.TaxableMinor)
	require.EqualValues(t, 800000, exempt.TotalMinor)
}

func TestNoTaxOrInvoiceWithoutARegistration(t *testing.T) {
	w, today := gstWorld(t, false)
	private, _ := w.sameDayStay(t, "private")
	w.census()
	if time.Now().In(ist).Format(time.DateOnly) != today {
		t.Skip("the stay crossed midnight in the hospital")
	}
	require.Zero(t, w.gstOwed(t, private), "an unregistered hospital charges no tax")
	status, body := w.post("/admissions/" + private + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	status, _, body = w.invoice(private, v7(), "Asha Rao")
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "gst_unregistered")

	// Registered now, from today: the day already priced is taxed, and a
	// final bill waits until the registration reaches the account.
	w.register(t, today)
	status, body = w.post("/admissions/" + private + "/final-bills")
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "bed_days_pending")
	w.census()
	require.EqualValues(t, 30000, w.gstOwed(t, private))
	status, body = w.post("/admissions/" + private + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	status, inv, body := w.invoice(private, v7(), "Asha Rao")
	require.Equal(t, http.StatusCreated, status, body)
	require.EqualValues(t, 630000, inv.TotalMinor)
}

func TestAVoidedDayTakesItsTaxAndAnInvoicedOneNeedsACreditNote(t *testing.T) {
	w, today := gstWorld(t, true)
	admission, patient := uuid.NewString(), uuid.NewString()
	first := stay{uuid.NewString(), uuid.NewString(), "private"}
	require.NoError(t, w.start(admission, patient, first, time.Now().Add(-3*time.Minute)))
	require.NoError(t, w.end(admission, first, time.Now().Add(-2*time.Minute), "transfer"))
	require.NoError(t, w.release(admission, patient, time.Now().Add(-time.Minute)))
	w.census()
	if time.Now().In(ist).Format(time.DateOnly) != today {
		t.Skip("the stay crossed midnight in the hospital")
	}
	require.EqualValues(t, 30000, w.gstOwed(t, admission))
	status, body := w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	status, _, body = w.invoice(admission, v7(), "Asha Rao")
	require.Equal(t, http.StatusCreated, status, body)

	// An earlier stay in a general bed arrives late: the minimum day is the
	// first bed's, so the private day and its tax are reversed.
	earlier := stay{uuid.NewString(), uuid.NewString(), "general"}
	require.NoError(t, w.start(admission, patient, earlier, time.Now().Add(-4*time.Minute)))
	require.NoError(t, w.end(admission, earlier, time.Now().Add(-3*time.Minute), "transfer"))
	w.census()
	require.Zero(t, w.gstOwed(t, admission), "the tax went with its day; the general day is exempt")
	status, body = w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	status, _, body = w.invoice(admission, v7(), "Asha Rao")
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "credit_note_required", "never a negative line")
}

func TestConcurrentInvoicesUnderOneGSTINAreNumberedInOrder(t *testing.T) {
	w, today := gstWorld(t, true)
	var admissions []string
	for range 4 {
		a, _ := w.sameDayStay(t, "private")
		admissions = append(admissions, a)
	}
	w.census()
	if time.Now().In(ist).Format(time.DateOnly) != today {
		t.Skip("the stays crossed midnight in the hospital")
	}
	for _, a := range admissions {
		status, body := w.post("/admissions/" + a + "/final-bills")
		require.Equal(t, http.StatusCreated, status, body)
	}
	numbers := make(chan string, len(admissions))
	var wg sync.WaitGroup
	for _, a := range admissions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, inv, body := w.invoice(a, v7(), "Asha Rao")
			if status != http.StatusCreated {
				numbers <- "error " + body
				return
			}
			numbers <- inv.Number
		}()
	}
	wg.Wait()
	close(numbers)
	got := map[string]bool{}
	for n := range numbers {
		got[n] = true
	}
	fy := financialYear(today)
	require.Equal(t, map[string]bool{"IP/" + fy + "/1": true, "IP/" + fy + "/2": true, "IP/" + fy + "/3": true, "IP/" + fy + "/4": true}, got)
}

func requireConstraint(t *testing.T, err error, name, why string) {
	t.Helper()
	var pg *pgconn.PgError
	require.True(t, errors.As(err, &pg), "%s: %v", why, err)
	require.Equal(t, name, pg.ConstraintName, why)
}

func TestGSTRulesHoldInTheDatabase(t *testing.T) {
	w, today := gstWorld(t, true)
	icu, _ := w.sameDayStay(t, "icu")
	private, _ := w.sameDayStay(t, "private")
	w.census()
	if time.Now().In(ist).Format(time.DateOnly) != today {
		t.Skip("the stays crossed midnight in the hospital")
	}
	asCensus := func(fn func(tx *gorm.DB) error) error {
		return w.db.WithTenant(tenantdb.WithActor(w.ctx, "system:billing-bed-census"), testutil.TenantA, fn)
	}
	var icuDay, privateDay string
	require.NoError(t, asCensus(func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT id FROM billing_bed_days WHERE admission_id=? AND voided_at IS NULL`, icu).Scan(&icuDay).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT id FROM billing_bed_days WHERE admission_id=? AND voided_at IS NULL`, private).Scan(&privateDay).Error
	}))
	err := asCensus(func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_gst_lines (tenant_id,bed_day_id) VALUES (?,?)`, testutil.TenantA, icuDay).Error
	})
	requireConstraint(t, err, "billing_gst_line_taxable", "an exempt room has no tax")
	err = asCensus(func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_account_entries (tenant_id,hospital_id,id,admission_id,kind,source_module,source_reference,source_document,charge_reference,amount_minor,currency,occurred_at)
 VALUES (?,?,?,?,'charge','gst',?,'x',?,1,'INR',now())`, testutil.TenantA, w.hospital, v7(), icu, icuDay, icuDay).Error
	})
	requireConstraint(t, err, "billing_gst_entry", "a tax charge is its day's recorded tax")
	err = asCensus(func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_account_entries (tenant_id,hospital_id,id,admission_id,kind,source_module,source_reference,source_document,charge_reference,amount_minor,currency,occurred_at)
 VALUES (?,?,?,?,'charge','gst',?,'x',?,29999,'INR',now())`, testutil.TenantA, w.hospital, v7(), private, privateDay, privateDay).Error
	})
	requireConstraint(t, err, "billing_gst_entry", "not the day's tax of 30000")
	err = asCensus(func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_gst_lines SET cgst_minor=0 WHERE bed_day_id=?`, privateDay).Error
	})
	require.Error(t, err, "a GST line is fixed")
	var valid []bool
	require.NoError(t, asCensus(func(tx *gorm.DB) error {
		return tx.Raw(`SELECT billing_gstin_valid(g) FROM unnest(ARRAY['27AAPFU0939F1ZV','29AAGCB7383J1Z4','27AAPFU0939F1ZW','27AAPFU0939F1YV','00AAPFU0939F1ZV']) g`).Scan(&valid).Error
	}))
	require.Equal(t, []bool{true, true, false, false, false}, valid)
}
