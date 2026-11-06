package globalapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
)

// departmentBrowserAcceptance runs only when the caller supplies an already-built local shell.
func departmentBrowserAcceptance(t *testing.T, gateway http.Handler, tokens map[string]string) func(string, map[string]any, map[string]any) {
	appURL := os.Getenv("HELIVANTA_TEAM_BROWSER_URL")
	if appURL == "" {
		return nil
	}
	return func(subject string, original, replacement map[string]any) {
		t.Helper()
		server := httptest.NewServer(gateway)
		defer server.Close()
		material, err := json.Marshal(map[string]any{"app_url": appURL, "gateway_url": server.URL, "token": tokens[subject], "tenant_id": testutil.TenantA, "subject": subject, "original": original, "replacement": replacement})
		require.NoError(t, err)
		root, err := filepath.Abs("../../..")
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts/verify-department-team-owner.mjs"))
		command.Dir = root
		command.Stdin = bytes.NewReader(material)
		output, err := command.CombinedOutput()
		require.NoError(t, err, "browser owner acceptance: %s", output)
		t.Log(string(output))
	}
}
