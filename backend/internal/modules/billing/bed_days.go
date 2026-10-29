package billing

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// Bed-day charges (#197 slice 2): reconcileBedDays converges an admission's
// recorded days to what its stays imply. Every path that learns something
// about a stay calls it, and the census calls it as midnights pass.

const (
	censusActor = "system:billing-bed-census"
	// censusGrace lets a discharge just before midnight arrive before that
	// midnight is counted, so it is not charged and then reversed.
	censusGrace = "15 minutes"
	// A stay whose reconcile fails is retried later, doubling to an hour, so
	// it never holds the head of the queue.
	firstCensusRetry = time.Minute
	maxCensusRetry   = time.Hour
)

// Variables so tests can drive small batches and settle setup at once.
var (
	censusBatch = 100
	// censusBudget bounds one pass: every hospital in a timezone reaches
	// midnight together, so a pass takes batch after batch until done or out
	// of time, and the next tick carries on.
	censusBudget = 45 * time.Second
	// censusSettle is how long new setup waits before it is applied, so a
	// consumer transaction that began before it committed has finished and
	// is covered by the stays it marks.
	censusSettle = 2 * time.Minute
)

func censusRetry(failures int) time.Duration {
	d := firstCensusRetry
	for i := 1; i < failures && d < maxCensusRetry; i++ {
		d *= 2
	}
	return min(d, maxCensusRetry)
}

// recordStayStart keeps the stay's start. A replay must say what was recorded.
func recordStayStart(tx *gorm.DB, tenant string, d medicorecontract.BedOccupancyStartedData, v []uuid.UUID, at time.Time) error {
	// v: hospital, admission, patient, occupancy, bed.
	if err := tx.Exec(`INSERT INTO billing_bed_stays (tenant_id,occupancy_id,hospital_id,admission_id,bed_id,bed_class,started_at) VALUES (?,?,?,?,?,?,?)
 ON CONFLICT (tenant_id,occupancy_id) DO UPDATE SET bed_class=EXCLUDED.bed_class,started_at=EXCLUDED.started_at WHERE billing_bed_stays.started_at IS NULL`,
		tenant, v[3], v[0], v[1], v[4], d.BedClass, at).Error; err != nil {
		return err
	}
	var held struct {
		HospitalID, AdmissionID, BedID uuid.UUID
		BedClass                       string
		StartedAt                      time.Time
	}
	if err := tx.Raw(`SELECT hospital_id,admission_id,bed_id,bed_class,started_at FROM billing_bed_stays WHERE tenant_id=? AND occupancy_id=?`, tenant, v[3]).Scan(&held).Error; err != nil {
		return err
	}
	// Compared at the database's microsecond precision.
	if held.HospitalID != v[0] || held.AdmissionID != v[1] || held.BedID != v[4] || held.BedClass != d.BedClass || !held.StartedAt.Equal(at.Truncate(time.Microsecond)) {
		return errors.New("a replayed bed stay differs from the one recorded")
	}
	return nil
}

func stayEndedConsumer() events.Consumer {
	return events.Consumer{Name: "billing-bed-occupancy-ended", Subject: medicorecontract.SubjectBedOccupancyEnded, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		var d medicorecontract.BedOccupancyEndedData
		if evt.Type != "BedOccupancyEnded" || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil {
			return errors.New("invalid bed occupancy event")
		}
		v, ok := ids(d.HospitalID, d.AdmissionID, d.OccupancyID, d.BedID)
		at, err := time.Parse(time.RFC3339Nano, d.EndedAt)
		if !ok || err != nil {
			return errors.New("invalid bed occupancy event")
		}
		if err := lockAdmission(tx, evt.TenantID, v[1]); err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO billing_bed_stays (tenant_id,occupancy_id,hospital_id,admission_id,bed_id,ended_at) VALUES (?,?,?,?,?,?)
 ON CONFLICT (tenant_id,occupancy_id) DO UPDATE SET ended_at=EXCLUDED.ended_at WHERE billing_bed_stays.ended_at IS NULL`,
			evt.TenantID, v[2], v[0], v[1], v[3], at).Error; err != nil {
			return err
		}
		var held struct {
			HospitalID, AdmissionID, BedID uuid.UUID
			EndedAt                        time.Time
		}
		if err := tx.Raw(`SELECT hospital_id,admission_id,bed_id,ended_at FROM billing_bed_stays WHERE tenant_id=? AND occupancy_id=?`, evt.TenantID, v[2]).Scan(&held).Error; err != nil {
			return err
		}
		if held.HospitalID != v[0] || held.AdmissionID != v[1] || held.BedID != v[3] || !held.EndedAt.Equal(at.Truncate(time.Microsecond)) {
			return errors.New("a replayed bed stay differs from the one recorded")
		}
		return reconcileBedDays(tx, evt.TenantID, v[1])
	}}
}

// lockAdmission serialises everything about one admission's stays and days.
// Every path takes it before touching the account or a stay, so all lock in
// the same order; it is re-entrant within the transaction.
func lockAdmission(tx *gorm.DB, tenant string, admission uuid.UUID) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, tenant+":billing-bed-days:"+admission.String()).Error
}

type impliedDay struct {
	OccupancyID uuid.UUID
	BedClass    string
	CensusDate  time.Time
	CensusAt    time.Time
	Basis       string
}

type liveDay struct {
	ID          uuid.UUID
	OccupancyID uuid.UUID
	BedClass    string
	CensusDate  time.Time
	CensusAt    time.Time
	Basis       string
	Priced      bool
	AmountMinor *int64 `gorm:"column:daily_rate_minor"`
	Currency    *string
}

func dayKey(t time.Time) string { return t.Format(time.DateOnly) }

// reconcileBedDays records the days an admission's stays imply, voids those
// they no longer imply, and prices what a rate now covers. It is serialised
// per admission; each step is idempotent, so any caller may run it again.
func reconcileBedDays(tx *gorm.DB, tenant string, admission uuid.UUID) error {
	if err := lockAdmission(tx, tenant, admission); err != nil {
		return err
	}
	var accounts []struct {
		HospitalID uuid.UUID
		Status     string
	}
	if err := tx.Raw(`SELECT hospital_id,status FROM billing_admission_accounts WHERE tenant_id=? AND admission_id=?`, tenant, admission).Scan(&accounts).Error; err != nil {
		return err
	}
	if len(accounts) == 0 {
		// Only an end has arrived; its start opens the account and reconciles.
		// Nothing is due until then.
		return tx.Exec(`UPDATE billing_bed_stays SET next_census_at=NULL,census_failures=0 WHERE tenant_id=? AND admission_id=? AND (next_census_at IS NOT NULL OR census_failures<>0)`, tenant, admission).Error
	}
	hospital := accounts[0].HospitalID
	var zones []string
	if err := tx.Raw(`SELECT timezone FROM billing_bed_charging WHERE tenant_id=? AND hospital_id=? FOR SHARE`, tenant, hospital).Scan(&zones).Error; err != nil {
		return err
	}
	if len(zones) == 0 {
		// Not set up: no midnight is known. Setting the timezone marks every
		// stay here due, and the census computes them all then.
		return tx.Exec(`UPDATE billing_bed_stays SET next_census_at=NULL,census_failures=0 WHERE tenant_id=? AND admission_id=? AND (next_census_at IS NOT NULL OR census_failures<>0)`, tenant, admission).Error
	}
	zone := zones[0]
	var implied []impliedDay
	// Midnight M of census date d is the start of d+1 in the hospital's zone;
	// a stay counts it when started_at <= M < ended_at (a transfer at
	// midnight counts the new bed, so the series starts the day before: a start
	// at midnight exactly is the previous date's midnight). An open stay counts
	// midnights at least the grace ago. Each date goes to one stay: while an
	// earlier bed's end is still on its way, the latest stay started wins, as a
	// transfer's new bed does.
	if err := tx.Raw(`SELECT DISTINCT ON (g.d) s.occupancy_id,s.bed_class,g.d AS census_date,g.m AS census_at,'midnight' AS basis FROM billing_bed_stays s
 CROSS JOIN LATERAL (SELECT x::date AS d, ((x::date+1)::timestamp AT TIME ZONE ?) AS m FROM generate_series((s.started_at AT TIME ZONE ?)::date-1,
  (COALESCE(s.ended_at, now()-interval '`+censusGrace+`') AT TIME ZONE ?)::date, interval '1 day') x) g
 WHERE s.tenant_id=? AND s.admission_id=? AND s.started_at IS NOT NULL AND g.m >= s.started_at
  AND (g.m < s.ended_at OR (s.ended_at IS NULL AND g.m <= now()-interval '`+censusGrace+`'))
 ORDER BY g.d,s.started_at DESC,s.occupancy_id DESC`, zone, zone, zone, tenant, admission).Scan(&implied).Error; err != nil {
		return err
	}
	if len(implied) == 0 && accounts[0].Status == "closed" {
		// The minimum day: released, every known stay started and ended, and
		// no midnight crossed. Charged at the first bed's class.
		var first []impliedDay
		if err := tx.Raw(`SELECT s.occupancy_id,s.bed_class,(s.started_at AT TIME ZONE ?)::date AS census_date,s.started_at AS census_at,'minimum' AS basis FROM billing_bed_stays s
 WHERE s.tenant_id=? AND s.admission_id=? AND s.started_at IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM billing_bed_stays o WHERE o.tenant_id=s.tenant_id AND o.admission_id=s.admission_id AND (o.started_at IS NULL OR o.ended_at IS NULL))
 ORDER BY s.started_at,s.occupancy_id LIMIT 1`, zone, tenant, admission).Scan(&first).Error; err != nil {
			return err
		}
		implied = first
	}
	var live []liveDay
	if err := tx.Raw(`SELECT id,occupancy_id,bed_class,census_date,census_at,basis,priced_at IS NOT NULL AS priced,daily_rate_minor,currency FROM billing_bed_days
 WHERE tenant_id=? AND admission_id=? AND voided_at IS NULL ORDER BY census_date FOR UPDATE`, tenant, admission).Scan(&live).Error; err != nil {
		return err
	}
	want := map[string]impliedDay{}
	for _, d := range implied {
		want[dayKey(d.CensusDate)] = d
	}
	kept := map[string]bool{}
	for _, d := range live {
		w, ok := want[dayKey(d.CensusDate)]
		if ok && w.OccupancyID == d.OccupancyID && w.BedClass == d.BedClass && w.Basis == d.Basis {
			kept[dayKey(d.CensusDate)] = true
			continue
		}
		if err := voidDay(tx, tenant, hospital, admission, d); err != nil {
			return err
		}
	}
	for _, d := range implied {
		if kept[dayKey(d.CensusDate)] {
			continue
		}
		if err := tx.Exec(`INSERT INTO billing_bed_days (tenant_id,id,hospital_id,admission_id,census_date,census_at,basis,occupancy_id,bed_class) VALUES (?,?,?,?,?,?,?,?,?)`,
			tenant, uuid.Must(uuid.NewV7()), hospital, admission, d.CensusDate, d.CensusAt, d.Basis, d.OccupancyID, d.BedClass).Error; err != nil {
			return err
		}
	}
	if err := priceDays(tx, tenant, hospital, admission); err != nil {
		return err
	}
	// The next look at an open stay is its next midnight, past the grace;
	// rows already right are left alone, so the audit trail records changes.
	return tx.Exec(`UPDATE billing_bed_stays s SET next_census_at=x.v,census_failures=0 FROM (SELECT occupancy_id,CASE WHEN started_at IS NOT NULL AND ended_at IS NULL
  THEN ((((now()-interval '`+censusGrace+`') AT TIME ZONE ?)::date+1)::timestamp AT TIME ZONE ?)+interval '`+censusGrace+`' END AS v
  FROM billing_bed_stays WHERE tenant_id=? AND admission_id=?) x
 WHERE s.tenant_id=? AND s.occupancy_id=x.occupancy_id AND (s.next_census_at IS DISTINCT FROM x.v OR s.census_failures<>0)`, zone, zone, tenant, admission, tenant).Error
}

// voidDay takes back a day the stays no longer imply; a priced day is
// reversed on the account, so the charge and its correction both remain.
func voidDay(tx *gorm.DB, tenant string, hospital, admission uuid.UUID, d liveDay) error {
	if err := exactlyOne(tx.Exec(`UPDATE billing_bed_days SET voided_at=clock_timestamp() WHERE tenant_id=? AND id=? AND voided_at IS NULL`, tenant, d.ID)); err != nil {
		return err
	}
	if !d.Priced {
		return nil
	}
	return postEntry(tx, tenant, hospital, admission, "bed", "reversal", uuid.Must(uuid.NewV7()), d.ID, bedDocument(d.CensusDate), *d.AmountMinor, *d.Currency, d.CensusAt)
}

// priceDays prices every live unpriced day a rate covers, at the rate in
// force on its date. A priced day is never repriced.
func priceDays(tx *gorm.DB, tenant string, hospital, admission uuid.UUID) error {
	var priced []struct {
		ID             uuid.UUID
		CensusDate     time.Time
		CensusAt       time.Time
		DailyRateMinor int64
		Currency       string
	}
	if err := tx.Raw(`UPDATE billing_bed_days d SET daily_rate_minor=r.daily_rate_minor,currency=r.currency,rate_effective_from=r.effective_from,priced_at=clock_timestamp()
 FROM (SELECT u.id,x.daily_rate_minor,x.currency,x.effective_from FROM billing_bed_days u CROSS JOIN LATERAL (SELECT daily_rate_minor,currency,effective_from FROM billing_bed_rates r
   WHERE r.tenant_id=u.tenant_id AND r.hospital_id=u.hospital_id AND r.bed_class=u.bed_class AND r.effective_from<=u.census_date ORDER BY r.effective_from DESC LIMIT 1) x
  WHERE u.tenant_id=? AND u.admission_id=? AND u.priced_at IS NULL AND u.voided_at IS NULL) r
 WHERE d.tenant_id=? AND d.id=r.id
 RETURNING d.id,d.census_date,d.census_at,d.daily_rate_minor,d.currency`, tenant, admission, tenant).Scan(&priced).Error; err != nil {
		return err
	}
	for _, p := range priced {
		if err := postEntry(tx, tenant, hospital, admission, "bed", "charge", p.ID, p.ID, bedDocument(p.CensusDate), p.DailyRateMinor, p.Currency, p.CensusAt); err != nil {
			return err
		}
	}
	return nil
}

func bedDocument(day time.Time) string { return "Bed day " + day.Format(time.DateOnly) }

func exactlyOne(res *gorm.DB) error {
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return errors.New("bed day changed underneath the census")
	}
	return nil
}

// RunBedCensus counts passing midnights and applies new setup until ctx ends.
func RunBedCensus(ctx context.Context, deps platform.Deps) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := BedCensusOnce(ctx, deps.DB); err != nil {
				slog.WarnContext(ctx, "billing bed census pass failed", "error", err)
			}
		}
	}
}

// BedCensusOnce runs one pass; tests drive it instead of the ticker.
func BedCensusOnce(ctx context.Context, db *tenantdb.DB) error {
	deadline := time.Now().Add(censusBudget)
	ctx = tenantdb.WithActor(ctx, censusActor)
	if err := applySetup(ctx, db, deadline); err != nil {
		return err
	}
	for time.Now().Before(deadline) {
		var due []struct {
			TenantID    uuid.UUID
			OccupancyID uuid.UUID
		}
		// The one cross-tenant read: keys of due stays, nothing else (the
		// system role holds SELECT on just these columns).
		if err := db.WithAllTenants(ctx, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT tenant_id,occupancy_id FROM billing_bed_stays WHERE next_census_at <= now() ORDER BY next_census_at LIMIT ?`, censusBatch).Scan(&due).Error
		}); err != nil {
			return err
		}
		for _, d := range due {
			censusStay(ctx, db, d.TenantID, d.OccupancyID)
		}
		if len(due) < censusBatch {
			return nil
		}
	}
	return nil
}

// censusStay reconciles a due stay's admission. A failure pushes the stay
// back, in its own transaction, so the queue moves on.
func censusStay(ctx context.Context, db *tenantdb.DB, tenant, occupancy uuid.UUID) {
	err := db.WithTenant(ctx, tenant.String(), func(tx *gorm.DB) error {
		var admissions []uuid.UUID
		if err := tx.Raw(`SELECT admission_id FROM billing_bed_stays WHERE tenant_id=? AND occupancy_id=? AND next_census_at <= now()`, tenant, occupancy).Scan(&admissions).Error; err != nil {
			return err
		}
		if len(admissions) == 0 {
			return nil
		}
		return reconcileBedDays(tx, tenant.String(), admissions[0])
	})
	if err == nil {
		return
	}
	slog.WarnContext(ctx, "billing bed census failed for a stay; retrying later", "tenant_id", tenant, "occupancy_id", occupancy, "error", err)
	if err := db.WithTenant(ctx, tenant.String(), func(tx *gorm.DB) error {
		var failures int
		if err := tx.Raw(`SELECT census_failures FROM billing_bed_stays WHERE tenant_id=? AND occupancy_id=? FOR UPDATE`, tenant, occupancy).Scan(&failures).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE billing_bed_stays SET census_failures=census_failures+1,next_census_at=clock_timestamp()+make_interval(secs => ?) WHERE tenant_id=? AND occupancy_id=?`,
			censusRetry(failures+1).Seconds(), tenant, occupancy).Error
	}); err != nil {
		slog.WarnContext(ctx, "billing bed census could not defer a failed stay", "tenant_id", tenant, "occupancy_id", occupancy, "error", err)
	}
}

// applySetup brings settled new setup to the admissions it affects, one
// admission per transaction (the same lock order as the consumers), then
// marks it applied. An admission that fails is left due for the census.
func applySetup(ctx context.Context, db *tenantdb.DB, deadline time.Time) error {
	var setups []struct {
		TenantID   uuid.UUID
		HospitalID uuid.UUID
		BedClass   *string
		Effective  *time.Time `gorm:"column:effective_from"`
	}
	if err := db.WithAllTenants(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT tenant_id,hospital_id,bed_class,effective_from FROM (
  SELECT tenant_id,hospital_id,NULL::text AS bed_class,NULL::date AS effective_from,created_at FROM billing_bed_charging WHERE applied_at IS NULL
  UNION ALL SELECT tenant_id,hospital_id,bed_class,effective_from,created_at FROM billing_bed_rates WHERE applied_at IS NULL) u
 WHERE created_at <= now()-make_interval(secs => ?) ORDER BY created_at LIMIT ?`, censusSettle.Seconds(), censusBatch).Scan(&setups).Error
	}); err != nil {
		return err
	}
	for _, s := range setups {
		if !time.Now().Before(deadline) {
			return nil
		}
		tenant := s.TenantID.String()
		var admissions []uuid.UUID
		if err := db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			if s.BedClass == nil {
				return tx.Raw(`SELECT DISTINCT admission_id FROM billing_bed_stays WHERE tenant_id=? AND hospital_id=?`, s.TenantID, s.HospitalID).Scan(&admissions).Error
			}
			return tx.Raw(`SELECT DISTINCT admission_id FROM billing_bed_days WHERE tenant_id=? AND hospital_id=? AND bed_class=? AND census_date>=? AND priced_at IS NULL AND voided_at IS NULL`,
				s.TenantID, s.HospitalID, *s.BedClass, *s.Effective).Scan(&admissions).Error
		}); err != nil {
			slog.WarnContext(ctx, "billing bed setup not applied; retrying later", "tenant_id", s.TenantID, "hospital_id", s.HospitalID, "error", err)
			continue
		}
		for _, a := range admissions {
			if err := db.WithTenant(ctx, tenant, func(tx *gorm.DB) error { return reconcileBedDays(tx, tenant, a) }); err != nil {
				slog.WarnContext(ctx, "billing bed setup could not reconcile an admission; the census retries it", "tenant_id", s.TenantID, "admission_id", a, "error", err)
				if err := db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
					return tx.Exec(`UPDATE billing_bed_stays SET next_census_at=now() WHERE tenant_id=? AND admission_id=?`, s.TenantID, a).Error
				}); err != nil {
					slog.WarnContext(ctx, "billing bed setup could not mark an admission due", "tenant_id", s.TenantID, "admission_id", a, "error", err)
				}
			}
		}
		if err := db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			if s.BedClass == nil {
				return tx.Exec(`UPDATE billing_bed_charging SET applied_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND applied_at IS NULL`, s.TenantID, s.HospitalID).Error
			}
			return tx.Exec(`UPDATE billing_bed_rates SET applied_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND bed_class=? AND effective_from=? AND applied_at IS NULL`,
				s.TenantID, s.HospitalID, *s.BedClass, *s.Effective).Error
		}); err != nil {
			slog.WarnContext(ctx, "billing bed setup could not be marked applied; retrying later", "tenant_id", s.TenantID, "hospital_id", s.HospitalID, "error", err)
		}
	}
	return nil
}
