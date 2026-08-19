package archtest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/bootstrap"
	"github.com/tesserix/helivanta/internal/modules/medicore"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// seamTenant is the one tenant this test reconciles. Distinct from the
// matrix suite's tenantA/tenantB so the two test binaries never collide
// on OpenFGA store contents if ever run against a shared store.
const seamTenant = "33333333-3333-3333-3333-333333333333"

// iamMembersSeamMigration mirrors iam_members' shape (tenant_id, subject,
// role_key) without importing the iam module — platform.Reconcile reads
// this table with a raw SQL query rather than a Go model (see
// platform/reconcile.go), and platform cannot import iam (iam imports
// platform). This is the same fixture internal/platform's own reconcile
// tests use, redefined here because it is unexported there.
var iamMembersSeamMigration = tenantdb.Migration{
	ID: "0001_seam_iam_members",
	SQL: `
		CREATE TABLE iam_members (
		  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
		  tenant_id uuid NOT NULL,
		  subject text NOT NULL,
		  role_key text NOT NULL,
		  created_at timestamptz NOT NULL DEFAULT now()
		);
		ALTER TABLE iam_members ENABLE ROW LEVEL SECURITY;
		ALTER TABLE iam_members FORCE ROW LEVEL SECURITY;
		CREATE POLICY tenant_isolation ON iam_members
		  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
		  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
		-- BYPASSRLS confers no table privileges, and the harness grants the
		-- system role none, so every fixture that a cross-tenant reader will
		-- touch must grant it explicitly — exactly as the real migrations do
		-- (#894). A fixture that omits this fails with "permission denied",
		-- which is the same error production would give.
		GRANT SELECT ON iam_members TO helivanta_system;`,
}

// seamVerifier is a minimal authn.TokenVerifier: the raw bearer token IS
// the subject, always resolved into seamTenant. It stands in only for
// the identity provider (which token verification is real IdP's job, not
// authorization's), unlike the resolver this test replaces with a real
// authz.Client — the seam under test is authn -> authz -> platform.Router
// -> handler, not token verification.
type seamVerifier struct{}

func (seamVerifier) Verify(_ context.Context, raw string) (authn.Principal, error) {
	// IdleDeadline an hour out (#848): authn.Middleware refuses a zero
	// deadline exactly like an already-past one (fail closed), so
	// leaving it unset would 401 every request here and this seam test
	// would stop exercising the seam it is about. The idle gate itself
	// is pinned by pkg/authn's own tests.
	return authn.Principal{Subject: raw, TenantID: seamTenant, AuthTime: time.Now(), IdleDeadline: time.Now().Add(time.Hour)}, nil
}

// seamNeverRevoked stands in for the real iam.RevocationChecker: this
// seam is about authn -> authz -> platform.Router -> handler, not
// credential revocation, which pkg/authn and internal/modules/iam test
// directly.
type seamNeverRevoked struct{}

func (seamNeverRevoked) RevokedAfter(context.Context, string) (time.Time, error) {
	return time.Time{}, nil
}

// newSeamHarness boots a real Postgres and a real OpenFGA, reconciles
// seamTenant with the real (Task 5) authoritative reconciler, seeds one
// nurse member, and mounts medicore's real routes behind real authn and
// authz middleware. No NATS/events.Bus: the only route this test drives
// to success (GET /medicore/visits) never touches deps.Bus, and POST is
// exercised only far enough to hit the 403 the nurse role does not clear,
// so standing up a broker would not test anything this seam needs.
func newSeamHarness(t *testing.T) *gin.Engine {
	t.Helper()
	ctx := context.Background()

	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)

	mod := medicore.New()
	// Platform migrations must run first: medicore's policy now depends
	// on hms_tenant_visible (Task 5/6), which only bootstrap's migration
	// defines.
	migs := append(bootstrap.PlatformMigrations(), iamMembersSeamMigration)
	migs = append(migs, mod.Migrations()...)
	require.NoError(t, db.Migrate(ctx, migs))

	require.NoError(t, db.WithTenant(ctx, seamTenant, func(tx *gorm.DB) error {
		return tx.Exec(
			`INSERT INTO iam_members (tenant_id, subject, role_key) VALUES (?, 'nurse-amy', 'nurse')`,
			seamTenant,
		).Error
	}))

	fga, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), t.Name())
	require.NoError(t, err)

	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(mod))

	// The real Task 5 reconciler: reads iam_members, writes the role and
	// permission tuples it implies, and prunes anything OpenFGA holds
	// that Postgres does not back. This is what produces the granted
	// tuples the assertions below depend on, not a fake resolver.
	require.NoError(t, platform.Reconcile(ctx, reg, db, fga))

	gin.SetMode(gin.TestMode)
	e := gin.New()
	deps := platform.Deps{DB: db, Authz: fga, Roles: fga}
	// fga doubles as the MembershipChecker here, not a fake: the real
	// Task 5 reconciler above wrote the tenant->role edge nurse-amy's
	// role backs (internal/platform/reconcile.go's applyGrants), so this
	// exercises the real membership Check the same way production does,
	// not a stand-in for it.
	api := platform.NewRouter(e.Group("/v1",
		authn.Middleware(seamVerifier{}, seamNeverRevoked{}),
		authz.Middleware(fga),
	), fga)
	mod.Routes(api, deps)
	return e
}

func seamRequest(e *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	e.ServeHTTP(w, req)
	return w
}

// TestHTTPAuthzSeam drives one real request through the full chain —
// authn.Middleware -> authz.Middleware -> platform.Router -> authz.Require
// -> handler — with a real OpenFGA populated by the real reconciler,
// rather than through authz.Client.Resolve directly (the matrix suite) or
// a fake Resolver substituted for the middleware (every module test via
// testutil.ModuleHarness). Those two styles never together prove the
// middleware is even mounted; this test does, and the discrimination
// check recorded in task-7-report.md demonstrates it by unmounting
// authz.Middleware and watching this test fail.
//
// One route per role is enough (see the task brief): nurse holds
// medicore.visit.read but not medicore.visit.create, so GET must succeed
// and POST must be denied.
func TestHTTPAuthzSeam(t *testing.T) {
	e := newSeamHarness(t)

	getResp := seamRequest(e, http.MethodGet, "/v1/medicore/visits", "nurse-amy")
	require.Equal(t, http.StatusOK, getResp.Code, "nurse holds medicore.visit.read: %s", getResp.Body.String())

	postResp := seamRequest(e, http.MethodPost, "/v1/medicore/visits", "nurse-amy")
	require.Equal(t, http.StatusForbidden, postResp.Code, "nurse does not hold medicore.visit.create: %s", postResp.Body.String())
}
