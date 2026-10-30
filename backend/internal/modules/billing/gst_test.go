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
// if register, its GST registration from tomorrow, the earliest it may take
// effect. It returns tomorrow, the date the tests' stays are on.
func gstWorld(t *testing.T, register bool) (*bedWorld, string) {
	t.Helper()
	w := newBedWorld(t)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Asia/Kolkata"}`).Code)
	for class, rate := range map[string]int64{"general": 500000, "semi_private": 500020, "private": 600000, "icu": 800000, "nicu": 800000, "hdu": 600000, "neonatal": 600000} {
		res := do(w.r, "PUT", w.root+"/bed-rates/"+class+"/2020-01-01", "admin", fmt.Sprintf(`{"daily_rate_minor":%d,"currency":"INR"}`, rate))
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	}
	tomorrow := time.Now().In(ist).AddDate(0, 0, 1).Format(time.DateOnly)
	if register {
		w.register(t, tomorrow)
	}
	w.census()
	return w, tomorrow
}

func (w *bedWorld) register(t *testing.T, from string) {
	t.Helper()
	res := do(w.r, "PUT", w.root+"/gst-registration", "admin", fmt.Sprintf(`{"gstin":%q,"legal_name":"TEST General Hospital Pvt Ltd","address":"1 MG Road, Pune","effective_from":%q}`, gstin, from))
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
}

// dayStay admits to a bed of the class at 10:00 IST on day, moves out and
// releases minutes later, so its one day is the minimum day, dated day.
func (w *bedWorld) dayStay(t *testing.T, day, class string) (admission, patient string) {
	t.Helper()
	at, err := time.ParseInLocation(time.DateTime, day+" 10:00:00", ist)
	require.NoError(t, err)
	admission, patient = uuid.NewString(), uuid.NewString()
	s := stay{uuid.NewString(), uuid.NewString(), class}
	require.NoError(t, w.start(admission, patient, s, at))
	require.NoError(t, w.end(admission, s, at.Add(time.Minute), "release"))
	require.NoError(t, w.release(admission, patient, at.Add(2*time.Minute)))
	return admission, patient
}

// sameDay skips a test whose hospital day changed while it ran: tomorrow is
// then today, and the dates it was set up with no longer hold.
func sameDay(t *testing.T, tomorrow string) {
	t.Helper()
	if time.Now().In(ist).AddDate(0, 0, 1).Format(time.DateOnly) != tomorrow {
		t.Skip("the test ran across midnight in the hospital")
	}
}

func (w *bedWorld) gstLines(t *testing.T, admission string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "system:billing-bed-census"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM billing_gst_lines WHERE admission_id=?`, admission).Scan(&n).Error
	}))
	return n
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
	w, day := gstWorld(t, true)
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
		admissions[class], _ = w.dayStay(t, day, class)
	}
	w.census()
	sameDay(t, day)
	for class, tax := range want {
		require.EqualValues(t, tax, w.gstOwed(t, admissions[class]), class)
		require.Equal(t, map[bool]int64{true: 1, false: 0}[tax > 0], w.gstLines(t, admissions[class]), "%s: a line only for a taxed day", class)
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

func TestARegistrationIsSetOnceFromTomorrowAndNeedsTheTimezone(t *testing.T) {
	w := newBedWorld(t)
	today := time.Now().In(ist).Format(time.DateOnly)
	tomorrow := time.Now().In(ist).AddDate(0, 0, 1).Format(time.DateOnly)
	body := func(g, from string) string {
		return fmt.Sprintf(`{"gstin":%q,"legal_name":"TEST General","address":"Pune","effective_from":%q}`, g, from)
	}
	res := do(w.r, "PUT", w.root+"/gst-registration", "admin", body(gstin, tomorrow))
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "not_configured")
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Asia/Kolkata"}`).Code)

	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", w.root+"/gst-registration", "admin", body("27AAPFU0939F1ZX", tomorrow)).Code, "a wrong check character")
	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", w.root+"/gst-registration", "admin", body("99AAPFU0939F1ZV", tomorrow)).Code, "no such state")
	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", w.root+"/gst-registration", "admin", body("27aapfu0939f1zv", tomorrow)).Code)
	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", w.root+"/gst-registration", "clerk", body(gstin, tomorrow)).Code, "administrators only")
	// Not today: a room day dated today may already be on a settled final bill.
	for _, from := range []string{today, time.Now().In(ist).AddDate(0, 0, -1).Format(time.DateOnly)} {
		res = do(w.r, "PUT", w.root+"/gst-registration", "admin", body(gstin, from))
		require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
		require.Contains(t, res.Body.String(), "registration_backdated", from)
	}

	res = do(w.r, "PUT", w.root+"/gst-registration", "admin", body(gstin, tomorrow))
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), `"applied_at":null`)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/gst-registration", "admin", body(gstin, tomorrow)).Code, "the same again replays")
	res = do(w.r, "PUT", w.root+"/gst-registration", "admin", body("29AAGCB7383J1Z4", tomorrow))
	require.Equal(t, http.StatusConflict, res.Code)
	require.Contains(t, res.Body.String(), "already_set")
	res = do(w.r, "GET", w.root+"/gst-registration", "nurse", "")
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), gstin)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.root+"/gst-registration", "outsider", "").Code)

	// A registered hospital's rates stay in INR.
	res = do(w.r, "PUT", w.root+"/bed-rates/general/2030-01-01", "admin", `{"daily_rate_minor":100,"currency":"USD"}`)
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "rates_not_inr")
}

func TestARegistrationRefusesAHospitalWithRatesInAnotherCurrency(t *testing.T) {
	w := newBedWorld(t)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Asia/Kolkata"}`).Code)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-rates/general/2020-01-01", "admin", `{"daily_rate_minor":100,"currency":"USD"}`).Code)
	tomorrow := time.Now().In(ist).AddDate(0, 0, 1).Format(time.DateOnly)
	res := do(w.r, "PUT", w.root+"/gst-registration", "admin", fmt.Sprintf(`{"gstin":%q,"legal_name":"TEST","address":"Pune","effective_from":%q}`, gstin, tomorrow))
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "rates_not_inr")
}

// Once recorded, a registration is answered from the record: another one is
// already_set whatever its date, never refused as backdated first.
func TestARecordedRegistrationIsAnsweredFromTheRecord(t *testing.T) {
	w, _ := gstWorld(t, true)
	today := time.Now().In(ist).Format(time.DateOnly)
	res := do(w.r, "PUT", w.root+"/gst-registration", "admin", fmt.Sprintf(`{"gstin":"29AAGCB7383J1Z4","legal_name":"Other","address":"Bengaluru","effective_from":%q}`, today))
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "already_set")
}

func TestFinancialYearsRunAprilToMarch(t *testing.T) {
	w := newBedWorld(t)
	var got []string
	require.NoError(t, w.db.WithTenant(w.ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT billing_financial_year(d::date) FROM unnest(ARRAY['2026-03-31','2026-04-01','2027-01-01','2028-02-29','2099-04-01','2100-03-31']) d`).Scan(&got).Error
	}))
	require.Equal(t, []string{"2526", "2627", "2627", "2728", "9900", "9900"}, got)
}

func TestATaxInvoiceCarriesTheRoomsAfterTheFinalBill(t *testing.T) {
	w, day := gstWorld(t, true)
	today := time.Now().In(ist).Format(time.DateOnly)
	private, patient := w.dayStay(t, day, "private")
	icu, _ := w.dayStay(t, day, "icu")
	w.charge(private, patient, 2500) // a medicine: pharmacy invoices it, not the hospital
	w.census()
	sameDay(t, day)

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
	for _, want := range []string{`"census_date":"` + day + `"`, `"bed_class":"private"`, `"sac":"9993"`, `"taxable":true`, `"cgst_bp":250`, `"cgst_minor":15000`, `"sgst_minor":15000`} {
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
	w, day := gstWorld(t, false)
	private, _ := w.dayStay(t, day, "private")
	w.census()
	sameDay(t, day)
	require.Zero(t, w.gstOwed(t, private), "an unregistered hospital charges no tax")
	status, body := w.post("/admissions/" + private + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	status, _, body = w.invoice(private, v7(), "Asha Rao")
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "gst_unregistered")

	// Registered now, from tomorrow: the day already priced for tomorrow is
	// taxed, and a final bill waits until the registration reaches the account.
	w.register(t, day)
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
	w, day := gstWorld(t, true)
	at, err := time.ParseInLocation(time.DateTime, day+" 10:00:00", ist)
	require.NoError(t, err)
	admission, patient := uuid.NewString(), uuid.NewString()
	first := stay{uuid.NewString(), uuid.NewString(), "private"}
	require.NoError(t, w.start(admission, patient, first, at))
	require.NoError(t, w.end(admission, first, at.Add(time.Minute), "transfer"))
	require.NoError(t, w.release(admission, patient, at.Add(2*time.Minute)))
	w.census()
	sameDay(t, day)
	require.EqualValues(t, 30000, w.gstOwed(t, admission))
	status, body := w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	status, _, body = w.invoice(admission, v7(), "Asha Rao")
	require.Equal(t, http.StatusCreated, status, body)

	// An earlier stay in a general bed arrives late: the minimum day is the
	// first bed's, so the private day and its tax are reversed.
	earlier := stay{uuid.NewString(), uuid.NewString(), "general"}
	require.NoError(t, w.start(admission, patient, earlier, at.Add(-time.Minute)))
	require.NoError(t, w.end(admission, earlier, at, "transfer"))
	w.census()
	require.Zero(t, w.gstOwed(t, admission), "the tax went with its day; the general day is exempt")
	status, body = w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	status, _, body = w.invoice(admission, v7(), "Asha Rao")
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "credit_note_required", "never a negative line")
}

func TestConcurrentInvoicesUnderOneGSTINAreNumberedInOrder(t *testing.T) {
	w, day := gstWorld(t, true)
	var admissions []string
	for range 4 {
		a, _ := w.dayStay(t, day, "private")
		admissions = append(admissions, a)
	}
	w.census()
	sameDay(t, day)
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
	fy := financialYear(time.Now().In(ist).Format(time.DateOnly))
	require.Equal(t, map[string]bool{"IP/" + fy + "/1": true, "IP/" + fy + "/2": true, "IP/" + fy + "/3": true, "IP/" + fy + "/4": true}, got)
}

func requireConstraint(t *testing.T, err error, name, why string) {
	t.Helper()
	var pg *pgconn.PgError
	require.True(t, errors.As(err, &pg), "%s: %v", why, err)
	require.Equal(t, name, pg.ConstraintName, why)
}

func TestGSTRulesHoldInTheDatabase(t *testing.T) {
	w, day := gstWorld(t, true)
	icu, _ := w.dayStay(t, day, "icu")
	private, _ := w.dayStay(t, day, "private")
	w.census()
	sameDay(t, day)
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
	// An invoice line is written only as its invoice is issued.
	err = w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-clerk"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.billing_subject','user-clerk',true)`).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO billing_tax_invoice_lines (tenant_id,hospital_id,admission_id,invoice_id,entry_id,census_date,bed_class,sac,policy_version,value_minor,taxable,cgst_bp,sgst_bp,cgst_minor,sgst_minor)
 VALUES (?,?,?,?,?,CURRENT_DATE,'private','9993','x',1,false,0,0,0,0)`, testutil.TenantA, w.hospital, private, v7(), v7()).Error
	})
	require.Error(t, err, "no line outside its invoice's issue")
	var valid []bool
	require.NoError(t, asCensus(func(tx *gorm.DB) error {
		return tx.Raw(`SELECT billing_gstin_valid(g) FROM unnest(ARRAY['27AAPFU0939F1ZV','29AAGCB7383J1Z4','27AAPFU0939F1ZW','27AAPFU0939F1YV','00AAPFU0939F1ZV']) g`).Scan(&valid).Error
	}))
	require.Equal(t, []bool{true, true, false, false, false}, valid)
}
