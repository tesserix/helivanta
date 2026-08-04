package reference_test

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

	"github.com/tesserix/hms/internal/modules/reference" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/tenantdb"
)

var (
	do = testutil.Do
)

func setup(t *testing.T) (*gin.Engine, *tenantdb.DB, context.Context) {
	r, db, _, ctx := testutil.ModuleHarness(t,
		map[string]string{"tokA": testutil.TenantA, "tokB": testutil.TenantB, "tokBad": "not-a-uuid"},
		reference.New())
	return r, db, ctx
}

func TestPingFullWiring(t *testing.T) {
	r, db, ctx := setup(t)

	w := do(r, "POST", "/v1/reference/ping", "tokA", `{"message":"hello"}`)
	require.Equal(t, http.StatusAccepted, w.Code)
	var resp struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.ID)

	// Tenant A sees the ping; tenant B does not.
	require.Contains(t, do(r, "GET", "/v1/reference/pings", "tokA", "").Body.String(), "hello")
	require.NotContains(t, do(r, "GET", "/v1/reference/pings", "tokB", "").Body.String(), "hello")

	// Cross-tenant probe by id → 404, not 403 (issue #2).
	require.Equal(t, http.StatusNotFound, do(r, "GET", "/v1/reference/pings/"+resp.ID, "tokB", "").Code)
	require.Equal(t, http.StatusOK, do(r, "GET", "/v1/reference/pings/"+resp.ID, "tokA", "").Code)

	// A random (but valid) uuid that was never created also 404s.
	require.Equal(t, http.StatusNotFound, do(r, "GET", "/v1/reference/pings/"+uuid.NewString(), "tokA", "").Code)

	// Event flows outbox → JetStream → consumer → receipt row, and the
	// receipt records the same ping id that was created above.
	require.Eventually(t, func() bool {
		var n int64
		_ = db.WithSystem(ctx, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM reference_ping_receipts`).Scan(&n).Error
		})
		return n == 1
	}, 20*time.Second, 200*time.Millisecond)

	var pingID string
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT ping_id FROM reference_ping_receipts LIMIT 1`).Scan(&pingID).Error
	}))
	require.Equal(t, resp.ID, pingID)
}

func TestPingValidation(t *testing.T) {
	r, _, _ := setup(t)
	require.Equal(t, http.StatusBadRequest, do(r, "POST", "/v1/reference/ping", "tokA", `{}`).Code)
	require.Equal(t, http.StatusUnauthorized, do(r, "POST", "/v1/reference/ping", "nope", `{"message":"x"}`).Code)
}

func TestPingInvalidTenantClaim(t *testing.T) {
	r, _, _ := setup(t)
	w := do(r, "POST", "/v1/reference/ping", "tokBad", `{"message":"x"}`)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "error")
}
