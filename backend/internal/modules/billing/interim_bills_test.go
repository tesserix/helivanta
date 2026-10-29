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

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type billBody struct {
	Number          int    `json:"number"`
	IssuedBy        string `json:"issued_by"`
	UnpricedBedDays int    `json:"unpriced_bed_days"`
	LineCount       int    `json:"line_count"`
	NewCount        int    `json:"new_count"`
	Totals          []struct {
		Currency         string `json:"currency"`
		ChargedMinor     int64  `json:"charged_minor"`
		ReversedMinor    int64  `json:"reversed_minor"`
		OutstandingMinor int64  `json:"outstanding_minor"`
	} `json:"totals"`
	Sources []struct {
		SourceModule     string `json:"source_module"`
		OutstandingMinor int64  `json:"outstanding_minor"`
	} `json:"sources"`
}

func (w *bedWorld) issue(admission string) billBody {
	res := do(w.r, "POST", w.root+"/admissions/"+admission+"/interim-bills", "clerk", "")
	require.Equal(w.t, http.StatusCreated, res.Code, res.Body.String())
	var b billBody
	require.NoError(w.t, json.Unmarshal(res.Body.Bytes(), &b))
	return b
}

func (w *bedWorld) bill(admission string, number string) billBody {
	res := do(w.r, "GET", w.root+"/admissions/"+admission+"/interim-bills/"+number, "nurse", "")
	require.Equal(w.t, http.StatusOK, res.Code, res.Body.String())
	var b billBody
	require.NoError(w.t, json.Unmarshal(res.Body.Bytes(), &b))
	return b
}

func (w *bedWorld) charge(admission, patient string, amount int64) pharmacycontract.AdmissionChargePostedData {
	c := pharmacycontract.AdmissionChargePostedData{SaleID: uuid.NewString(), InvoiceNumber: "TS/2627/9", AdmissionID: admission, HospitalID: w.hospital, PatientID: patient,
		OutletID: uuid.NewString(), AmountMinor: amount, Currency: "INR", PostedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	require.NoError(w.t, w.deliver("billing-pharmacy-admission-charge", c))
	return c
}

func TestInterimBillsFreezeTheAccountAndNumberInOrder(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	bills := w.root + "/admissions/" + admission + "/interim-bills"
	require.Equal(t, http.StatusNotFound, do(w.r, "POST", bills, "clerk", "").Code, "no account yet")

	require.NoError(t, w.start(admission, patient, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-time.Hour)))
	res := do(w.r, "POST", bills, "clerk", "")
	require.Equal(t, http.StatusConflict, res.Code)
	require.Contains(t, res.Body.String(), "nothing_new", "an account with nothing on it has nothing to bill")

	sale := w.charge(admission, patient, 2500)
	require.Equal(t, http.StatusForbidden, do(w.r, "POST", bills, "nurse", "").Code, "reading the account is not issuing a bill")
	require.Equal(t, http.StatusNotFound, do(w.r, "POST", bills, "outsider", "").Code, "no grant at the hospital")
	first := w.issue(admission)
	require.Equal(t, 1, first.Number)
	require.Equal(t, "user-clerk", first.IssuedBy)
	require.Equal(t, 1, first.LineCount)
	require.Equal(t, 1, first.NewCount)
	require.Len(t, first.Totals, 1)
	require.EqualValues(t, 2500, first.Totals[0].OutstandingMinor)
	require.Len(t, first.Sources, 1)
	require.Equal(t, "pharmacy", first.Sources[0].SourceModule)

	res = do(w.r, "POST", bills, "clerk", "")
	require.Equal(t, http.StatusConflict, res.Code)
	require.Contains(t, res.Body.String(), "nothing_new", "nothing since bill 1")

	require.NoError(t, w.deliver("billing-pharmacy-admission-reversal", pharmacycontract.AdmissionChargeReversedData{CreditNoteID: uuid.NewString(), CreditNoteNumber: "TS/2627/C9",
		SaleID: sale.SaleID, AdmissionID: admission, HospitalID: w.hospital, PatientID: patient, AmountMinor: 1000, Currency: "INR", ReversedAt: time.Now().UTC().Format(time.RFC3339Nano)}))
	second := w.issue(admission)
	require.Equal(t, 2, second.Number)
	require.Equal(t, 2, second.LineCount, "a bill is cumulative")
	require.Equal(t, 1, second.NewCount)
	require.EqualValues(t, 1500, second.Totals[0].OutstandingMinor)
	require.EqualValues(t, 1000, second.Totals[0].ReversedMinor)

	again := w.bill(admission, "1")
	require.Equal(t, 1, again.LineCount, "bill 1 is frozen as issued")
	require.EqualValues(t, 2500, again.Totals[0].OutstandingMinor)

	res = do(w.r, "GET", bills+"/2/lines", "nurse", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var lines struct {
		Items []struct {
			Kind string `json:"kind"`
			New  bool   `json:"new"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &lines))
	require.Len(t, lines.Items, 2, res.Body.String())
	require.Equal(t, "reversal", lines.Items[0].Kind, res.Body.String())
	require.True(t, lines.Items[0].New)
	require.Equal(t, "charge", lines.Items[1].Kind)
	require.False(t, lines.Items[1].New, "the charge was on bill 1")

	res = do(w.r, "GET", bills, "nurse", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var list struct {
		Items []billBody `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &list))
	require.Len(t, list.Items, 2)
	require.Equal(t, []int{2, 1}, []int{list.Items[0].Number, list.Items[1].Number})
	require.Equal(t, 2, list.Items[0].LineCount)

	for _, path := range []string{bills, bills + "/1", bills + "/1/lines"} {
		require.Equal(t, http.StatusNotFound, do(w.r, "GET", path, "outsider", "").Code, path)
	}
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", bills+"/3", "nurse", "").Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", bills+"/3/lines", "nurse", "").Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", bills+"/0", "nurse", "").Code)

	w.charge(admission, patient, 700)
	require.NoError(t, w.release(admission, patient, time.Now()))
	res = do(w.r, "POST", bills, "clerk", "")
	require.Equal(t, http.StatusConflict, res.Code)
	require.Contains(t, res.Body.String(), "account_closed")
}

func TestAnInterimBillCountsBedDaysWithoutARate(t *testing.T) {
	w := newBedWorld(t)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Asia/Kolkata"}`).Code)
	w.census()
	admission, patient := uuid.NewString(), uuid.NewString()
	require.NoError(t, w.start(admission, patient, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-50*time.Hour)))
	a, _ := w.account(admission)
	require.Positive(t, a.BedDays.NotPriced)

	first := w.issue(admission)
	require.EqualValues(t, a.BedDays.NotPriced, first.UnpricedBedDays, "a day without a rate is counted, not charged as zero")
	require.Equal(t, 0, first.LineCount)
	require.Empty(t, first.Totals)
	require.Equal(t, http.StatusConflict, do(w.r, "POST", w.root+"/admissions/"+admission+"/interim-bills", "clerk", "").Code)

	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-rates/general/2020-01-01", "admin", `{"daily_rate_minor":150000,"currency":"INR"}`).Code)
	w.census()
	second := w.issue(admission)
	require.Equal(t, 0, second.UnpricedBedDays)
	require.EqualValues(t, a.BedDays.NotPriced, second.LineCount, "each priced day is a line")
	require.Equal(t, "bed", second.Sources[0].SourceModule)
}

func TestConcurrentIssuesNumberOneAfterTheOther(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 2500)
	codes := make([]int, 6)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = do(w.r, "POST", w.root+"/admissions/"+admission+"/interim-bills", "clerk", "").Code
		}()
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		require.Contains(t, []int{http.StatusCreated, http.StatusConflict}, c)
		if c == http.StatusCreated {
			created++
		}
	}
	require.Equal(t, 1, created, "one bill; the rest find nothing new")
}

func TestInterimBillTablesRefuseLateLinesAndOtherWriters(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 2500)
	w.issue(admission)
	w.charge(admission, patient, 400)

	asClerk := func(fn func(tx *gorm.DB) error) error {
		return w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-clerk"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.billing_subject','user-clerk',true)`).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}
	var visible int64
	require.NoError(t, asClerk(func(tx *gorm.DB) error {
		return tx.Table("billing_interim_bills").Where("admission_id=?", admission).Count(&visible).Error
	}))
	require.EqualValues(t, 1, visible, "the clerk with the grant reads the bill")

	require.Error(t, asClerk(func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_interim_bill_lines (tenant_id,hospital_id,admission_id,number,id,new)
 SELECT tenant_id,hospital_id,admission_id,1,id,true FROM billing_account_entries WHERE admission_id=? AND amount_minor=400`, admission).Error
	}), "an issued bill never gains a line")
	require.Error(t, asClerk(func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_interim_bills SET unpriced_bed_days=9 WHERE admission_id=?`, admission).Error
	}))
	require.Error(t, asClerk(func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM billing_interim_bill_lines WHERE admission_id=?`, admission).Error
	}))
	require.Error(t, asClerk(func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_interim_bills (tenant_id,hospital_id,admission_id,number,id,issued_by,unpriced_bed_days) VALUES (?,?,?,3,?,'user-clerk',0)`,
			testutil.TenantA, w.hospital, admission, uuid.Must(uuid.NewV7())).Error
	}), "numbers have no gaps")
	require.Error(t, asClerk(func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_interim_bills (tenant_id,hospital_id,admission_id,number,id,issued_by,unpriced_bed_days) VALUES (?,?,?,2,?,'user-nurse',0)`,
			testutil.TenantA, w.hospital, admission, uuid.Must(uuid.NewV7())).Error
	}), "the issuer is the acting subject")
	require.Error(t, asClerk(func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_interim_bills (tenant_id,hospital_id,admission_id,number,id,issued_by,issued_at,unpriced_bed_days) VALUES (?,?,?,2,?,'user-clerk',now()-interval '1 day',0)`,
			testutil.TenantA, w.hospital, admission, uuid.Must(uuid.NewV7())).Error
	}), "a bill is issued now, never backdated")

	require.NoError(t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-outsider"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.billing_subject','user-outsider',true)`).Error; err != nil {
			return err
		}
		return tx.Table("billing_interim_bills").Where("admission_id=?", admission).Count(&visible).Error
	}))
	require.Zero(t, visible, "no grant, no bills")
}
