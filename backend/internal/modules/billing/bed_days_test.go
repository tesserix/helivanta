package billing_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/billing" //nolint:depguard // module under test
	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type bedWorld struct {
	t         *testing.T
	r         *gin.Engine
	db        *tenantdb.DB
	ctx       context.Context
	consumers map[string]events.Consumer
	hospital  string
	root      string
}

var ist = func() *time.Location { l, _ := time.LoadLocation("Asia/Kolkata"); return l }()

func newBedWorld(t *testing.T) *bedWorld {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "nurse": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"admin": {billing.PermAccessManage, billing.PermAccountRead, billing.PermRateManage},
			"nurse": {billing.PermAccountRead},
		},
		Modules: []platform.Module{billing.New()},
	})
	w := &bedWorld{t: t, r: r, db: db, ctx: ctx, consumers: map[string]events.Consumer{}, hospital: uuid.NewString()}
	w.root = "/v1/billing/hospitals/" + w.hospital
	for _, c := range billing.New().Consumers(platform.Deps{}) {
		w.consumers[c.Name] = c
	}
	require.Equal(t, http.StatusOK, do(r, "PUT", w.root+"/access/user-admin", "admin", "").Code)
	require.Equal(t, http.StatusOK, do(r, "PUT", w.root+"/access/user-nurse", "admin", "").Code)
	return w
}

var eventTypes = map[string]string{"billing-bed-occupancy-started": "BedOccupancyStarted", "billing-bed-occupancy-ended": "BedOccupancyEnded", "billing-admission-released": "AdmissionReleased"}

func (w *bedWorld) deliver(name string, v any) error {
	data, err := json.Marshal(v)
	require.NoError(w.t, err)
	c := w.consumers[name]
	return w.db.WithTenant(tenantdb.WithActor(w.ctx, "system:"+name), testutil.TenantA, func(tx *gorm.DB) error {
		return c.Handle(w.ctx, tx, events.Event{Type: eventTypes[name], Version: 1, TenantID: testutil.TenantA, Data: data})
	})
}

type stay struct{ occupancy, bed, class string }

func (w *bedWorld) start(admission, patient string, s stay, at time.Time) error {
	return w.deliver("billing-bed-occupancy-started", medicorecontract.BedOccupancyStartedData{OccupancyID: s.occupancy, AdmissionID: admission, PatientID: patient,
		HospitalID: w.hospital, WardID: uuid.NewString(), RoomID: uuid.NewString(), BedID: s.bed, WardCode: "W-A", RoomCode: "R-1", BedCode: "A-1", BedClass: s.class,
		StartedAt: at.UTC().Format(time.RFC3339Nano)})
}

func (w *bedWorld) end(admission string, s stay, at time.Time, how string) error {
	return w.deliver("billing-bed-occupancy-ended", medicorecontract.BedOccupancyEndedData{OccupancyID: s.occupancy, AdmissionID: admission, HospitalID: w.hospital, BedID: s.bed,
		EndedAt: at.UTC().Format(time.RFC3339Nano), End: how})
}

func (w *bedWorld) release(admission, patient string, at time.Time) error {
	return w.deliver("billing-admission-released", medicorecontract.AdmissionReleasedData{AdmissionID: admission, PatientID: patient, HospitalID: w.hospital,
		ReleasedAt: at.UTC().Format(time.RFC3339Nano), Reason: "discharged"})
}

func (w *bedWorld) census() { require.NoError(w.t, billing.BedCensusOnce(w.ctx, w.db)) }

type accountBody struct {
	Status  string `json:"status"`
	Sources []struct {
		SourceModule     string `json:"source_module"`
		Currency         string `json:"currency"`
		OutstandingMinor int64  `json:"outstanding_minor"`
	} `json:"sources"`
	BedDays struct {
		Configured bool  `json:"configured"`
		Priced     int64 `json:"priced"`
		NotPriced  int64 `json:"not_priced"`
	} `json:"bed_days"`
}

func (w *bedWorld) account(admission string) (accountBody, int64) {
	res := do(w.r, "GET", w.root+"/admissions/"+admission+"/account", "nurse", "")
	require.Equal(w.t, http.StatusOK, res.Code, res.Body.String())
	var a accountBody
	require.NoError(w.t, json.Unmarshal(res.Body.Bytes(), &a))
	var bed int64
	for _, s := range a.Sources {
		if s.SourceModule == "bed" {
			bed = s.OutstandingMinor
		}
	}
	return a, bed
}

type dayRow struct {
	CensusDate string
	BedClass   string
	Basis      string
	Rate       *int64
	Voided     bool
}

// days reads every recorded day, voided ones included, as a reader would.
func (w *bedWorld) days(admission string) []dayRow {
	var rows []dayRow
	require.NoError(w.t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "system:billing-bed-census"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT to_char(census_date,'YYYY-MM-DD') AS census_date,bed_class,basis,daily_rate_minor AS rate,voided_at IS NOT NULL AS voided FROM billing_bed_days
 WHERE admission_id=? ORDER BY census_date,recorded_at`, admission).Scan(&rows).Error
	}))
	return rows
}

// liveDays are the days the account holds now.
func (w *bedWorld) liveDays(admission string) []dayRow {
	var out []dayRow
	for _, d := range w.days(admission) {
		if !d.Voided {
			out = append(out, d)
		}
	}
	return out
}

func ptr(v int64) *int64 { return &v }

func TestBedDaysFollowTheMidnightCensusAcrossATransfer(t *testing.T) {
	w := newBedWorld(t)
	// Setup: administrators with the grant set it, once; readers cannot.
	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", w.root+"/bed-charging", "nurse", `{"timezone":"Asia/Kolkata"}`).Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.root+"/bed-charging", "nurse", "").Code)
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", w.root+"/bed-rates/general/2026-01-01", "admin", `{"daily_rate_minor":150000,"currency":"INR"}`).Code, "rates need the timezone first")
	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Mars/Olympus"}`).Code)
	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Local"}`).Code)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Asia/Kolkata"}`).Code)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Asia/Kolkata"}`).Code, "the same again is accepted")
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"UTC"}`).Code, "the timezone never changes")
	require.Contains(t, do(w.r, "GET", w.root+"/bed-charging", "nurse", "").Body.String(), `"timezone":"Asia/Kolkata"`)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-rates/general/2026-01-01", "admin", `{"daily_rate_minor":150000,"currency":"INR"}`).Code)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-rates/icu/2026-01-01", "admin", `{"daily_rate_minor":600000,"currency":"INR"}`).Code)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-rates/icu/2026-01-01", "admin", `{"daily_rate_minor":600000,"currency":"INR"}`).Code, "the same rate again is accepted")
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", w.root+"/bed-rates/icu/2026-01-01", "admin", `{"daily_rate_minor":650000,"currency":"INR"}`).Code, "a rate is fixed once set")
	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", w.root+"/bed-rates/suite/2026-01-01", "admin", `{"daily_rate_minor":1,"currency":"INR"}`).Code, "only a known bed class")
	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", w.root+"/bed-rates/icu/2026-01-01", "admin", `{"daily_rate_minor":0,"currency":"INR"}`).Code, "a rate is never zero")
	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", w.root+"/bed-rates/icu/2026-02-01", "nurse", `{"daily_rate_minor":1,"currency":"INR"}`).Code)
	rates := do(w.r, "GET", w.root+"/bed-rates", "nurse", "")
	require.Equal(t, http.StatusOK, rates.Code)
	require.Contains(t, rates.Body.String(), `"bed_class":"icu","effective_from":"2026-01-01","daily_rate_minor":600000`)
	w.census()

	// Admitted 1 March 10:00 to a general bed, moved to ICU at midnight
	// exactly, released 4 March noon. Midnights: 2 Mar (general), 3 Mar (the
	// new bed, ICU), 4 Mar (ICU) — census dates 1, 2 and 3 March.
	admission, patient := uuid.NewString(), uuid.NewString()
	general := stay{uuid.NewString(), uuid.NewString(), "general"}
	icu := stay{uuid.NewString(), uuid.NewString(), "icu"}
	at := func(day, hour int) time.Time { return time.Date(2026, 3, day, hour, 0, 0, 0, ist) }
	require.NoError(t, w.start(admission, patient, general, at(1, 10)))
	require.NoError(t, w.end(admission, general, at(3, 0), "transfer"))
	// The ICU stay's end arrives before its start: either order is kept.
	require.NoError(t, w.end(admission, icu, at(4, 12), "release"))
	require.NoError(t, w.start(admission, patient, icu, at(3, 0)))
	require.NoError(t, w.release(admission, patient, at(4, 12)))

	require.Equal(t, []dayRow{
		{CensusDate: "2026-03-01", BedClass: "general", Basis: "midnight", Rate: ptr(150000)},
		{CensusDate: "2026-03-02", BedClass: "icu", Basis: "midnight", Rate: ptr(600000)},
		{CensusDate: "2026-03-03", BedClass: "icu", Basis: "midnight", Rate: ptr(600000)},
	}, w.liveDays(admission), "the general stay, open until its end arrived, was counted to today and then voided")
	a, bed := w.account(admission)
	require.EqualValues(t, 1350000, bed, "₹1,500 + 2 × ₹6,000")
	require.True(t, a.BedDays.Configured)
	require.EqualValues(t, 3, a.BedDays.Priced)
	require.Zero(t, a.BedDays.NotPriced)
	require.Contains(t, do(w.r, "GET", w.root+"/admissions/"+admission+"/entries", "nurse", "").Body.String(), `"source_document":"Bed day 2026-03-02"`)

	// Replays change nothing; a replay that differs is refused.
	require.NoError(t, w.start(admission, patient, general, at(1, 10)))
	require.NoError(t, w.end(admission, icu, at(4, 12), "release"))
	require.NoError(t, w.release(admission, patient, at(4, 12)))
	w.census()
	require.Error(t, w.start(admission, patient, stay{general.occupancy, general.bed, "private"}, at(1, 10)), "a replayed start with another class is refused")
	require.Error(t, w.start(admission, patient, stay{uuid.NewString(), uuid.NewString(), ""}, at(1, 10)), "a start without its bed class is refused")
	require.Len(t, w.liveDays(admission), 3)
	_, bed = w.account(admission)
	require.EqualValues(t, 1350000, bed)

	// The database refuses to reprice a day, or to change a stay's start.
	asCensus := func(sql string, args ...any) error {
		return w.db.WithTenant(tenantdb.WithActor(w.ctx, "system:billing-bed-census"), testutil.TenantA, func(tx *gorm.DB) error { return tx.Exec(sql, args...).Error })
	}
	require.Error(t, asCensus(`UPDATE billing_bed_days SET daily_rate_minor=1 WHERE admission_id=?`, admission))
	require.Error(t, asCensus(`UPDATE billing_bed_stays SET started_at=started_at-interval '1 hour' WHERE occupancy_id=?`, general.occupancy))
	require.Error(t, asCensus(`DELETE FROM billing_bed_days WHERE admission_id=?`, admission))
	require.Error(t, asCensus(`UPDATE billing_bed_rates SET daily_rate_minor=1 WHERE hospital_id=?`, w.hospital))
}

func TestABedDayWithoutARateIsNotPricedUntilOneIsSet(t *testing.T) {
	w := newBedWorld(t)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Asia/Kolkata"}`).Code)
	admission, patient := uuid.NewString(), uuid.NewString()
	private := stay{uuid.NewString(), uuid.NewString(), "private"}
	require.NoError(t, w.start(admission, patient, private, time.Date(2026, 3, 1, 10, 0, 0, 0, ist)))
	require.NoError(t, w.end(admission, private, time.Date(2026, 3, 2, 8, 0, 0, 0, ist), "release"))
	require.NoError(t, w.release(admission, patient, time.Date(2026, 3, 2, 8, 0, 0, 0, ist)))
	a, bed := w.account(admission)
	require.EqualValues(t, 1, a.BedDays.NotPriced, "the day is recorded and shown as not priced")
	require.Zero(t, a.BedDays.Priced)
	require.Zero(t, bed, "and nothing is charged for it, rather than zero being charged")
	for _, s := range a.Sources {
		require.NotEqual(t, "bed", s.SourceModule, "no bed entry exists before a rate")
	}

	// A rate set later, from before the day, prices it on the census.
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-rates/private/2026-03-01", "admin", `{"daily_rate_minor":300000,"currency":"INR"}`).Code)
	w.census()
	w.census()
	a, bed = w.account(admission)
	require.EqualValues(t, 1, a.BedDays.Priced)
	require.Zero(t, a.BedDays.NotPriced)
	require.EqualValues(t, 300000, bed)
	// A rate from a later date leaves the priced day as it was.
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-rates/private/2026-03-02", "admin", `{"daily_rate_minor":1,"currency":"INR"}`).Code)
	w.census()
	w.census()
	_, bed = w.account(admission)
	require.EqualValues(t, 300000, bed)
}

// census dates an open stay begun at start has reached by now: each midnight
// at least the grace ago.
func expectedOpenDays(start time.Time) []string {
	var out []string
	cut := time.Now().Add(-15 * time.Minute)
	for d := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, ist); ; d = d.AddDate(0, 0, 1) {
		m := d.AddDate(0, 0, 1)
		if !m.Before(cut) {
			return out
		}
		if !m.Before(start) {
			out = append(out, d.Format(time.DateOnly))
		}
	}
}

func TestTheCensusCatchesUpAndALateEndIsReversedToTheMinimumDay(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	general := stay{uuid.NewString(), uuid.NewString(), "general"}
	today := time.Now().In(ist)
	start := time.Date(today.Year(), today.Month(), today.Day()-3, 9, 0, 0, 0, ist)
	require.NoError(t, w.start(admission, patient, general, start))
	a, _ := w.account(admission)
	require.False(t, a.BedDays.Configured, "bed charging is reported as not set up")
	require.Empty(t, w.days(admission), "no midnight is known without the hospital's timezone")

	// Setting it up later computes every day since admission.
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Asia/Kolkata"}`).Code)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-rates/general/2026-01-01", "admin", `{"daily_rate_minor":150000,"currency":"INR"}`).Code)
	w.census()
	w.census()
	want := expectedOpenDays(start)
	require.NotEmpty(t, want)
	var got []string
	for _, d := range w.days(admission) {
		got = append(got, d.CensusDate)
	}
	require.Equal(t, want, got, "one day per midnight passed while the stay is open")
	_, bed := w.account(admission)
	require.EqualValues(t, int64(len(want))*150000, bed)

	// The stay turns out to have ended the same afternoon: every counted day
	// is voided and reversed, and release charges the minimum day.
	require.NoError(t, w.end(admission, general, start.Add(9*time.Hour), "release"))
	require.NoError(t, w.release(admission, patient, start.Add(9*time.Hour)))
	days := w.days(admission)
	require.Len(t, days, len(want)+1)
	live := 0
	for _, d := range days {
		if !d.Voided {
			live++
			require.Equal(t, dayRow{CensusDate: start.Format(time.DateOnly), BedClass: "general", Basis: "minimum", Rate: ptr(150000)}, d)
		}
	}
	require.Equal(t, 1, live)
	a, bed = w.account(admission)
	require.EqualValues(t, 150000, bed, "the corrections remain as reversals; one day is owed")
	require.EqualValues(t, 1, a.BedDays.Priced)
	var reversals int64
	require.NoError(t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "system:billing-bed-census"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("billing_account_entries").Where("admission_id=? AND source_module='bed' AND kind='reversal'", admission).Count(&reversals).Error
	}))
	require.EqualValues(t, len(want), reversals)
}
