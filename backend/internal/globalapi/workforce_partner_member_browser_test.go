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

func assertPartnerMemberBrowser(t *testing.T, gateway http.Handler, tokens map[string]string, subject, group string, member map[string]any) {
	t.Helper()
	appURL := os.Getenv("HELIVANTA_PARTNER_MEMBER_BROWSER_URL")
	if appURL == "" {
		return
	}
	server := httptest.NewServer(gateway)
	defer server.Close()
	material, err := json.Marshal(map[string]any{"app_url": appURL, "gateway_url": server.URL, "token": tokens[subject], "tenant_id": testutil.TenantA, "subject": subject, "hospital_id": member["hospital_id"], "group_id": group, "group_name": "Synthetic independently reviewed receiving team", "member": member})
	require.NoError(t, err)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts/verify-partner-membership-owner.mjs"))
	command.Dir = root
	command.Stdin = bytes.NewReader(material)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "partner membership browser acceptance: %s", output)
	t.Log(string(output))
}
