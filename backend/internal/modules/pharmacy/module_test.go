package pharmacy_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/modules/pharmacy" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

var (
	do = testutil.Do
)

var (
	busRef *events.Bus
)

func setup(t *testing.T) (*gin.Engine, *tenantdb.DB, *events.Bus, context.Context) {
	r, db, bus, ctx := testutil.ModuleHarness(t,
		map[string]string{"tokA": testutil.TenantA, "tokB": testutil.TenantB},
		pharmacy.New())
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

func TestMedicationsCrud(t *testing.T) {
	r, _, _, _ := setup(t)
	require.Equal(t, http.StatusCreated,
		do(r, "POST", "/v1/pharmacy/medications", "tokA", `{"name":"Paracetamol","strength":"500mg"}`).Code)
	require.Equal(t, http.StatusBadRequest,
		do(r, "POST", "/v1/pharmacy/medications", "tokA", `{}`).Code)
	require.Contains(t, do(r, "GET", "/v1/pharmacy/medications", "tokA", "").Body.String(), "Paracetamol")
	require.NotContains(t, do(r, "GET", "/v1/pharmacy/medications", "tokB", "").Body.String(), "Paracetamol")
}
