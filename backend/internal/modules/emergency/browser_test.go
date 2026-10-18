//go:build emergencybrowser

package emergency_test

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/internal/testutil"
)

func TestEmergencyConsoleBrowser(t *testing.T) {
	origin := os.Getenv("HELIVANTA_BROWSER_ORIGIN")
	parsed, err := url.Parse(origin)
	require.NoError(t, err)
	require.Contains(t, []string{"localhost", "127.0.0.1"}, parsed.Hostname(), "use an already running local shell, never a production URL")
	f := setupWithTransit(t, vault(t))
	caseID, assessment, vehicle, duty := prepareDispatch(t, f)
	dispatchID := newID()
	require.Equal(t, 202, testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+dispatchID, "dispatcher", dispatchBody(caseID, assessment, vehicle, duty)).Code)
	_, err = dispatchProcessor(t, f).Offer(t.Context(), emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: f.base, DispatchID: dispatchID})
	require.NoError(t, err)
	f.r.GET("/__test/crew-seed", func(c *gin.Context) { c.JSON(200, gin.H{"base_id": f.base, "dispatch_id": dispatchID}) })
	f.r.GET("/v1/iam/me/permissions", func(c *gin.Context) {
		if actor := c.GetHeader("Authorization"); actor == "Bearer driver" || actor == "Bearer medic" {
			c.JSON(200, gin.H{"subject": "user-" + actor[len("Bearer "):], "tenant_id": testutil.TenantA, "data": []string{"emergency.crew.read", "emergency.crew.act"}})
			return
		}
		if c.GetHeader("Authorization") == "Bearer doctor" {
			c.JSON(200, gin.H{"subject": "user-doctor", "tenant_id": "11111111-1111-1111-1111-111111111111", "data": []string{"emergency.base.read", "emergency.case.read", "emergency.case.assess"}})
			return
		}
		c.JSON(200, gin.H{"subject": "user-admin", "tenant_id": "11111111-1111-1111-1111-111111111111", "data": []string{"emergency.base.read", "emergency.base.manage", "emergency.case.read", "emergency.case.intake", "emergency.case.close", "emergency.fleet.read", "emergency.fleet.manage", "emergency.fleet.check", "emergency.clinical.manage"}})
	})
	server := httptest.NewServer(f.r)
	defer server.Close()
	root, err := filepath.Abs("../../../..")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "pnpm", "exec", "playwright", "test", "--config=playwright.emergency.config.ts")
	command.Dir = filepath.Join(root, "e2e")
	command.Env = append(os.Environ(), "EMERGENCY_BROWSER_API="+server.URL)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	require.NoError(t, err)
}
