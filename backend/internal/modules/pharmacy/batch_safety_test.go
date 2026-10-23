package pharmacy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func stockPath(suffix string) string {
	return "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet + "/stock/" + suffix
}
func safetyFixture(t *testing.T) (*gin.Engine, *tenantdb.DB, string, string) {
	t.Helper()
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	_, location, batch := stockFixture(t, r)
	return r, db, location, batch
}

func safetyBody(batch, location string) string {
	return fmt.Sprintf(`{"batch_id":%q,"location_id":%q,"kind":"temperature_excursion","reason":"TEST refrigerator temperature excursion","occurred_at":"2026-01-01T10:00:00Z","measured_temperature_millicelsius":12000}`, batch, location)
}

func TestBatchSafetyJournalReplaysAndDoesNotReleaseStock(t *testing.T) {
	r, db, location, batch := safetyFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	body := safetyBody(batch, location)
	path := stockPath("safety-incidents/" + id)
	w := do(r, "PUT", path, "editor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "user-editor", result["actor_subject"])
	require.Equal(t, "temperature_excursion", result["kind"])
	replay := do(r, "PUT", path, "editor", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, w.Body.String(), replay.Body.String())
	require.Equal(t, 409, do(r, "PUT", path, "editor", strings.Replace(body, "12000", "14000", 1)).Code)
	history := do(r, "GET", stockPath("safety-incidents?batch_id="+batch), "editor", "")
	require.Equal(t, 200, history.Code, history.Body.String())
	require.Equal(t, "no-store", history.Header().Get("Cache-Control"))
	var page struct{ Data []map[string]any }
	require.NoError(t, json.Unmarshal(history.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, id, page.Data[0]["id"])
	var counts struct{ Incidents, Events, Audits int }
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (SELECT count(*) FROM pharmacy_batch_safety_incidents) incidents,
		 (SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.pharmacy.batch_safety_recorded.v1') events,
		 (SELECT count(*) FROM audit_events WHERE resource_type='pharmacy_batch_safety_incidents') audits`).Scan(&counts).Error
	}))
	require.Equal(t, 1, counts.Incidents)
	require.Equal(t, 1, counts.Events)
	require.Equal(t, 1, counts.Audits)
	balance := do(r, "GET", stockPath("balance?location_id="+location+"&batch_id="+batch), "editor", "")
	require.Equal(t, 200, balance.Code, balance.Body.String())
	require.Contains(t, balance.Body.String(), `"dispensable_units":0`)
}

func TestBatchSafetyScopesPermissionsAndValidation(t *testing.T) {
	r, _, location, batch := safetyFixture(t)
	grantCatalogue(t, r, "user-reader", "reader")
	path := stockPath("safety-incidents/" + uuid.Must(uuid.NewV7()).String())
	body := safetyBody(batch, location)
	require.Equal(t, 404, do(r, "PUT", path, "reader", body).Code)
	require.Equal(t, 403, do(r, "PUT", path, "denied", body).Code)
	for _, token := range []string{"sibling", "foreign"} {
		require.Equal(t, 404, do(r, "PUT", path, token, body).Code)
		require.Equal(t, 404, do(r, "GET", stockPath("safety-incidents?batch_id="+batch), token, "").Code)
	}
	require.Equal(t, 401, do(r, "PUT", path, "unknown", body).Code)
	for _, bad := range []string{
		strings.Replace(body, batch, uuid.Must(uuid.NewV7()).String(), 1),
		strings.Replace(body, location, uuid.Must(uuid.NewV7()).String(), 1),
	} {
		require.Equal(t, 404, do(r, "PUT", path, "editor", bad).Code)
	}
	for _, bad := range []string{
		strings.Replace(body, "temperature_excursion", "release", 1),
		strings.Replace(body, "TEST refrigerator temperature excursion", "  short  ", 1),
		strings.Replace(body, "TEST refrigerator temperature excursion", strings.Repeat("x", 501), 1),
		strings.Replace(body, "12000", "-273151", 1),
		strings.Replace(body, "12000", "1000001", 1),
		strings.Replace(body, "2026-01-01T10:00:00Z", "2026-01-01T10:00:00.1234567Z", 1),
		strings.Replace(body, "2026-01-01T10:00:00Z", time.Now().Add(time.Hour).UTC().Format(time.RFC3339), 1),
		strings.Replace(body, "2026-01-01T10:00:00Z", "invalid", 1),
		strings.Replace(body, "2026-01-01T10:00:00Z", "2026-01-01T10:00:00.1234560001Z", 1),
		strings.Replace(body, "2026-01-01T10:00:00Z", "2026-01-01T10:00:00,123456Z", 1),
		strings.Replace(body, "2026-01-01T10:00:00Z", "0001-01-01T00:00:00+01:00", 1),
		strings.Replace(body, `,"measured_temperature_millicelsius":12000`, "", 1),
		strings.Replace(body, "temperature_excursion", "packaging_damage", 1),
		strings.Replace(body, `"batch_id":`, `"actor_subject":"forged","batch_id":`, 1),
	} {
		w := do(r, "PUT", path, "editor", bad)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	for _, test := range []struct {
		query string
		want  int
	}{
		{"", 400}, {"?batch_id=bad", 400}, {"?batch_id=" + uuid.Must(uuid.NewV7()).String(), 404},
	} {
		require.Equal(t, test.want, do(r, "GET", stockPath("safety-incidents"+test.query), "editor", "").Code)
	}

}

func TestBatchSafetyConcurrentReplayAndDatabaseImmutability(t *testing.T) {
	r, db, location, batch := safetyFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	body := safetyBody(batch, location)
	var wg sync.WaitGroup
	codes := make(chan int, 6)
	for range 6 {
		wg.Go(func() { codes <- do(r, "PUT", stockPath("safety-incidents/"+id), "editor", body).Code })
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		require.Equal(t, 200, code)
	}
	for _, sql := range []string{
		`UPDATE pharmacy_batch_safety_incidents SET reason='forged reason'`,
		`DELETE FROM pharmacy_batch_safety_incidents`,
		`TRUNCATE pharmacy_batch_safety_incidents`,
	} {
		err := catalogueTx(t, db, func(tx *gorm.DB) error {
			return tx.Exec(sql).Error
		})
		require.Error(t, err)
	}
	var count int64
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		return tx.Table("pharmacy_batch_safety_incidents").Count(&count).Error
	}))
	require.EqualValues(t, 1, count)
}

func TestBatchSafetyOptionalLocationReaderPaginationAndRevokedGrant(t *testing.T) {
	r, _, location, batch := safetyFixture(t)
	grantCatalogue(t, r, "user-reader", "reader")
	for i := range 3 {
		body := fmt.Sprintf(`{"batch_id":%q,"kind":"quality_concern","reason":"TEST quality concern %d","occurred_at":"2026-01-01T10:00:00+05:30"}`, batch, i)
		w := do(r, "PUT", stockPath("safety-incidents/"+uuid.Must(uuid.NewV7()).String()), "editor", body)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		w := do(r, "GET", stockPath("safety-incidents?batch_id="+batch+"&limit=1&cursor="+cursor), "reader", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var page struct {
			Data []struct {
				ID         string
				LocationID *string `json:"location_id"`
				OccurredAt string  `json:"occurred_at"`
			}
			Page struct {
				HasMore    bool   `json:"has_more"`
				NextCursor string `json:"next_cursor"`
			}
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		require.Len(t, page.Data, 1)
		require.False(t, seen[page.Data[0].ID])
		require.Nil(t, page.Data[0].LocationID)
		require.Equal(t, "2026-01-01T04:30:00Z", page.Data[0].OccurredAt)
		seen[page.Data[0].ID] = true
		if !page.Page.HasMore {
			break
		}
		require.NotEmpty(t, page.Page.NextCursor)
		cursor = page.Page.NextCursor
	}
	require.Len(t, seen, 3)
	id := uuid.Must(uuid.NewV7()).String()
	body := safetyBody(batch, location)
	require.Equal(t, 200, do(r, "PUT", stockPath("safety-incidents/"+id), "editor", body).Code)
	grantCatalogue(t, r, "user-sibling", "editor")
	require.Equal(t, 409, do(r, "PUT", stockPath("safety-incidents/"+id), "sibling", body).Code)
	require.Equal(t, 200, do(r, "DELETE", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+pharmaOutlet+"/access/user-editor", "editor", "").Code)
	require.Equal(t, 404, do(r, "GET", stockPath("safety-incidents?batch_id="+batch), "editor", "").Code)
	require.Equal(t, 404, do(r, "PUT", stockPath("safety-incidents/"+id), "editor", body).Code)
}

func TestBatchSafetyDatabaseRejectsSpoofedActorsAndForeignReferences(t *testing.T) {
	r, db, location, batch := safetyFixture(t)
	for _, test := range []struct{ tenant, outlet, batch, location, actor string }{
		{testutil.TenantA, pharmaOutlet, batch, location, "forged-actor"},
		{testutil.TenantB, pharmaOutlet, batch, location, "user-editor"},
		{testutil.TenantA, pharmaOutlet, uuid.Must(uuid.NewV7()).String(), location, "user-editor"},
		{testutil.TenantA, pharmaOutlet, batch, uuid.Must(uuid.NewV7()).String(), "user-editor"},
	} {
		require.Error(t, catalogueTx(t, db, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO pharmacy_batch_safety_incidents(id,tenant_id,outlet_id,batch_id,location_id,kind,reason,occurred_at,actor_subject) VALUES (?,?,?,?,?,'quality_concern','TEST direct validation','2026-01-01',?)`, uuid.Must(uuid.NewV7()), test.tenant, test.outlet, test.batch, test.location, test.actor).Error
		}))
	}
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		var role struct{ Superuser, Bypass bool }
		if err := tx.Raw(`SELECT rolsuper superuser,rolbypassrls bypass FROM pg_roles WHERE rolname=current_user`).Scan(&role).Error; err != nil {
			return err
		}
		require.False(t, role.Superuser)
		require.False(t, role.Bypass)
		return nil
	}))
	foreignPath := strings.Replace(stockPath("safety-incidents/"+uuid.Must(uuid.NewV7()).String()), pharmaOrganization, uuid.Must(uuid.NewV7()).String(), 1)
	require.Equal(t, 404, do(r, "PUT", foreignPath, "editor", safetyBody(batch, location)).Code)
}

func TestBatchSafetyOutboxFailureRollsBackIncidentAndAudit(t *testing.T) {
	r, db, location, batch := safetyFixture(t)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_safety_outbox_failure", SQL: `
	 CREATE FUNCTION test_safety_outbox_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
	 IF NEW.subject='helivanta.in.pharmacy.batch_safety_recorded.v1' THEN RAISE EXCEPTION 'TEST outbox unavailable'; END IF;
	 RETURN NEW;END $$;
	 CREATE TRIGGER test_safety_outbox_failure BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION test_safety_outbox_fail();`}}))
	w := do(r, "PUT", stockPath("safety-incidents/"+uuid.Must(uuid.NewV7()).String()), "editor", safetyBody(batch, location))
	require.Equal(t, 500, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "TEST")
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("pharmacy_batch_safety_incidents").Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count)
		if err := tx.Table("audit_events").Where("resource_type='pharmacy_batch_safety_incidents'").Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count)
		return nil
	}))
}

func TestBatchSafetyOutboxReachesJetStreamWithoutDescriptions(t *testing.T) {
	r, db, bus, ctx := catalogueSetup(t)
	seedPharmacyOutlet(t, r)
	grantCatalogue(t, r, "user-editor", "editor")
	_, location, batch := stockFixture(t, r)
	delivered := make(chan events.Event, 1)
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{Name: "TEST-batch-safety-observer", Subject: "helivanta.in.pharmacy.batch_safety_recorded.v1", Handle: func(_ context.Context, _ *gorm.DB, event events.Event) error {
		select {
		case delivered <- event:
		default:
		}
		return nil
	}}}))
	id := uuid.Must(uuid.NewV7()).String()
	body := safetyBody(batch, location)
	require.Equal(t, 200, do(r, "PUT", stockPath("safety-incidents/"+id), "editor", body).Code)
	require.Equal(t, 200, do(r, "PUT", stockPath("safety-incidents/"+id), "editor", body).Code)
	select {
	case event := <-delivered:
		require.Equal(t, testutil.TenantA, event.TenantID)
		require.Equal(t, "BatchSafetyRecorded", event.Type)
		require.JSONEq(t, fmt.Sprintf(`{"pharmacy_id":%q,"outlet_id":%q,"batch_id":%q,"incident_id":%q}`, pharmaOrganization, pharmaOutlet, batch, id), string(event.Data))
	case <-ctx.Done():
		t.Fatal("batch safety outbox did not reach JetStream")
	}
}

func TestBatchSafetyDatabaseTimestampAndAuditAttribution(t *testing.T) {
	_, db, location, batch := safetyFixture(t)
	insert := func(tx *gorm.DB, occurred, recorded string) error {
		return tx.Exec(`INSERT INTO pharmacy_batch_safety_incidents(id,tenant_id,outlet_id,batch_id,location_id,kind,reason,occurred_at,created_at,actor_subject) VALUES (?,?,?,?,?,'quality_concern','TEST timestamp integrity',?,?, 'user-editor')`, uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, batch, location, occurred, recorded).Error
	}
	for _, occurred := range []string{"2099-01-01", "-infinity", "infinity", "0001-12-31 BC"} {
		require.Error(t, catalogueTx(t, db, func(tx *gorm.DB) error { return insert(tx, occurred, "2099-01-01") }))
	}
	for _, actor := range []string{"forged-audit-actor", ""} {
		require.Error(t, catalogueTx(t, db, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, actor).Error; err != nil {
				return err
			}
			return insert(tx, "2026-01-01", "2026-01-01")
		}))
	}
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		if err := insert(tx, "2026-01-01", "2000-01-01"); err != nil {
			return err
		}
		var valid bool
		if err := tx.Raw(`SELECT bool_and(created_at>clock_timestamp()-interval '1 minute' AND occurred_at='2026-01-01'::timestamptz) FROM pharmacy_batch_safety_incidents`).Scan(&valid).Error; err != nil {
			return err
		}
		require.True(t, valid)
		return nil
	}))
}

// #576 D4: the overview's safety count opens the outlet's concerns recorded
// in the last 30 outlet-local days, today included. created_at comes from
// the database clock, so the module clock moves forward instead: 29 days on,
// a concern recorded today is still in the window; 30 days on, it is not.
func TestRecentSafetyConcernsOutletWide(t *testing.T) {
	r, _, location, batch := safetyFixture(t)
	for range 2 {
		w := do(r, "PUT", stockPath("safety-incidents/"+uuid.Must(uuid.NewV7()).String()), "editor", safetyBody(batch, location))
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	recorded := time.Now().In(kolkata)
	count := func(daysLater int) int {
		later := time.Date(recorded.Year(), recorded.Month(), recorded.Day()+daysLater, 12, 0, 0, 0, kolkata)
		pharmacy.SetClock(t, func() time.Time { return later })
		w := do(r, "GET", stockPath("safety-incidents?recorded=30d"), "editor", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var page struct{ Data []json.RawMessage }
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		return len(page.Data)
	}
	require.Equal(t, 2, count(0))
	require.Equal(t, 2, count(29))
	require.Equal(t, 0, count(30))

	require.Equal(t, 400, do(r, "GET", stockPath("safety-incidents"), "editor", "").Code)
	require.Equal(t, 400, do(r, "GET", stockPath("safety-incidents?recorded=7d"), "editor", "").Code)
}
