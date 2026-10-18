package emergency_test

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/emergency" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/helivanta/pkg/transit"
	"testing"
)

type fixture struct {
	r    *gin.Engine
	db   *tenantdb.DB
	base string
}

func setup(t *testing.T) *fixture { return setupWithTransit(t, nil) }
func setupWithTransit(t *testing.T, v *transit.Client) *fixture {
	return setupWithSubjectAliases(t, v, nil)
}
func setupWithSubjectAliases(t *testing.T, v *transit.Client, aliases map[string]string, entry ...bool) *fixture {
	t.Helper()
	all := []authz.Permission{"emergency.receiving.manage", "emergency.receiving.read", "emergency.receiving.act", "emergency.crew.read", "emergency.crew.act", "emergency.base.manage", "emergency.base.read", "emergency.case.read", "emergency.case.intake", "emergency.dispatch.manage", "emergency.case.close", "emergency.fleet.read", "emergency.fleet.manage", "emergency.fleet.check", "emergency.clinical.manage", "emergency.case.assess"}
	tokens := map[string]string{"admin": testutil.TenantA, "verifier": testutil.TenantA, "dispatcher": testutil.TenantA, "supervisor": testutil.TenantA, "reader": testutil.TenantA, "doctor": testutil.TenantA, "driver": testutil.TenantA, "medic": testutil.TenantA, "unassigned": testutil.TenantA, "other": testutil.TenantB, "badmin": testutil.TenantB, "bdispatcher": testutil.TenantB, "bsupervisor": testutil.TenantB, "bdoctor": testutil.TenantB, "bdriver": testutil.TenantB, "bmedic": testutil.TenantB, "bmanager": testutil.TenantB, "breceiver": testutil.TenantB}
	perms := map[string][]authz.Permission{
		"admin": all, "verifier": all, "other": all, "badmin": all,
		"bmanager": {"emergency.receiving.read", "emergency.receiving.act"}, "breceiver": {"emergency.receiving.read", "emergency.receiving.act"},
		"dispatcher": {"emergency.base.read", "emergency.case.read", "emergency.case.intake", "emergency.dispatch.manage", "emergency.fleet.read", "emergency.fleet.check"},
		"supervisor": {"emergency.base.read", "emergency.case.read", "emergency.case.intake", "emergency.dispatch.manage", "emergency.case.close", "emergency.fleet.read", "emergency.fleet.manage", "emergency.fleet.check"},
		"reader":     {"emergency.base.read", "emergency.case.read", "emergency.fleet.read"},
		"doctor":     {"emergency.base.read", "emergency.case.read", "emergency.case.assess"},
		"driver":     {"emergency.crew.read", "emergency.crew.act"}, "medic": {"emergency.crew.read", "emergency.crew.act"}, "unassigned": {"emergency.crew.read", "emergency.crew.act"},
	}
	for _, name := range []string{"dispatcher", "supervisor", "doctor", "driver", "medic"} {
		perms["b"+name] = perms[name]
	}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{EmergencyDispatchEnabled: len(entry) == 0 || entry[0], Transit: v, Tokens: tokens, Perms: perms, Verifier: subjectAliasVerifier{tokens: tokens, aliases: aliases}, Modules: []platform.Module{emergency.New()}})
	return &fixture{r: r, db: db, base: uuid.Must(uuid.NewV7()).String()}
}
func (f *fixture) path() string { return "/v1/emergency/bases/" + f.base }
func baseBody() string {
	return `{"name":"Synthetic Odisha Base","timezone":"Asia/Kolkata","policy_reference":"SYN-IN-EMERGENCY-2026-01","contact_retention_days":30}`
}
func decodeBody(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var v map[string]any
	require.NoError(t, json.Unmarshal(b, &v))
	return v
}
func TestBaseSetupDoesNotGrantCaseAccess(t *testing.T) {
	f := setup(t)
	w := testutil.Do(f.r, "PUT", f.path(), "admin", baseBody())
	require.Equal(t, 200, w.Code, w.Body.String())
	row := decodeBody(t, w.Body.Bytes())
	require.Equal(t, "Synthetic Odisha Base", row["name"])
	require.Equal(t, "Asia/Kolkata", row["timezone"])
	w = testutil.Do(f.r, "GET", f.path()+"/cases/"+uuid.Must(uuid.NewV7()).String(), "admin", "")
	require.Equal(t, 404, w.Code, "tenant administrator must not inherit clinical scope")
	w = testutil.Do(f.r, "PUT", f.path()+"/access/user-dispatcher", "admin", `{"level":"dispatcher"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", f.path()+"/access/user-reader", "dispatcher", `{"level":"supervisor"}`)
	require.Equal(t, 403, w.Code, "dispatcher cannot grant scope")
}

func TestCurrentBaseAccessDoesNotInheritAdministratorScope(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	for _, who := range []string{"admin", "other"} {
		w := testutil.Do(f.r, "GET", f.path()+"/access", who, "")
		require.Equal(t, 404, w.Code, w.Body.String())
	}
	w := testutil.Do(f.r, "GET", f.path()+"/access", "supervisor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "supervisor", decodeBody(t, w.Body.Bytes())["level"])
}

type subjectAliasVerifier struct {
	tokens  testutil.StaticVerifier
	aliases map[string]string
}

func (v subjectAliasVerifier) Verify(ctx context.Context, token string) (authn.Principal, error) {
	p, err := v.tokens.Verify(ctx, token)
	if err == nil {
		if subject, ok := v.aliases[token]; ok {
			p.Subject = subject
		}
	}
	return p, err
}
