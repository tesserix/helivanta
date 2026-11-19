package customeronboarding

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

func TestCredentialProjectionRequiresAccountRecoveryEvidence(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	const generation = "019f39c2-1738-7998-b5de-6d4214f614e0"
	const base = `{"purpose":"status","tenantId":"` + tenant + `","organizationId":"helivanta-` + tenant + `","subject":"synthetic-user","state":"active","admission":"unassessed","credentials":{"passwordConfigured":false}`
	for _, tc := range []struct{ name, requirement, state string }{
		{"none", `{"state":"none"}`, "none"},
		{"blocked", `{"state":"blocked","generationId":"` + generation + `"}`, "blocked"},
		{"enrollment", `{"state":"enrollment_required","generationId":"` + generation + `"}`, "enrollment_required"},
		{"completed", `{"state":"completed","generationId":"` + generation + `"}`, "completed"},
		{"missing", "", ""},
		{"null", "null", ""},
		{"missing_generation", `{"state":"completed"}`, ""},
		{"wrong_generation", `{"state":"completed","generationId":"11111111-1111-4111-8111-111111111111"}`, ""},
		{"unknown_state", `{"state":"success"}`, ""},
		{"private_source", `{"state":"none","reason":"private source"}`, ""},
		{"duplicate", `{"state":"none","state":"completed"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := filepath.Join(t.TempDir(), "workload-token")
			require.NoError(t, os.WriteFile(token, []byte("synthetic-workload"), 0600))
			owner := &Handlers{base: "https://owner.example.invalid", tokenFile: token, client: &http.Client{Transport: assignmentIdentityTransport(func(r *http.Request) (*http.Response, error) {
				var input customercontract.IdentityProjectionInput
				require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
				require.True(t, input.IncludeMFARecovery)
				require.True(t, input.IncludeCredentialState)
				require.Equal(t, "status", input.Purpose)
				body := base
				if tc.requirement != "" {
					body += `,"mfaRecovery":` + tc.requirement
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body + "}"))}, nil
			})}}
			out, err := owner.TenantIdentitySnapshot(t.Context(), tenant, "synthetic-user", loginclient.IdentityReadCredentials)
			if tc.state == "" {
				require.ErrorIs(t, err, loginclient.ErrUnavailable)
				require.Nil(t, out.MFARecovery)
				require.Zero(t, out.State)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, out.MFARecovery)
			require.Equal(t, tc.state, out.MFARecovery.State)
			if tc.state != "none" {
				require.Equal(t, generation, out.MFARecovery.GenerationID)
			}
		})
	}
}
