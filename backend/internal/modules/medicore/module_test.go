package medicore_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/modules/medicore" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/tenantdb"
)

var (
	do = testutil.Do
)

func setup(t *testing.T) (*gin.Engine, *tenantdb.DB, context.Context) {
	r, db, _, ctx := testutil.ModuleHarness(t,
		map[string]string{"tokA": testutil.TenantA, "tokB": testutil.TenantB},
		map[string][]authz.Permission{
			"tokA": {medicore.PermVisitCreate, medicore.PermVisitRead},
			"tokB": {medicore.PermVisitRead},
		},
		medicore.New())
	return r, db, ctx
}

func TestCreateAndListVisits(t *testing.T) {
	r, db, ctx := setup(t)

	w := do(r, "POST", "/v1/medicore/visits", "tokA", `{"patient_name":"Asha Rao","department":"OPD"}`)
	require.Equal(t, http.StatusAccepted, w.Code)
	var resp struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.ID)

	// Tenant isolation on list.
	require.Contains(t, do(r, "GET", "/v1/medicore/visits", "tokA", "").Body.String(), "Asha Rao")
	require.NotContains(t, do(r, "GET", "/v1/medicore/visits", "tokB", "").Body.String(), "Asha Rao")

	// visit_created reached the outbox and got published (peek the
	// outbox row directly — no consumer in this module).
	require.Eventually(t, func() bool {
		var n int64
		_ = db.WithSystem(ctx, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM outbox_events
				WHERE subject = 'hms.in.medicore.visit_created.v1' AND published_at IS NOT NULL`).Scan(&n).Error
		})
		return n == 1
	}, 20*time.Second, 200*time.Millisecond)
}

func TestVisitValidation(t *testing.T) {
	r, _, _ := setup(t)
	require.Equal(t, http.StatusBadRequest, do(r, "POST", "/v1/medicore/visits", "tokA", `{}`).Code)
	require.Equal(t, http.StatusBadRequest,
		do(r, "POST", "/v1/medicore/visits", "tokA", `{"patient_name":"X","department":"ICU"}`).Code)
	require.Equal(t, http.StatusUnauthorized,
		do(r, "POST", "/v1/medicore/visits", "nope", `{"patient_name":"X","department":"OPD"}`).Code)
}
