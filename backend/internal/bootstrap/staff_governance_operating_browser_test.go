package bootstrap_test

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

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/internal/testutil"
)

func exerciseNativeOperatingBrowser(t *testing.T, owner http.Handler, kind string, employer, staff, location uuid.UUID) {
	t.Helper()
	appURL := os.Getenv("HELIVANTA_OPERATING_BROWSER_URL")
	if appURL == "" {
		return
	}
	backend := httptest.NewServer(owner)
	defer backend.Close()
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": backend.URL, "workforce": backend.URL, "pharmacy": backend.URL, "supply": backend.URL}})
	require.NoError(t, err)
	server := httptest.NewServer(gateway)
	defer server.Close()
	material, err := json.Marshal(map[string]any{"app_url": appURL, "gateway_url": server.URL, "tenant_id": testutil.TenantA, "kind": kind, "employer": employer, "staff": staff, "location": location})
	require.NoError(t, err)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts/verify-native-operating-owner.mjs"))
	command.Dir = root
	command.Stdin = bytes.NewReader(material)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "native operating browser acceptance: %s", output)
	t.Log(string(output))
}
