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
)

func TestEmergencyConsoleBrowser(t *testing.T) {
	origin := os.Getenv("HELIVANTA_BROWSER_ORIGIN")
	parsed, err := url.Parse(origin)
	require.NoError(t, err)
	require.Contains(t, []string{"localhost", "127.0.0.1"}, parsed.Hostname(), "use an already running local shell, never a production URL")
	f := setupWithTransit(t, vault(t))
	f.r.GET("/v1/iam/me/permissions", func(c *gin.Context) {
		c.JSON(200, gin.H{"subject": "user-admin", "tenant_id": "11111111-1111-1111-1111-111111111111", "data": []string{"emergency.base.read", "emergency.base.manage", "emergency.case.read", "emergency.case.intake", "emergency.case.close", "emergency.fleet.read", "emergency.fleet.manage", "emergency.fleet.check"}})
	})
	server := httptest.NewServer(f.r)
	defer server.Close()
	root, err := filepath.Abs("../../../..")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "pnpm", "exec", "playwright", "test", "--config=playwright.emergency.config.ts")
	command.Dir = filepath.Join(root, "e2e")
	command.Env = append(os.Environ(), "EMERGENCY_BROWSER_API="+server.URL)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	require.NoError(t, err)
}
