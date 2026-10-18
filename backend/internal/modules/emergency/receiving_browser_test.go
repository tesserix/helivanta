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
	"github.com/tesserix/helivanta/internal/testutil"
)

func TestReceivingTransportBrowser(t *testing.T) {
	origin := os.Getenv("HELIVANTA_BROWSER_ORIGIN")
	parsed, err := url.Parse(origin)
	require.NoError(t, err)
	require.Contains(t, []string{"localhost", "127.0.0.1"}, parsed.Hostname())
	f := setupWithTransit(t, vault(t))
	dispatch := prepareScene(t, f)
	facility := prepareReceivingFacility(t, f)
	prepareReceivingLink(t, f, facility)
	trip := decodeBody(t, testutil.Do(f.r, "GET", f.path()+"/dispatches/"+dispatch, "supervisor", "").Body.Bytes())
	f.r.GET("/__test/receiving-seed", func(c *gin.Context) {
		c.JSON(200, gin.H{"base_id": f.base, "case_id": trip["case_id"], "facility_id": facility})
	})
	f.r.GET("/v1/iam/me/permissions", func(c *gin.Context) {
		subject, tenant := "user-supervisor", testutil.TenantA
		permissions := []string{"emergency.base.read", "emergency.case.read", "emergency.case.intake", "emergency.dispatch.manage", "emergency.case.close", "emergency.fleet.read", "emergency.fleet.manage", "emergency.fleet.check"}
		switch c.GetHeader("Authorization") {
		case "Bearer breceiver":
			subject, tenant = "user-breceiver", testutil.TenantB
			permissions = []string{"emergency.receiving.read", "emergency.receiving.act"}
		case "Bearer driver":
			subject = "user-driver"
			permissions = []string{"emergency.crew.read", "emergency.crew.act"}
		}
		c.JSON(200, gin.H{"subject": subject, "tenant_id": tenant, "data": permissions})
	})
	f.r.GET("/v1/iam/me/tenants", func(c *gin.Context) {
		tenant := testutil.TenantA
		if c.GetHeader("Authorization") == "Bearer breceiver" {
			tenant = testutil.TenantB
		}
		c.JSON(200, gin.H{"data": []gin.H{{"tenant_id": tenant, "current": true}}})
	})
	server := httptest.NewServer(f.r)
	defer server.Close()
	root, err := filepath.Abs("../../../..")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "pnpm", "exec", "playwright", "test", "--config=playwright.receiving.config.ts")
	command.Dir = filepath.Join(root, "e2e")
	command.Env = append(os.Environ(), "EMERGENCY_BROWSER_API="+server.URL)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	require.NoError(t, err)
}
