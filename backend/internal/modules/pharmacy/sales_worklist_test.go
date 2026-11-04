package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract" //nolint:depguard // external test feeding the consumer its published contract
	"github.com/tesserix/helivanta/internal/modules/pharmacy"                                     //nolint:depguard // external test importing its own module
)

// #987 slice 1: the sales worklist and each sale's summary.

type workRow struct {
	ID                string  `json:"id"`
	InvoiceNumber     string  `json:"invoice_number"`
	SaleDate          string  `json:"sale_date"`
	Customer          string  `json:"customer"`
	Linkage           string  `json:"linkage"`
	BedCode           *string `json:"admission_bed_code"`
	Currency          string  `json:"currency"`
	TotalMinor        int64   `json:"total_minor"`
	DiscountMinor     int64   `json:"discount_minor"`
	Units             int64   `json:"units"`
	CreditedMinor     int64   `json:"credited_minor"`
	NetMinor          int64   `json:"net_minor"`
	AdmissionDueMinor int64   `json:"admission_outstanding_minor"`
}

type summaryResp struct {
	ID            string `json:"id"`
	InvoiceNumber string `json:"invoice_number"`
	Customer      string `json:"customer"`
	TotalMinor    int64  `json:"total_minor"`
	Lines         []struct {
		LineNumber    int   `json:"line_number"`
		Units         int64 `json:"quantity_units"`
		ReturnedUnits int64 `json:"returned_units"`
		AmountMinor   int64 `json:"amount_minor"`
		CreditedMinor int64 `json:"credited_minor"`
	} `json:"lines"`
	Charges []struct {
		ChargeNumber  int   `json:"charge_number"`
		AmountMinor   int64 `json:"amount_minor"`
		CreditedMinor int64 `json:"credited_minor"`
	} `json:"charges"`
	Payments    map[string]int64 `json:"payments"`
	CreditNotes []struct {
		CreditNoteNumber string           `json:"credit_note_number"`
		CreditNoteDate   string           `json:"credit_note_date"`
		TotalMinor       int64            `json:"total_minor"`
		Refunds          map[string]int64 `json:"refunds"`
	} `json:"credit_notes"`
	MoreCreditNotes        bool  `json:"more_credit_notes"`
	CreditedMinor          int64 `json:"credited_minor"`
	NetMinor               int64 `json:"net_minor"`
	CollectedMinor         int64 `json:"collected_minor"`
	RefundedMinor          int64 `json:"refunded_minor"`
	AdmissionBilledMinor   int64 `json:"admission_billed_minor"`
	AdmissionReversedMinor int64 `json:"admission_reversed_minor"`
}

// workWorld posts, on 1 November, a walk-in cash sale with a delivery
// charge, a registered patient's sale, and an admission sale; on 2 November
// it credits the charge and a unit of the walk-in sale, two units of the
// patient's, and one of the admission's.
type workWorld struct {
	saleWorld
	walkIn, patient, admitted string
	walkInSale, patientSale   saleResp
}

func newWorkWorld(t *testing.T) workWorld {
	t.Helper()
	w := workWorld{saleWorld: newSaleWorld(t, true)}
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	delivery := uuid.Must(uuid.NewV7()).String()
	w.declareCharge(t, delivery, ownCharge("DELIVERY", "counter", 5000, "2026-11-01"), "approved")
	code, charged, out := w.postCharged(t, strings.Replace(w.chargedCashSale(t, p, 2, 7500, delivery), `"tenders":[`,
		`"buyer":{"name":"TEST Walk In","phone":"9800000001"},"tenders":[`, 1))
	require.Equal(t, http.StatusCreated, code, out)
	w.walkIn = charged.ID

	d := newWard(t, w.saleWorld)
	d.verify()
	registered := uuid.NewString()
	require.NoError(t, d.handle("pharmacy-patient-registered", patientmastercontract.PatientRegisteredData{PatientID: registered, HospitalID: d.hospital}))
	w.patient = uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", salePath("/"+w.patient), "pharmacist", fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":5}],"tenders":[{"kind":"upi","amount_minor":6250,"reference":"TEST-UPI-9"}],"patient":{"patient_id":%q,"hospital_id":%q}}`,
		p, registered, d.hospital))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &w.patientSale))

	stay := d.admit("R-1", "B-7")
	w.admitted = uuid.Must(uuid.NewV7()).String()
	res = do(w.r, "PUT", salePath("/"+w.admitted), "pharmacist", admittedSale(p, stay.occupancy))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())

	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 2, 6, 30, 0, 0, time.UTC) })
	res = do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "pharmacist",
		chargeReturn(w.walkIn, []int{1}, fmt.Sprintf(`{"sale_line":1,"quantity_units":1,"quarantine_location_id":%q}`, w.quarantine), 6250, "cash"))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	code, _, out = w.returnUnits(t, returnBody(w.patient, 2, w.quarantine, 2500))
	require.Equal(t, http.StatusCreated, code, out)
	code, _, out = w.returnUnits(t, strings.Replace(returnBody(w.admitted, 1, w.quarantine, 1250), `{"kind":"cash","amount_minor":1250}`, `{"kind":"admission_account","amount_minor":1250}`, 1))
	require.Equal(t, http.StatusCreated, code, out)
	return w
}

const worklistPath = "/sales/worklist?from=2026-11-01&to=2026-11-02"

func TestTheSalesWorklistShowsEachSaleAndWhatRemains(t *testing.T) {
	w := newWorkWorld(t)
	code, body, out := reportPage[workRow](t, w.saleWorld, "pharmacist", worklistPath)
	require.Equal(t, http.StatusOK, code, out)
	for _, personal := range []string{"buyer_name", "buyer_phone", "patient_id", "TEST Walk In", "9800000001"} {
		require.NotContains(t, out, personal, "the worklist carries no personal data")
	}
	require.Len(t, body.Data, 3, out)
	admitted, patient, walkIn := body.Data[0], body.Data[1], body.Data[2]
	require.Equal(t, []string{w.admitted, w.patient, w.walkIn}, []string{admitted.ID, patient.ID, walkIn.ID}, "newest first")

	require.Equal(t, "walk_in", walkIn.Customer)
	require.Nil(t, walkIn.BedCode)
	require.EqualValues(t, 7500, walkIn.TotalMinor)
	require.EqualValues(t, 2, walkIn.Units)
	require.EqualValues(t, 6250, walkIn.CreditedMinor, "the charge and one unit")
	require.EqualValues(t, 1250, walkIn.NetMinor)
	require.Zero(t, walkIn.AdmissionDueMinor)

	require.Equal(t, "patient", patient.Customer)
	require.Equal(t, w.patientSale.InvoiceNumber, patient.InvoiceNumber)
	require.EqualValues(t, 6250, patient.TotalMinor)
	require.EqualValues(t, 2500, patient.CreditedMinor)
	require.EqualValues(t, 3750, patient.NetMinor)
	require.Zero(t, patient.AdmissionDueMinor, "paid by UPI, nothing on an admission")

	require.Equal(t, "admission", admitted.Customer)
	require.Equal(t, "B-7", *admitted.BedCode)
	require.EqualValues(t, 2500, admitted.TotalMinor)
	require.EqualValues(t, 1250, admitted.CreditedMinor)
	require.EqualValues(t, 1250, admitted.AdmissionDueMinor, "billed less reversed")
	require.Equal(t, "2026-11-01", admitted.SaleDate)

	// Credits are those posted so far, whatever the period.
	_, day, out := reportPage[workRow](t, w.saleWorld, "pharmacist", "/sales/worklist?from=2026-11-01&to=2026-11-01")
	require.Len(t, day.Data, 3, out)
	require.EqualValues(t, 6250, day.Data[2].CreditedMinor)
	_, later, out := reportPage[workRow](t, w.saleWorld, "pharmacist", "/sales/worklist?from=2026-11-02&to=2026-11-02")
	require.Empty(t, later.Data, out, "a sale counts on its own date")
	_, earlier, out := reportPage[workRow](t, w.saleWorld, "pharmacist", "/sales/worklist?from=2026-10-01&to=2026-10-31")
	require.Empty(t, earlier.Data, out, "nor after the period")

	for kind, id := range map[string]string{"walk_in": w.walkIn, "patient": w.patient, "admission": w.admitted} {
		_, one, out := reportPage[workRow](t, w.saleWorld, "pharmacist", worklistPath+"&customer="+kind)
		require.Len(t, one.Data, 1, kind+out)
		require.Equal(t, id, one.Data[0].ID, kind)
	}
	_, found, out := reportPage[workRow](t, w.saleWorld, "pharmacist", "/sales/worklist?invoice="+url.QueryEscape(w.patientSale.InvoiceNumber))
	require.Len(t, found.Data, 1, out)
	require.Equal(t, w.patient, found.Data[0].ID, "an invoice is found without a period")
	_, none, out := reportPage[workRow](t, w.saleWorld, "pharmacist", "/sales/worklist?invoice=NOPE%2F2627%2F99")
	require.Empty(t, none.Data, out)

	seen := map[string]bool{}
	path := worklistPath + "&limit=1"
	for {
		_, pg, out := reportPage[workRow](t, w.saleWorld, "pharmacist", path)
		require.Len(t, pg.Data, 1, out)
		require.False(t, seen[pg.Data[0].ID])
		seen[pg.Data[0].ID] = true
		if pg.Page.NextCursor == nil {
			break
		}
		path = worklistPath + "&limit=1&cursor=" + url.QueryEscape(*pg.Page.NextCursor)
	}
	require.Len(t, seen, 3)
}

func TestTheSalesWorklistIsBoundedAndScoped(t *testing.T) {
	w := newWorkWorld(t)
	for _, bad := range []string{"/sales/worklist", worklistPath + "&customer=vip", "/sales/worklist?invoice=ABCD%2F2627%2F123456789", "/sales/worklist?from=2025-10-31&to=2026-11-01", "/sales/worklist?from=2026-11-02&to=2026-11-01"} {
		code, _, out := reportPage[workRow](t, w.saleWorld, "pharmacist", bad)
		require.Equal(t, http.StatusBadRequest, code, bad+out)
	}
	code, _, _ := reportPage[workRow](t, w.saleWorld, "stocker", worklistPath)
	require.Equal(t, http.StatusForbidden, code)
	code, _, _ = reportPage[workRow](t, w.saleWorld, "foreign", worklistPath)
	require.Equal(t, http.StatusNotFound, code)
	code, body, out := reportPage[workRow](t, w.saleWorld, "owner", worklistPath)
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, body.Data, 3)
}

func saleSummary(t *testing.T, w saleWorld, who, id string) (int, summaryResp, string) {
	t.Helper()
	res := do(w.r, "GET", salePath("/"+id+"/summary"), who, "")
	var s summaryResp
	if res.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &s))
	}
	return res.Code, s, res.Body.String()
}

func TestASaleSummaryReconciles(t *testing.T) {
	w := newWorkWorld(t)
	for _, id := range []string{w.walkIn, w.patient, w.admitted} {
		code, s, out := saleSummary(t, w.saleWorld, "pharmacist", id)
		require.Equal(t, http.StatusOK, code, out)
		for _, personal := range []string{"buyer_name", "buyer_phone", "patient_id", "TEST Walk In", "9800000001", "TEST Ravi Kumar"} {
			require.NotContains(t, out, personal, "the summary carries no personal data")
		}
		var lines, charges, lineCredits, chargeCredits, paid, notes, refunds int64
		for _, l := range s.Lines {
			lines += l.AmountMinor
			lineCredits += l.CreditedMinor
		}
		for _, c := range s.Charges {
			charges += c.AmountMinor
			chargeCredits += c.CreditedMinor
		}
		for _, v := range s.Payments {
			paid += v
		}
		for _, n := range s.CreditNotes {
			notes += n.TotalMinor
			for _, v := range n.Refunds {
				refunds += v
			}
		}
		require.Equal(t, s.TotalMinor, lines+charges, "lines and charges make the total")
		require.Equal(t, s.TotalMinor, paid, "payments settle the total")
		require.Equal(t, s.CreditedMinor, notes)
		require.Equal(t, s.CreditedMinor, lineCredits+chargeCredits, "the notes credit these lines and charges")
		require.Equal(t, s.CreditedMinor, refunds, "every credit is refunded")
		require.Equal(t, s.TotalMinor-s.CreditedMinor, s.NetMinor)
		require.Equal(t, s.NetMinor, s.CollectedMinor-s.RefundedMinor+s.AdmissionBilledMinor-s.AdmissionReversedMinor, "net is what was kept plus what is still on the admission")
	}

	_, walkIn, _ := saleSummary(t, w.saleWorld, "pharmacist", w.walkIn)
	require.Equal(t, "walk_in", walkIn.Customer)
	require.Len(t, walkIn.Lines, 1)
	require.EqualValues(t, 1, walkIn.Lines[0].ReturnedUnits)
	require.Positive(t, walkIn.Lines[0].CreditedMinor)
	require.Len(t, walkIn.Charges, 1)
	require.EqualValues(t, 5000, walkIn.Charges[0].CreditedMinor, "the charge credited in full")
	require.Equal(t, map[string]int64{"cash": 7500}, walkIn.Payments)
	require.Len(t, walkIn.CreditNotes, 1)
	require.Equal(t, "2026-11-02", walkIn.CreditNotes[0].CreditNoteDate)
	require.Equal(t, map[string]int64{"cash": 6250}, walkIn.CreditNotes[0].Refunds)
	require.EqualValues(t, 7500, walkIn.CollectedMinor)
	require.EqualValues(t, 6250, walkIn.RefundedMinor)

	_, patient, _ := saleSummary(t, w.saleWorld, "pharmacist", w.patient)
	require.Equal(t, "patient", patient.Customer)
	require.Equal(t, map[string]int64{"upi": 6250}, patient.Payments)
	require.EqualValues(t, 2, patient.Lines[0].ReturnedUnits)
	require.Empty(t, patient.Charges)

	_, admitted, _ := saleSummary(t, w.saleWorld, "pharmacist", w.admitted)
	require.Equal(t, "admission", admitted.Customer)
	require.Zero(t, admitted.CollectedMinor, "nothing collected at the counter")
	require.Zero(t, admitted.RefundedMinor)
	require.EqualValues(t, 2500, admitted.AdmissionBilledMinor)
	require.EqualValues(t, 1250, admitted.AdmissionReversedMinor)

	code, _, _ := saleSummary(t, w.saleWorld, "pharmacist", uuid.Must(uuid.NewV7()).String())
	require.Equal(t, http.StatusNotFound, code)
	code, _, _ = saleSummary(t, w.saleWorld, "pharmacist", "nope")
	require.Equal(t, http.StatusNotFound, code, "a malformed id is not found, as for the invoice")
	code, _, _ = saleSummary(t, w.saleWorld, "foreign", w.walkIn)
	require.Equal(t, http.StatusNotFound, code)
	code, _, _ = saleSummary(t, w.saleWorld, "stocker", w.walkIn)
	require.Equal(t, http.StatusForbidden, code)
}

func TestASaleSummaryListsBoundedCreditNotesButCountsThemAll(t *testing.T) {
	w := newWorkWorld(t)
	// The patient's sale had two units back on one note; two more notes.
	for range 2 {
		code, _, out := w.returnUnits(t, returnBody(w.patient, 1, w.quarantine, 1250))
		require.Equal(t, http.StatusCreated, code, out)
	}
	pharmacy.SetSummaryCreditNotes(t, 2)
	code, s, out := saleSummary(t, w.saleWorld, "pharmacist", w.patient)
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, s.CreditNotes, 2, "the list is bounded")
	require.True(t, s.MoreCreditNotes, "and says there are more")
	require.EqualValues(t, 5000, s.CreditedMinor, "every note is counted")
	require.EqualValues(t, 5000, s.RefundedMinor)
	require.EqualValues(t, 4, s.Lines[0].ReturnedUnits)
	require.Equal(t, s.NetMinor, s.CollectedMinor-s.RefundedMinor)

	pharmacy.SetSummaryCreditNotes(t, 3)
	_, s, _ = saleSummary(t, w.saleWorld, "pharmacist", w.patient)
	require.Len(t, s.CreditNotes, 3)
	require.False(t, s.MoreCreditNotes)
}

func TestASaleSummaryIsOneReading(t *testing.T) {
	w := newWorkWorld(t)
	// A return posted while the summary is being read must not show in part.
	once := false
	pharmacy.SetSummaryMidRead(t, func() {
		if once {
			return
		}
		once = true
		code, _, out := w.returnUnits(t, returnBody(w.patient, 1, w.quarantine, 1250))
		require.Equal(t, http.StatusCreated, code, out)
	})
	code, s, out := saleSummary(t, w.saleWorld, "pharmacist", w.patient)
	require.Equal(t, http.StatusOK, code, out)
	require.True(t, once)
	var lineCredits int64
	for _, l := range s.Lines {
		lineCredits += l.CreditedMinor
	}
	require.Equal(t, s.CreditedMinor, lineCredits, "the lines and the totals are one reading")
	require.Equal(t, s.NetMinor, s.CollectedMinor-s.RefundedMinor)
	require.EqualValues(t, 2500, s.CreditedMinor, "as it stood when the read began")
	_, after, _ := saleSummary(t, w.saleWorld, "pharmacist", w.patient)
	require.EqualValues(t, 3750, after.CreditedMinor, "the next read sees the return")
}
