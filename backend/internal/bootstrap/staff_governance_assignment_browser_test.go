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

func exerciseNativeAssignmentBrowser(t *testing.T, owner http.Handler, hospital, staff uuid.UUID, subject, name, email string) {
	t.Helper()
	appURL := os.Getenv("HELIVANTA_OPERATING_BROWSER_URL")
	if appURL == "" {
		return
	}
	backend := httptest.NewServer(owner)
	defer backend.Close()
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": backend.URL, "workforce": backend.URL, "organization": backend.URL}})
	require.NoError(t, err)
	server := httptest.NewServer(gateway)
	defer server.Close()
	material, err := json.Marshal(map[string]any{"app_url": appURL, "gateway_url": server.URL, "tenant_id": testutil.TenantA, "hospital": hospital, "staff": staff, "subject": subject, "name": name, "email": email})
	require.NoError(t, err)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts/verify-native-assignment-owner.mjs"))
	command.Dir = root
	command.Stdin = bytes.NewReader(material)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "native hospital assignment browser acceptance: %s", output)
	t.Log(string(output))
}
