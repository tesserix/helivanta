package testutil

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/containerhelpers"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

const (
	TenantA = "11111111-1111-1111-1111-111111111111"
	TenantB = "22222222-2222-2222-2222-222222222222"
)

// StaticVerifier maps bearer tokens to tenant ids for tests.
type StaticVerifier map[string]string

func (s StaticVerifier) Verify(ctx context.Context, raw string) (authn.Principal, error) {
	if tenant, ok := s[raw]; ok {
		return authn.Principal{Subject: "user-" + raw, TenantID: tenant}, nil
	}
	return authn.Principal{}, context.DeadlineExceeded
}

// ModuleHarness boots the full module stack (Postgres, NATS, routes,
// consumers, dispatcher) for the given modules. One call replaces the
// setup() previously copy-pasted per module test package.
func ModuleHarness(t *testing.T, tokens map[string]string, mods ...platform.Module) (*gin.Engine, *tenantdb.DB, *events.Bus, context.Context) {
	t.Helper()
	appDSN, adminDSN := containerhelpers.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)

	migs := events.Migrations()
	for _, m := range mods {
		migs = append(migs, m.Migrations()...)
	}
	require.NoError(t, db.Migrate(ctx, migs))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Empty(t, bad, "tenant tables must carry forced RLS")

	bus, err := events.NewBus(containerhelpers.StartNATS(t))
	require.NoError(t, err)
	t.Cleanup(bus.Close)

	deps := platform.Deps{DB: db, Bus: bus}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/v1", authn.Middleware(StaticVerifier(tokens)))
	for _, m := range mods {
		m.Routes(api, deps)
		require.NoError(t, bus.StartConsumers(ctx, db, m.Consumers(deps)))
	}
	go bus.RunDispatcher(ctx, db)
	return r, db, bus, ctx
}

// Do issues an authenticated JSON request against the harness router.
func Do(r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}
