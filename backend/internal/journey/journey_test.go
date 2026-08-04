// Package journey holds the phase 2 cross-module integration test:
// medicore visit_created fans out to pharmacy AND lab via JetStream.
package journey

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

	"github.com/tesserix/hms/internal/modules/lab"
	"github.com/tesserix/hms/internal/modules/medicore"
	"github.com/tesserix/hms/internal/modules/pharmacy"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
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

func do(r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestVisitFansOutToPharmacyAndLab(t *testing.T) {
	appDSN, adminDSN := testutil.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)

	mods := []platform.Module{medicore.New(), pharmacy.New(), lab.New()}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)

	migs := events.Migrations()
	for _, m := range mods {
		migs = append(migs, m.Migrations()...)
	}
	require.NoError(t, db.Migrate(ctx, migs))

	bus, err := events.NewBus(testutil.StartNATS(t))
	require.NoError(t, err)
	t.Cleanup(bus.Close)

	deps := platform.Deps{DB: db, Bus: bus}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/v1", authn.Middleware(staticVerifier{"tokA": tenantA, "tokB": tenantB}))
	for _, m := range mods {
		m.Routes(api, deps)
		require.NoError(t, bus.StartConsumers(ctx, db, m.Consumers(deps)))
	}
	go bus.RunDispatcher(ctx, db)

	// Create a visit as tenant A.
	w := do(r, "POST", "/v1/medicore/visits", "tokA", `{"patient_name":"Asha Rao","department":"OPD"}`)
	require.Equal(t, http.StatusAccepted, w.Code)
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	// Both downstream modules open pending work for tenant A.
	require.Eventually(t, func() bool {
		return strings.Contains(do(r, "GET", "/v1/pharmacy/dispenses", "tokA", "").Body.String(), created.ID)
	}, 30*time.Second, 200*time.Millisecond, "pharmacy should open a pending dispense")
	require.Eventually(t, func() bool {
		return strings.Contains(do(r, "GET", "/v1/lab/orders", "tokA", "").Body.String(), created.ID)
	}, 30*time.Second, 200*time.Millisecond, "lab should open a pending order")

	// Tenant B sees none of it.
	require.NotContains(t, do(r, "GET", "/v1/pharmacy/dispenses", "tokB", "").Body.String(), created.ID)
	require.NotContains(t, do(r, "GET", "/v1/lab/orders", "tokB", "").Body.String(), created.ID)
}
