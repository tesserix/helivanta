package pharmacy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var (
	do = testutil.Do
)

var (
	busRef *events.Bus
)

func setup(t *testing.T) (*gin.Engine, *tenantdb.DB, *events.Bus, context.Context) {
	r, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"tokA": testutil.TenantA, "tokB": testutil.TenantB, "tokRead": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"tokA": {
				pharmacy.PermMedicationWrite, pharmacy.PermMedicationRead,
				pharmacy.PermDispenseRead, pharmacy.PermDispenseFulfil,
			},
			"tokB":    {pharmacy.PermDispenseRead, pharmacy.PermDispenseFulfil, pharmacy.PermMedicationRead},
			"tokRead": {pharmacy.PermDispenseRead},
		},
		Modules: []platform.Module{pharmacy.New()},
	})
	busRef = bus
	return r, db, bus, ctx
}

// New visits no longer open a pharmacy dispense (#1680): the free-text queue
// they fed is retired, so a row would be work nobody can complete. Lab keeps
// its own visit intake; this only removes pharmacy's.
func TestVisitCreatedNoLongerOpensADispense(t *testing.T) {
	for _, c := range pharmacy.New().Consumers(platform.Deps{}) {
		require.NotEqual(t, "helivanta.in.medicore.visit_created.v1", c.Subject, "consumer %s", c.Name)
	}

	r, db, bus, ctx := setup(t)
	delivered := make(chan events.Event, 1)
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{Name: "TEST-visit-observer", Subject: "helivanta.in.medicore.visit_created.v1",
		Handle: func(_ context.Context, _ *gorm.DB, event events.Event) error { delivered <- event; return nil }}}))

	visitID := uuid.NewString()
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		data, _ := json.Marshal(map[string]string{"visit_id": visitID, "patient_name": "Asha Rao", "department": "OPD"})
		return busRef.Publish(tx, "helivanta.in.medicore.visit_created.v1", events.Event{
			Type: "VisitCreated", Version: 1, TenantID: testutil.TenantA, Data: data,
		})
	}))
	select {
	case <-delivered:
	case <-time.After(20 * time.Second):
		t.Fatal("visit_created.v1 was never delivered, so the absence below would prove nothing")
	}
	require.Never(t, func() bool {
		var n int64
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM pharmacy_dispenses WHERE visit_id = ?`, visitID).Scan(&n).Error
		}))
		return n > 0
	}, 3*time.Second, 200*time.Millisecond)
	require.NotContains(t, do(r, "GET", "/v1/pharmacy/dispenses", "tokA", "").Body.String(), visitID)
}

// The free-text dispense route is retired (#1680): it recorded a hand-over
// with no product, stock, schedule, prescription or register check. Every
// permitted call answers 410 without reading the row, so no row state —
// pending, dispensed, missing or another tenant's — reaches a write or
// reveals whether the row exists.
func TestRetiredDispenseRouteRefusesWithoutWriting(t *testing.T) {
	r, db, _, ctx := setup(t)
	seed := tenantdb.WithActor(ctx, "test-seed")

	var pending, dispensed, foreign string
	require.NoError(t, db.WithTenant(seed, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Raw(`INSERT INTO pharmacy_dispenses (tenant_id, visit_id, patient_name)
			VALUES (?, ?, 'Asha Rao') RETURNING id`, testutil.TenantA, uuid.New()).Scan(&pending).Error; err != nil {
			return err
		}
		return tx.Raw(`INSERT INTO pharmacy_dispenses (tenant_id, visit_id, patient_name, medication, status, dispensed_at)
			VALUES (?, ?, 'Ravi Kumar', 'Paracetamol 500mg', 'dispensed', now()) RETURNING id`, testutil.TenantA, uuid.New()).Scan(&dispensed).Error
	}))
	require.NoError(t, db.WithTenant(seed, testutil.TenantB, func(tx *gorm.DB) error {
		return tx.Raw(`INSERT INTO pharmacy_dispenses (tenant_id, visit_id, patient_name)
			VALUES (?, ?, 'Other Tenant') RETURNING id`, testutil.TenantB, uuid.New()).Scan(&foreign).Error
	}))

	type snapshot struct {
		Rows    string
		Outbox  int64
		Updates int64
	}
	snap := func() snapshot {
		var s snapshot
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Raw(`SELECT string_agg(row_to_json(d)::text, '|' ORDER BY id) FROM pharmacy_dispenses d
				WHERE id IN (?, ?)`, pending, dispensed).Scan(&s.Rows).Error; err != nil {
				return err
			}
			if err := tx.Raw(`SELECT count(*) FROM outbox_events`).Scan(&s.Outbox).Error; err != nil {
				return err
			}
			return tx.Raw(`SELECT count(*) FROM audit_events WHERE resource_type = 'pharmacy_dispenses'
				AND action = 'update'`).Scan(&s.Updates).Error
		}))
		return s
	}
	before := snap()
	require.Contains(t, before.Rows, `"status":"pending"`)
	require.Contains(t, before.Rows, `"status":"dispensed"`)

	for name, id := range map[string]string{"pending": pending, "dispensed": dispensed, "unknown": uuid.NewString(), "other tenant": foreign} {
		w := do(r, "POST", "/v1/pharmacy/dispenses/"+id+"/dispense", "tokA", `{"medication":"Paracetamol 500mg"}`)
		require.Equal(t, http.StatusGone, w.Code, name)
		require.Contains(t, w.Body.String(), `"dispense_retired"`, name)
	}
	// Without the permission it stays 403: retirement does not widen who may call.
	require.Equal(t, http.StatusForbidden,
		do(r, "POST", "/v1/pharmacy/dispenses/"+pending+"/dispense", "tokRead", `{"medication":"Paracetamol 500mg"}`).Code)

	require.Equal(t, before, snap(), "the retired route must not change a row, the outbox or the audit trail")

	// History stays readable and the server-side status split (#1258 D4)
	// still holds, so a pending row is never hidden behind dispensed ones.
	statusBody := func(query string) (int, string) {
		w := do(r, "GET", "/v1/pharmacy/dispenses"+query, "tokA", "")
		return w.Code, w.Body.String()
	}
	code, body := statusBody("?status=dispensed")
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, body, dispensed)
	require.Contains(t, body, `"medication":"Paracetamol 500mg"`)
	require.NotContains(t, body, pending)
	code, body = statusBody("?status=pending")
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, body, pending)
	require.NotContains(t, body, dispensed)
	code, _ = statusBody("?status=cancelled")
	require.Equal(t, http.StatusBadRequest, code)
}

func TestMedicationsCrud(t *testing.T) {
	r, _, _, _ := setup(t)
	require.Equal(t, http.StatusCreated,
		do(r, "POST", "/v1/pharmacy/medications", "tokA", `{"name":"Paracetamol","strength":"500mg"}`).Code)
	require.Equal(t, http.StatusBadRequest,
		do(r, "POST", "/v1/pharmacy/medications", "tokA", `{}`).Code)
	require.Contains(t, do(r, "GET", "/v1/pharmacy/medications", "tokA", "").Body.String(), "Paracetamol")
	require.NotContains(t, do(r, "GET", "/v1/pharmacy/medications", "tokB", "").Body.String(), "Paracetamol")
}

// seedMedications inserts n medications directly so
// TestMedicationsArePaginated controls exactly how many rows exist.
func seedMedications(t *testing.T, db *tenantdb.DB, ctx context.Context, tenantID uuid.UUID, n int) {
	ctx = tenantdb.WithActor(ctx, "test-seed")
	t.Helper()
	for i := range n {
		name := fmt.Sprintf("med-%02d", i)
		require.NoError(t, db.WithTenant(ctx, tenantID.String(), func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO pharmacy_medications (tenant_id, name, strength) VALUES (?, ?, '500mg')`,
				tenantID, name).Error
		}))
	}
}

// seedDispenses inserts n dispenses directly so TestDispensesArePaginated
// controls exactly how many rows exist.
func seedDispenses(t *testing.T, db *tenantdb.DB, ctx context.Context, tenantID uuid.UUID, n int) {
	ctx = tenantdb.WithActor(ctx, "test-seed")
	t.Helper()
	for i := range n {
		name := fmt.Sprintf("dispense-%02d", i)
		require.NoError(t, db.WithTenant(ctx, tenantID.String(), func(tx *gorm.DB) error {
			return tx.Exec(
				`INSERT INTO pharmacy_dispenses (tenant_id, visit_id, patient_name) VALUES (?, ?, ?)`,
				tenantID, uuid.New(), name).Error
		}))
	}
}

// TestMedicationsArePaginated walks a seeded tenant's medications to
// exhaustion through the cursor and asserts every seeded medication
// appears exactly once — same pattern medicore's TestVisitsArePaginated
// proved, repeated for pharmacy's second collection.
func TestMedicationsArePaginated(t *testing.T) {
	r, db, _, ctx := setup(t)
	tenantID := uuid.MustParse(testutil.TenantA)
	seedMedications(t, db, ctx, tenantID, 7)

	w := do(r, "GET", "/v1/pharmacy/medications?limit=3", "tokA", "")
	require.Equal(t, http.StatusOK, w.Code)

	seen := paginateAll(t, r, "/v1/pharmacy/medications", "tokA", 3)
	require.Len(t, seen, 7)
	for name, n := range seen {
		require.Equal(t, 1, n, "medication %s appeared %d times across pages", name, n)
	}
}

// TestDispensesArePaginated is the same property for pharmacy's other
// collection, /dispenses.
func TestDispensesArePaginated(t *testing.T) {
	r, db, _, ctx := setup(t)
	tenantID := uuid.MustParse(testutil.TenantA)
	seedDispenses(t, db, ctx, tenantID, 7)

	seen := paginateAll(t, r, "/v1/pharmacy/dispenses", "tokA", 3)
	require.Len(t, seen, 7)
	for name, n := range seen {
		require.Equal(t, 1, n, "dispense %s appeared %d times across pages", name, n)
	}
}

// paginateAll walks path to exhaustion via limit and cursor, returning
// how many times each row's patient_name/name was seen. Shared by both
// pharmacy collections since the walk shape is identical.
func paginateAll(t *testing.T, r *gin.Engine, path, token string, limit int) map[string]int {
	t.Helper()
	type row struct {
		Name string `json:"name"`
		PN   string `json:"patient_name"`
	}
	seen := map[string]int{}
	cursor := ""
	for i := 0; i < 20; i++ {
		q := fmt.Sprintf("%s?limit=%d", path, limit)
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		w := do(r, "GET", q, token, "")
		require.Equal(t, http.StatusOK, w.Code)
		var body struct {
			Data []row `json:"data"`
			Page struct {
				NextCursor *string `json:"next_cursor"`
				HasMore    bool    `json:"has_more"`
			} `json:"page"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		for _, rr := range body.Data {
			name := rr.Name
			if name == "" {
				name = rr.PN
			}
			seen[name]++
		}
		if !body.Page.HasMore {
			require.Nil(t, body.Page.NextCursor)
			return seen
		}
		cursor = *body.Page.NextCursor
	}
	t.Fatal("did not reach the last page within 20 iterations")
	return nil
}

// TestPharmacyRejectAnotherTenantsCursor mirrors medicore's cursor
// tenant-mismatch test for pharmacy's two collections.
func TestPharmacyRejectAnotherTenantsCursor(t *testing.T) {
	r, _, _, _ := setup(t)
	foreign := pagination.Cursor{
		TenantID:  testutil.TenantB,
		CreatedAt: time.Now().UTC(),
		ID:        uuid.New(),
	}.Encode()

	require.Equal(t, http.StatusBadRequest,
		do(r, "GET", "/v1/pharmacy/medications?cursor="+url.QueryEscape(foreign), "tokA", "").Code)
	require.Equal(t, http.StatusBadRequest,
		do(r, "GET", "/v1/pharmacy/dispenses?cursor="+url.QueryEscape(foreign), "tokA", "").Code)
}
