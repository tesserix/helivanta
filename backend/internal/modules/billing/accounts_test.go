package billing_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/billing" //nolint:depguard // module under test
	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func do(r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAdmissionAccountsTakePharmacyChargesOnce(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "nurse": testutil.TenantA, "outsider": testutil.TenantA, "nurseB": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"admin":    {billing.PermAccessManage, billing.PermAccountRead},
			"nurse":    {billing.PermAccountRead},
			"outsider": {billing.PermAccountRead},
			"nurseB":   {billing.PermAccountRead, billing.PermAccessManage},
		},
		Modules: []platform.Module{billing.New()},
	})
	hospital, admission, patient := uuid.NewString(), uuid.NewString(), uuid.NewString()
	root := "/v1/billing/hospitals/" + hospital
	require.Equal(t, http.StatusForbidden, do(r, "PUT", root+"/access/user-nurse", "nurse", "").Code)
	require.Equal(t, http.StatusOK, do(r, "PUT", root+"/access/user-nurse", "admin", "").Code)

	consumers := map[string]events.Consumer{}
	for _, c := range billing.New().Consumers(platform.Deps{}) {
		consumers[c.Name] = c
	}
	types := map[string]string{"billing-pharmacy-admission-charge": "AdmissionChargePosted", "billing-pharmacy-admission-reversal": "AdmissionChargeReversed"}
	deliver := func(name string, v any) error {
		data, err := json.Marshal(v)
		require.NoError(t, err)
		c := consumers[name]
		return db.WithTenant(tenantdb.WithActor(ctx, "system:"+name), testutil.TenantA, func(tx *gorm.DB) error {
			return c.Handle(ctx, tx, events.Event{Type: types[name], Version: 1, TenantID: testutil.TenantA, Data: data})
		})
	}
	now := func() string { return time.Now().UTC().Format(time.RFC3339Nano) }
	sale, credit := uuid.NewString(), uuid.NewString()
	charge := pharmacycontract.AdmissionChargePostedData{SaleID: sale, InvoiceNumber: "TS/2627/7", AdmissionID: admission, HospitalID: hospital, PatientID: patient,
		OutletID: uuid.NewString(), AmountMinor: 2500, Currency: "INR", PostedAt: now()}
	reversal := pharmacycontract.AdmissionChargeReversedData{CreditNoteID: credit, CreditNoteNumber: "TS/2627/C1", SaleID: sale, AdmissionID: admission, HospitalID: hospital,
		PatientID: patient, AmountMinor: 1000, Currency: "INR", ReversedAt: now()}

	// A reversal that overtakes its charge is parked, and applied with it.
	require.NoError(t, deliver("billing-pharmacy-admission-reversal", reversal))
	require.NoError(t, deliver("billing-pharmacy-admission-reversal", reversal), "a replay of a parked reversal is idempotent")
	// A charge before the admission's first stay still opens its account.
	require.NoError(t, deliver("billing-pharmacy-admission-charge", charge))
	require.NoError(t, deliver("billing-pharmacy-admission-charge", charge), "a replay is idempotent")
	changed := charge
	changed.AmountMinor = 9999
	require.Error(t, deliver("billing-pharmacy-admission-charge", changed), "a replay that differs is refused")
	var applied struct{ Reversed int64 }
	require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "system:billing-pharmacy-admission-charge"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT COALESCE(sum(amount_minor),0) AS reversed FROM billing_account_entries WHERE admission_id=? AND kind='reversal'`, admission).Scan(&applied).Error
	}))
	require.EqualValues(t, 1000, applied.Reversed, "the parked reversal is applied when its charge arrives")
	require.NoError(t, deliver("billing-bed-occupancy-started", medicorecontract.BedOccupancyStartedData{OccupancyID: uuid.NewString(), AdmissionID: admission, PatientID: patient,
		HospitalID: hospital, WardID: uuid.NewString(), RoomID: uuid.NewString(), BedID: uuid.NewString(), WardCode: "W-A", RoomCode: "R-1", BedCode: "A-1", BedClass: "general", StartedAt: now()}))
	require.NoError(t, deliver("billing-pharmacy-admission-reversal", reversal), "a replay after the parked reversal was applied is idempotent")
	over := reversal
	over.CreditNoteID, over.AmountMinor = uuid.NewString(), 1600
	require.Error(t, deliver("billing-pharmacy-admission-reversal", over), "reversals never exceed the charge")
	other := charge
	other.PatientID = uuid.NewString()
	other.SaleID = uuid.NewString()
	require.Error(t, deliver("billing-pharmacy-admission-charge", other), "an admission keeps its patient")

	account := do(r, "GET", root+"/admissions/"+admission+"/account", "nurse", "")
	require.Equal(t, http.StatusOK, account.Code, account.Body.String())
	var view struct {
		Status     string `json:"status"`
		EntryCount int64  `json:"entry_count"`
		OpenedAt   *string
		Totals     []struct {
			Currency         string `json:"currency"`
			ChargedMinor     int64  `json:"charged_minor"`
			ReversedMinor    int64  `json:"reversed_minor"`
			OutstandingMinor int64  `json:"outstanding_minor"`
		} `json:"totals"`
	}
	require.NoError(t, json.Unmarshal(account.Body.Bytes(), &view))
	require.Equal(t, "open", view.Status)
	require.EqualValues(t, 2, view.EntryCount)
	require.Len(t, view.Totals, 1)
	require.EqualValues(t, 2500, view.Totals[0].ChargedMinor)
	require.EqualValues(t, 1000, view.Totals[0].ReversedMinor)
	require.EqualValues(t, 1500, view.Totals[0].OutstandingMinor)

	entries := do(r, "GET", root+"/admissions/"+admission+"/entries", "nurse", "")
	require.Equal(t, http.StatusOK, entries.Code, entries.Body.String())
	require.Contains(t, entries.Body.String(), `"source_document":"TS/2627/7"`)
	require.Contains(t, entries.Body.String(), `"source_document":"TS/2627/C1"`)

	// Release closes the account; a late charge is still recorded.
	require.NoError(t, deliver("billing-admission-released", medicorecontract.AdmissionReleasedData{AdmissionID: admission, PatientID: patient, HospitalID: hospital, ReleasedAt: now(), Reason: "discharged"}))
	late := charge
	late.SaleID, late.InvoiceNumber, late.AmountMinor = uuid.NewString(), "TS/2627/8", 400
	require.NoError(t, deliver("billing-pharmacy-admission-charge", late))
	account = do(r, "GET", root+"/admissions/"+admission+"/account", "nurse", "")
	require.Contains(t, account.Body.String(), `"status":"closed"`)
	require.Contains(t, account.Body.String(), `"outstanding_minor":1900`)

	// Authority: no grant is a 404, as is another tenant.
	require.Equal(t, http.StatusNotFound, do(r, "GET", root+"/admissions/"+admission+"/account", "outsider", "").Code)
	require.Equal(t, http.StatusNotFound, do(r, "GET", root+"/admissions/"+admission+"/entries", "outsider", "").Code)
	require.Equal(t, http.StatusOK, do(r, "PUT", root+"/access/user-nurseB", "nurseB", "").Code)
	require.Equal(t, http.StatusNotFound, do(r, "GET", root+"/admissions/"+admission+"/account", "nurseB", "").Code, "another tenant sees nothing")
	require.Equal(t, http.StatusNotFound, do(r, "GET", root+"/admissions/"+uuid.NewString()+"/account", "nurse", "").Code)

	// The database holds the history.
	asReader := func(fn func(tx *gorm.DB) error) error {
		return db.WithTenant(tenantdb.WithActor(ctx, "user-nurse"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.billing_subject','user-nurse',true)`).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}
	fresh := uuid.NewString()
	require.Error(t, asReader(func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_account_entries (tenant_id,hospital_id,id,admission_id,kind,source_module,source_reference,source_document,charge_reference,amount_minor,currency,occurred_at) VALUES (?,?,?,?,'charge','pharmacy',?,'X',?,1,'INR',now())`,
			testutil.TenantA, hospital, uuid.Must(uuid.NewV7()), admission, fresh, fresh).Error
	}), "only billing's consumers write entries")
	require.Error(t, db.WithTenant(tenantdb.WithActor(ctx, "system:billing-pharmacy-admission-charge"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_account_entries SET amount_minor=1`).Error
	}), "entries are never changed")
	require.Error(t, db.WithTenant(tenantdb.WithActor(ctx, "system:billing-admission-released"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_admission_accounts SET status='open',closed_at=NULL WHERE admission_id=?`, admission).Error
	}), "an account closes once")

	// A replayed reversal larger than half its charge is still a no-op.
	big := reversal
	big.CreditNoteID, big.AmountMinor = uuid.NewString(), 1500
	require.NoError(t, deliver("billing-pharmacy-admission-reversal", big))
	require.NoError(t, deliver("billing-pharmacy-admission-reversal", big), "a replay never counts itself against the charge")
	account = do(r, "GET", root+"/admissions/"+admission+"/account", "nurse", "")
	require.Contains(t, account.Body.String(), `"reversed_minor":2500`)
}

func TestConcurrentReversalsNeverExceedTheirCharge(t *testing.T) {
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{billing.New()}})
	consumers := map[string]events.Consumer{}
	for _, c := range billing.New().Consumers(platform.Deps{}) {
		consumers[c.Name] = c
	}
	handle := func(tx *gorm.DB, name, typ string, v any) error {
		data, err := json.Marshal(v)
		require.NoError(t, err)
		c := consumers[name]
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, "system:"+name).Error; err != nil {
			return err
		}
		return c.Handle(ctx, tx, events.Event{Type: typ, Version: 1, TenantID: testutil.TenantA, Data: data})
	}
	in := func(fn func(tx *gorm.DB) error) error { return db.WithTenant(ctx, testutil.TenantA, fn) }
	hospital, admission, patient, sale := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	require.NoError(t, in(func(tx *gorm.DB) error {
		return handle(tx, "billing-pharmacy-admission-charge", "AdmissionChargePosted", pharmacycontract.AdmissionChargePostedData{SaleID: sale, InvoiceNumber: "TS/2627/9",
			AdmissionID: admission, HospitalID: hospital, PatientID: patient, OutletID: uuid.NewString(), AmountMinor: 2500, Currency: "INR", PostedAt: now})
	}))
	// Raw inserts, bypassing the consumer's own lock: the database alone
	// must hold the bound.
	reverse := func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','system:billing-pharmacy-admission-reversal',true)`).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO billing_account_entries (tenant_id,hospital_id,id,admission_id,kind,source_module,source_reference,source_document,charge_reference,amount_minor,currency,occurred_at)
 VALUES (?,?,?,?,'reversal','pharmacy',?,'TS/2627/C9',?,1500,'INR',now())`, testutil.TenantA, hospital, uuid.Must(uuid.NewV7()), admission, uuid.New(), sale).Error
	}
	held, let, first := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		first <- in(func(tx *gorm.DB) error {
			if err := reverse(tx); err != nil {
				return err
			}
			close(held)
			<-let
			return nil
		})
	}()
	<-held
	second := make(chan error, 1)
	go func() { second <- in(reverse) }()
	time.Sleep(300 * time.Millisecond)
	close(let)
	require.NoError(t, <-first)
	require.ErrorContains(t, <-second, "reversals cannot exceed the charge", "the second waits for the first and then sees it")
}

func TestReleaseBeforeTheFirstStayStillClosesTheAccount(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {billing.PermAccessManage, billing.PermAccountRead}},
		Modules: []platform.Module{billing.New()},
	})
	hospital, admission, patient := uuid.NewString(), uuid.NewString(), uuid.NewString()
	require.Equal(t, http.StatusOK, do(r, "PUT", "/v1/billing/hospitals/"+hospital+"/access/user-admin", "admin", "").Code)
	consumers := map[string]events.Consumer{}
	for _, c := range billing.New().Consumers(platform.Deps{}) {
		consumers[c.Name] = c
	}
	deliver := func(name string, v any) {
		data, err := json.Marshal(v)
		require.NoError(t, err)
		require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "system:"+name), testutil.TenantA, func(tx *gorm.DB) error {
			return consumers[name].Handle(ctx, tx, events.Event{Version: 1, TenantID: testutil.TenantA, Data: data})
		}))
	}
	at := time.Now().UTC()
	deliver("billing-admission-released", medicorecontract.AdmissionReleasedData{AdmissionID: admission, PatientID: patient, HospitalID: hospital, ReleasedAt: at.Format(time.RFC3339Nano), Reason: "discharged"})
	deliver("billing-bed-occupancy-started", medicorecontract.BedOccupancyStartedData{OccupancyID: uuid.NewString(), AdmissionID: admission, PatientID: patient, HospitalID: hospital,
		WardID: uuid.NewString(), RoomID: uuid.NewString(), BedID: uuid.NewString(), WardCode: "W-A", RoomCode: "R-1", BedCode: "A-1", BedClass: "general", StartedAt: at.Add(-time.Hour).Format(time.RFC3339Nano)})
	account := do(r, "GET", "/v1/billing/hospitals/"+hospital+"/admissions/"+admission+"/account", "admin", "")
	require.Equal(t, http.StatusOK, account.Code, account.Body.String())
	require.Contains(t, account.Body.String(), `"status":"closed"`, "a start that arrives after release does not reopen the account")
	require.NotContains(t, account.Body.String(), `"opened_at":null`, "it still records when the stay began")
}
