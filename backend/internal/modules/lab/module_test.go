package lab_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/modules/lab" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/authz"
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
	r, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"tokA": testutil.TenantA, "tokB": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"tokA": {lab.PermOrderRead, lab.PermOrderFulfil},
			"tokB": {lab.PermOrderRead, lab.PermOrderFulfil},
		},
		Modules: []platform.Module{lab.New()},
	})
	busRef = bus
	return r, db, bus, ctx
}

func TestVisitIntakeCreatesPendingOrder(t *testing.T) {
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

	require.Eventually(t, func() bool {
		return strings.Contains(do(r, "GET", "/v1/lab/orders", "tokA", "").Body.String(), visitID)
	}, 20*time.Second, 200*time.Millisecond)
	body := do(r, "GET", "/v1/lab/orders", "tokA", "").Body.String()
	require.Contains(t, body, `"pending"`)
	require.Contains(t, body, `"CBC"`)
	require.NotContains(t, do(r, "GET", "/v1/lab/orders", "tokB", "").Body.String(), visitID)
}

func TestResultFlow(t *testing.T) {
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

// Exactly one of N concurrent result submissions may win. The guarded
// UPDATE is what enforces it — under READ COMMITTED the losers
// re-evaluate "WHERE status = 'pending'" after the winner commits and
// match zero rows. Delete that branch and this test fails; the
// sequential flow test does not, because it never reaches the UPDATE.
func TestConcurrentResultYieldsExactlyOneWinner(t *testing.T) {
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

	const n = 8
	codes := make(chan int, n)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < n; i++ {
		go func() {
			start.Wait()
			codes <- do(r, "POST", "/v1/lab/orders/"+orderID+"/result", "tokA",
				`{"result_value":"WBC 6.1"}`).Code
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
	require.Equal(t, 1, ok, "exactly one result submission must win")
	require.Equal(t, n-1, conflict, "every loser must get 409")
	require.Zero(t, other)
}
