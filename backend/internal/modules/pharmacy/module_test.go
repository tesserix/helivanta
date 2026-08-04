package pharmacy_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/modules/pharmacy"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
	"gorm.io/gorm"
)

type staticVerifier map[string]string

func (s staticVerifier) Verify(ctx context.Context, raw string) (authn.Principal, error) {
	if t, ok := s[raw]; ok {
		return authn.Principal{Subject: "user-" + raw, TenantID: t}, nil
	}
	return authn.Principal{}, context.DeadlineExceeded
}

const (
	tenantA = "11111111-1111-1111-1111-111111111111"
	tenantB = "22222222-2222-2222-2222-222222222222"
)

var busRef *events.Bus

func setup(t *testing.T) (*gin.Engine, *tenantdb.DB, context.Context) {
	appDSN, adminDSN := testutil.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)

	mod := pharmacy.New()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	migs := append(events.Migrations(), mod.Migrations()...)
	require.NoError(t, db.Migrate(ctx, migs))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Empty(t, bad, "pharmacy tables must carry forced RLS")

	bus, err := events.NewBus(testutil.StartNATS(t))
	require.NoError(t, err)
	t.Cleanup(bus.Close)
	busRef = bus

	deps := platform.Deps{DB: db, Bus: bus}
	require.NoError(t, bus.StartConsumers(ctx, db, mod.Consumers(deps)))
	go bus.RunDispatcher(ctx, db)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/v1", authn.Middleware(staticVerifier{"tokA": tenantA, "tokB": tenantB}))
	mod.Routes(api, deps)
	return r, db, ctx
}

func do(r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestVisitIntakeCreatesPendingDispense(t *testing.T) {
	r, db, ctx := setup(t)

	// Simulate medicore publishing visit_created (module boundary: we
	// publish the envelope, not import medicore).
	visitID := uuid.NewString()
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		data, _ := json.Marshal(map[string]string{
			"visit_id": visitID, "patient_name": "Asha Rao", "department": "OPD",
		})
		return busRef.Publish(tx, "hms.in.medicore.visit_created.v1", events.Event{
			Type: "VisitCreated", Version: 1, TenantID: tenantA, Data: data,
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
	r, db, ctx := setup(t)

	visitID := uuid.NewString()
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		data, _ := json.Marshal(map[string]string{
			"visit_id": visitID, "patient_name": "Asha Rao", "department": "OPD",
		})
		return busRef.Publish(tx, "hms.in.medicore.visit_created.v1", events.Event{
			Type: "VisitCreated", Version: 1, TenantID: tenantA, Data: data,
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
	r, _, _ := setup(t)
	require.Equal(t, http.StatusCreated,
		do(r, "POST", "/v1/pharmacy/medications", "tokA", `{"name":"Paracetamol","strength":"500mg"}`).Code)
	require.Equal(t, http.StatusBadRequest,
		do(r, "POST", "/v1/pharmacy/medications", "tokA", `{}`).Code)
	require.Contains(t, do(r, "GET", "/v1/pharmacy/medications", "tokA", "").Body.String(), "Paracetamol")
	require.NotContains(t, do(r, "GET", "/v1/pharmacy/medications", "tokB", "").Body.String(), "Paracetamol")
}
