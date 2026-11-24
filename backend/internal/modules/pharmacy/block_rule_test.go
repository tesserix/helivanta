package pharmacy_test

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// #1634 slice 0a: one database rule decides whether a lot may leave a
// sellable shelf, on the outlet's own date.

// rewriteBatch simulates legacy data or the calendar moving on; batches are
// otherwise immutable.
func (w releaseWorld) rewriteBatch(t *testing.T, batch, set string) {
	t.Helper()
	require.NoError(t, w.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_rewrite_batch_" + uuid.Must(uuid.NewV7()).String(), SQL: `
 SELECT set_config('app.tenant_id','` + testutil.TenantA + `',true),set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet','` + pharmaOutlet + `',true),set_config('app.pharmacy_id','` + pharmaOrganization + `',true);
 ALTER TABLE pharmacy_stock_batches DISABLE TRIGGER pharmacy_stock_batch_immutable;
 DO $$ DECLARE changed bigint; BEGIN
 UPDATE pharmacy_stock_batches SET ` + set + ` WHERE id='` + batch + `';
 GET DIAGNOSTICS changed=ROW_COUNT;
 IF changed<>1 THEN RAISE EXCEPTION 'fixture must change exactly one batch'; END IF;
 END $$;
 ALTER TABLE pharmacy_stock_batches ENABLE TRIGGER pharmacy_stock_batch_immutable;`}}))
}

func (w releaseWorld) incident(t *testing.T, batch, location string) {
	t.Helper()
	res := do(w.r, "PUT", stockPath("safety-incidents/"+uuid.Must(uuid.NewV7()).String()), "editor", safetyBody(batch, location))
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
}

func blockReason(t *testing.T, db *tenantdb.DB, batch string, day any) (sql.NullString, error) {
	t.Helper()
	var reason sql.NullString
	err := catalogueTx(t, db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT pharmacy_lot_block_reason(?,?,?,?::date)`, testutil.TenantA, pharmaOutlet, batch, day).Row().Scan(&reason)
	})
	return reason, err
}

func sqlState(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	return ""
}

// AC-0a-01
func TestLotBlockReasonReportsExpiryThenMRPThenIncident(t *testing.T) {
	w := newReleaseWorld(t)
	everything := receive(t, w.r, w.product, w.quarantine, "2026-11-01", `,"mrp_minor":12500`)
	w.rewriteBatch(t, everything, "mrp_minor=NULL")
	w.incident(t, everything, w.quarantine)
	unpriced := receive(t, w.r, w.product, w.quarantine, "2030-10-31", `,"mrp_minor":12500`)
	w.rewriteBatch(t, unpriced, "mrp_minor=NULL")
	w.incident(t, unpriced, w.quarantine)
	held := receive(t, w.r, w.product, w.quarantine, "2030-10-31", `,"mrp_minor":12500`)
	w.incident(t, held, w.quarantine)

	for batch, want := range map[string]sql.NullString{
		everything: {String: "expired", Valid: true},
		unpriced:   {String: "mrp_missing", Valid: true},
		held:       {String: "safety_incident", Valid: true},
		w.batch:    {},
	} {
		got, err := blockReason(t, w.db, batch, "2026-11-01")
		require.NoError(t, err)
		require.Equal(t, want, got, batch)
	}
}

// AC-0a-02
func TestLotBlockReasonTreatsTheExpiryDayAsExpired(t *testing.T) {
	w := newReleaseWorld(t)
	got, err := blockReason(t, w.db, w.batch, "2030-10-30")
	require.NoError(t, err)
	require.False(t, got.Valid, "sellable the day before expiry")
	got, err = blockReason(t, w.db, w.batch, "2030-10-31")
	require.NoError(t, err)
	require.Equal(t, sql.NullString{String: "expired", Valid: true}, got)
}

// AC-0a-03: null means "may sell", so an unanswerable call aborts instead.
func TestLotBlockReasonFailsClosed(t *testing.T) {
	w := newReleaseWorld(t)
	_, err := blockReason(t, w.db, w.batch, nil)
	require.Equal(t, "22004", sqlState(err), "a null day: %v", err)
	_, err = blockReason(t, w.db, uuid.Must(uuid.NewV7()).String(), "2026-11-01")
	require.Equal(t, "P0002", sqlState(err), "an unknown lot: %v", err)
}

// AC-0a-04: UTC+14 and UTC-11 are always on different dates.
func TestOutletTodayUsesTheOutletsOwnZone(t *testing.T) {
	r, db := catalogueHarness(t)
	east, west := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	for code, id := range map[string]string{"TEST-EAST": east, "TEST-WEST": west} {
		res := do(r, "PUT", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+id, "editor", fmt.Sprintf(`{"code":%q,"name":"TEST Outlet","country_code":"IN","timezone":"Asia/Kolkata"}`, code))
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	}
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_outlet_zones_" + east, SQL: `
 SELECT set_config('app.tenant_id','` + testutil.TenantA + `',true);
 ALTER TABLE pharmacy_outlets DISABLE TRIGGER pharmacy_outlet_immutable;
 DO $$ DECLARE changed bigint; BEGIN
 UPDATE pharmacy_outlets SET timezone=CASE id WHEN '` + east + `' THEN 'Pacific/Kiritimati' ELSE 'Pacific/Pago_Pago' END WHERE id IN ('` + east + `','` + west + `');
 GET DIAGNOSTICS changed=ROW_COUNT;
 IF changed<>2 THEN RAISE EXCEPTION 'fixture must rezone exactly two outlets'; END IF;
 END $$;
 ALTER TABLE pharmacy_outlets ENABLE TRIGGER pharmacy_outlet_immutable;`}}))

	var before, after time.Time
	var eastDay, westDay string
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT clock_timestamp()`).Row().Scan(&before); err != nil {
			return err
		}
		if err := tx.Raw(`SELECT pharmacy_outlet_today(?,?)::text,pharmacy_outlet_today(?,?)::text`, testutil.TenantA, east, testutil.TenantA, west).Row().Scan(&eastDay, &westDay); err != nil {
			return err
		}
		return tx.Raw(`SELECT clock_timestamp()`).Row().Scan(&after)
	}))
	for day, zone := range map[string]string{eastDay: "Pacific/Kiritimati", westDay: "Pacific/Pago_Pago"} {
		location, err := time.LoadLocation(zone)
		require.NoError(t, err)
		require.Contains(t, []string{before.In(location).Format(time.DateOnly), after.In(location).Format(time.DateOnly)}, day, zone)
	}
	require.NotEqual(t, eastDay, westDay)

	err := catalogueTx(t, db, func(tx *gorm.DB) error {
		var day sql.NullString
		return tx.Raw(`SELECT pharmacy_outlet_today(?,?)::text`, testutil.TenantA, uuid.Must(uuid.NewV7()).String()).Row().Scan(&day)
	})
	require.Equal(t, "P0002", sqlState(err), "an unknown outlet has no date: %v", err)
}

// AC-0a-05
func TestEveryBlockReasonStopsDispensing(t *testing.T) {
	w := newReleaseWorld(t)
	shelve := func() string {
		batch := receive(t, w.r, w.product, w.quarantine, "2030-10-31", `,"mrp_minor":12500`)
		code, msg := w.release(batch, 30)
		require.Equal(t, http.StatusOK, code, msg)
		return batch
	}
	control := shelve()
	expired, unpriced, held := shelve(), shelve(), shelve()
	w.rewriteBatch(t, expired, "expires_on='2026-11-01'")
	w.rewriteBatch(t, unpriced, "mrp_minor=NULL")
	w.incident(t, held, w.sellable)
	for reason, batch := range map[string]string{"expired": expired, "mrp_missing": unpriced, "safety_incident": held} {
		require.EqualValues(t, 30, w.balance(t, w.sellable, batch), reason+": the units are still on hand")
		require.EqualValues(t, 0, w.dispensable(t, w.sellable, batch), reason)
	}
	require.EqualValues(t, 30, w.dispensable(t, w.sellable, control))
}

// AC-0a-11
func TestOnlyTheBlockRuleReadsSafetyIncidents(t *testing.T) {
	w := newReleaseWorld(t)
	var readers, fixedZone []string
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		// Rows are read FROM/JOIN the table; the operating-choice fingerprint only names its policy.
		if err := tx.Raw(`SELECT proname FROM pg_proc WHERE pronamespace=current_schema()::regnamespace AND proname LIKE 'pharmacy\_%' AND prosrc ~* '\m(from|join)\s+(\w+\.)?pharmacy_batch_safety_incidents\M' ORDER BY proname`).Scan(&readers).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT proname FROM pg_proc WHERE pronamespace=current_schema()::regnamespace AND proname LIKE 'pharmacy\_%' AND prosrc LIKE '%Asia/Kolkata%' ORDER BY proname`).Scan(&fixedZone).Error
	}))
	require.Equal(t, []string{"pharmacy_lot_block_reasons"}, readers)
	require.Empty(t, fixedZone)
}

// AC-0a-10 and AC-0a-12
func TestPharmacyDatesUseTheOutletsZone(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	var fixedZone, incidentReaders []string
	asks := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		require.NoError(t, err)
		source := string(raw)
		for i, line := range strings.Split(source, "\n") {
			// A new outlet's default zone is a value, not a date rule.
			defaultZone := name == "customer_workspace.go" && strings.Contains(line, `Timezone: "Asia/Kolkata"`)
			if strings.Contains(line, "time.FixedZone") || strings.Contains(line, `"Asia/Kolkata"`) && !defaultZone {
				fixedZone = append(fixedZone, fmt.Sprintf("%s:%d", name, i+1))
			}
		}
		if name != "batch_safety.go" && strings.Contains(source, "pharmacy_batch_safety_incidents") {
			incidentReaders = append(incidentReaders, name)
		}
		if name == "fulfilment_safety.go" {
			asks = strings.Contains(source, "pharmacy_lot_block_reason(")
		}
	}
	require.Empty(t, fixedZone, "dates come from the outlet's zone")
	require.Empty(t, incidentReaders, "only the block rule reads incidents")
	require.True(t, asks, "the shipment pre-check asks the block rule")
}
