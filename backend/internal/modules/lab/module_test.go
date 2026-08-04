package lab_test

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

	"github.com/tesserix/hms/internal/modules/lab" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
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

	mod := lab.New()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	migs := append(events.Migrations(), mod.Migrations()...)
	require.NoError(t, db.Migrate(ctx, migs))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Empty(t, bad, "lab tables must carry forced RLS")

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

func TestVisitIntakeCreatesPendingOrder(t *testing.T) {
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

	require.Eventually(t, func() bool {
		return strings.Contains(do(r, "GET", "/v1/lab/orders", "tokA", "").Body.String(), visitID)
	}, 20*time.Second, 200*time.Millisecond)
	body := do(r, "GET", "/v1/lab/orders", "tokA", "").Body.String()
	require.Contains(t, body, `"pending"`)
	require.Contains(t, body, `"CBC"`)
	require.NotContains(t, do(r, "GET", "/v1/lab/orders", "tokB", "").Body.String(), visitID)
}

func TestResultFlow(t *testing.T) {
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

	var orderID string
	require.Eventually(t, func() bool {
		var resp struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		_ = json.Unmarshal(do(r, "GET", "/v1/lab/orders", "tokA", "").Body.Bytes(), &resp)
		if len(resp.Data) == 0 {
			return false
		}
		orderID = resp.Data[0].ID
		return true
	}, 20*time.Second, 200*time.Millisecond)

	require.Equal(t, http.StatusNotFound,
		do(r, "POST", "/v1/lab/orders/"+orderID+"/result", "tokB", `{"result_value":"WBC 6.1"}`).Code)
	require.Equal(t, http.StatusBadRequest,
		do(r, "POST", "/v1/lab/orders/"+orderID+"/result", "tokA", `{}`).Code)

	require.Equal(t, http.StatusOK,
		do(r, "POST", "/v1/lab/orders/"+orderID+"/result", "tokA", `{"result_value":"WBC 6.1"}`).Code)
	body := do(r, "GET", "/v1/lab/orders", "tokA", "").Body.String()
	require.Contains(t, body, `"completed"`)
	require.Contains(t, body, "WBC 6.1")

	require.Equal(t, http.StatusConflict,
		do(r, "POST", "/v1/lab/orders/"+orderID+"/result", "tokA", `{"result_value":"again"}`).Code)

	require.Eventually(t, func() bool {
		var n int64
		_ = db.WithSystem(ctx, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM outbox_events
				WHERE subject = 'hms.in.lab.result_ready.v1'`).Scan(&n).Error
		})
		return n == 1
	}, 10*time.Second, 200*time.Millisecond)
}
