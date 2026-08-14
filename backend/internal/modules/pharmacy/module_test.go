package pharmacy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/modules/pharmacy" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/pagination"
	"github.com/tesserix/hms/pkg/tenantdb"
)

var (
	do = testutil.Do
)

var (
	busRef *events.Bus
)

func setup(t *testing.T) (*gin.Engine, *tenantdb.DB, *events.Bus, context.Context) {
	r, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"tokA": testutil.TenantA, "tokB": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"tokA": {
				pharmacy.PermMedicationWrite, pharmacy.PermMedicationRead,
				pharmacy.PermDispenseRead, pharmacy.PermDispenseFulfil,
			},
			"tokB": {pharmacy.PermDispenseRead, pharmacy.PermDispenseFulfil, pharmacy.PermMedicationRead},
		},
		Modules: []platform.Module{pharmacy.New()},
	})
	busRef = bus
	return r, db, bus, ctx
}

func TestVisitIntakeCreatesPendingDispense(t *testing.T) {
	r, db, _, ctx := setup(t)

	// Simulate medicore publishing visit_created (module boundary: we
	// publish the envelope, not import medicore).
	visitID := uuid.NewString()
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		data, _ := json.Marshal(map[string]string{
			"visit_id": visitID, "patient_name": "Asha Rao", "department": "OPD",
		})
		return busRef.Publish(tx, "hms.in.medicore.visit_created.v1", events.Event{
			Type: "VisitCreated", Version: 1, TenantID: testutil.TenantA, Data: data,
		})
	}))

	// Pending dispense appears for tenant A…
	require.Eventually(t, func() bool {
		return strings.Contains(do(r, "GET", "/v1/pharmacy/dispenses", "tokA", "").Body.String(), visitID)
	}, 20*time.Second, 200*time.Millisecond)
	require.Contains(t, do(r, "GET", "/v1/pharmacy/dispenses", "tokA", "").Body.String(), `"pending"`)
	// …and not for tenant B.
	require.NotContains(t, do(r, "GET", "/v1/pharmacy/dispenses", "tokB", "").Body.String(), visitID)
}

func TestDispenseFlow(t *testing.T) {
	r, db, _, ctx := setup(t)

	visitID := uuid.NewString()
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		data, _ := json.Marshal(map[string]string{
			"visit_id": visitID, "patient_name": "Asha Rao", "department": "OPD",
		})
		return busRef.Publish(tx, "hms.in.medicore.visit_created.v1", events.Event{
			Type: "VisitCreated", Version: 1, TenantID: testutil.TenantA, Data: data,
		})
	}))

	var dispenseID string
	require.Eventually(t, func() bool {
		var resp struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		_ = json.Unmarshal(do(r, "GET", "/v1/pharmacy/dispenses", "tokA", "").Body.Bytes(), &resp)
		if len(resp.Data) == 0 {
			return false
		}
		dispenseID = resp.Data[0].ID
		return true
	}, 20*time.Second, 200*time.Millisecond)

	// Cross-tenant dispense probe → 404.
	require.Equal(t, http.StatusNotFound,
		do(r, "POST", "/v1/pharmacy/dispenses/"+dispenseID+"/dispense", "tokB", `{"medication":"Paracetamol 500mg"}`).Code)

	// Dispense succeeds once…
	require.Equal(t, http.StatusOK,
		do(r, "POST", "/v1/pharmacy/dispenses/"+dispenseID+"/dispense", "tokA", `{"medication":"Paracetamol 500mg"}`).Code)
	require.Contains(t, do(r, "GET", "/v1/pharmacy/dispenses", "tokA", "").Body.String(), `"dispensed"`)

	// …and 409s on repeat.
	require.Equal(t, http.StatusConflict,
		do(r, "POST", "/v1/pharmacy/dispenses/"+dispenseID+"/dispense", "tokA", `{"medication":"Paracetamol 500mg"}`).Code)

	// dispense_recorded hit the outbox.
	require.Eventually(t, func() bool {
		var n int64
		_ = db.WithSystem(ctx, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM outbox_events
				WHERE subject = 'hms.in.pharmacy.dispense_recorded.v1'`).Scan(&n).Error
		})
		return n == 1
	}, 10*time.Second, 200*time.Millisecond)
}

// Exactly one of N concurrent fulfilments may win. The guarded UPDATE is
// what enforces it — under READ COMMITTED the losers re-evaluate
// "WHERE status = 'pending'" after the winner commits and match zero
// rows. Delete that branch and this test fails; the sequential flow test
// does not, because it never reaches the UPDATE.
func TestConcurrentDispenseYieldsExactlyOneWinner(t *testing.T) {
	r, db, _, ctx := setup(t)

	visitID := uuid.NewString()
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		data, _ := json.Marshal(map[string]string{
			"visit_id": visitID, "patient_name": "Asha Rao", "department": "OPD",
		})
		return busRef.Publish(tx, "hms.in.medicore.visit_created.v1", events.Event{
			Type: "VisitCreated", Version: 1, TenantID: testutil.TenantA, Data: data,
		})
	}))

	var dispenseID string
	require.Eventually(t, func() bool {
		var resp struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		_ = json.Unmarshal(do(r, "GET", "/v1/pharmacy/dispenses", "tokA", "").Body.Bytes(), &resp)
		if len(resp.Data) == 0 {
			return false
		}
		dispenseID = resp.Data[0].ID
		return true
	}, 20*time.Second, 200*time.Millisecond)

	const n = 8
	codes := make(chan int, n)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < n; i++ {
		go func() {
			start.Wait()
			codes <- do(r, "POST", "/v1/pharmacy/dispenses/"+dispenseID+"/dispense", "tokA",
				`{"medication":"Paracetamol 500mg"}`).Code
		}()
	}
	start.Done()

	ok, conflict, other := 0, 0, 0
	for i := 0; i < n; i++ {
		switch c := <-codes; c {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
		default:
			other++
			t.Logf("unexpected status %d", c)
		}
	}
	require.Equal(t, 1, ok, "exactly one fulfilment must win")
	require.Equal(t, n-1, conflict, "every loser must get 409")
	require.Zero(t, other)
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
