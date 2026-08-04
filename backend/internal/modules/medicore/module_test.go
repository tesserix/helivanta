package medicore_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/modules/medicore"
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

func setup(t *testing.T) (*gin.Engine, *tenantdb.DB, context.Context) {
	appDSN, adminDSN := testutil.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)

	mod := medicore.New()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	migs := append(events.Migrations(), mod.Migrations()...)
	require.NoError(t, db.Migrate(ctx, migs))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Empty(t, bad, "medicore tables must carry forced RLS")

	bus, err := events.NewBus(testutil.StartNATS(t))
	require.NoError(t, err)
	t.Cleanup(bus.Close)

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
