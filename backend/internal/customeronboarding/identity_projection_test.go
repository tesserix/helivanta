package customeronboarding

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

func TestIdentityProjectionUsesScopedPrivateWorkloadAndFailsClosed(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	const subject = "synthetic-staff"
	const valid = `{"purpose":"membership","tenantId":"` + tenant + `","organizationId":"helivanta-` + tenant + `","subject":"` + subject + `","state":"active","admission":"eligible"}`
	const profile = `{"purpose":"profile","tenantId":"` + tenant + `","organizationId":"helivanta-` + tenant + `","subject":"` + subject + `","state":"active","admission":"unassessed","profile":{"displayName":"Synthetic Staff","email":"synthetic@example.invalid"},"tenantName":"Yes Hospital"}`
	for _, tc := range []struct {
		name, body        string
		purpose           loginclient.IdentityReadPurpose
		status            int
		state             loginclient.UserState
		eligible, profile bool
	}{
		{name: "owned membership", body: valid, purpose: loginclient.IdentityReadMembership, state: loginclient.UserStateActive, eligible: true},
		{name: "foreign membership", body: strings.Replace(valid, `"eligible"`, `"ineligible"`, 1), purpose: loginclient.IdentityReadMembership, state: loginclient.UserStateActive},
		{name: "authoritative inactive", body: strings.ReplaceAll(strings.Replace(valid, `"eligible"`, `"ineligible"`, 1), `"active"`, `"inactive"`), purpose: loginclient.IdentityReadMembership, state: loginclient.UserStateInactive},
		{name: "status", body: strings.ReplaceAll(strings.Replace(valid, `"eligible"`, `"unassessed"`, 1), `"membership"`, `"status"`), purpose: loginclient.IdentityReadStatus, state: loginclient.UserStateActive},
		{name: "self profile", body: profile, purpose: loginclient.IdentityReadProfile, state: loginclient.UserStateActive, profile: true},
		{name: "missing tenant name", body: strings.Replace(profile, `,"tenantName":"Yes Hospital"`, "", 1), purpose: loginclient.IdentityReadProfile},
		{name: "unicode tenant name", body: strings.Replace(profile, "Yes Hospital", strings.Repeat("ଔ", 200), 1), purpose: loginclient.IdentityReadProfile, state: loginclient.UserStateActive, profile: true},
		{name: "oversized tenant name", body: strings.Replace(profile, "Yes Hospital", strings.Repeat("ଔ", 201), 1), purpose: loginclient.IdentityReadProfile},
		{name: "unknown tenant", body: `{}`, purpose: loginclient.IdentityReadMembership, status: 404},
		{name: "workload refused", body: `{"error":"synthetic@example.invalid"}`, purpose: loginclient.IdentityReadMembership, status: 403},
		{name: "outage", body: `{}`, purpose: loginclient.IdentityReadMembership, status: 503},
		{name: "wrong tenant", body: strings.ReplaceAll(valid, tenant, "22222222-2222-4222-8222-222222222222"), purpose: loginclient.IdentityReadMembership},
		{name: "wrong organization", body: strings.Replace(valid, "helivanta-"+tenant, "foreign-org", 1), purpose: loginclient.IdentityReadMembership},
		{name: "wrong subject", body: strings.Replace(valid, subject, "foreign-staff", 1), purpose: loginclient.IdentityReadMembership},
		{name: "wrong purpose", body: valid, purpose: loginclient.IdentityReadStatus},
		{name: "unknown state", body: strings.Replace(valid, `"active"`, `"future"`, 1), purpose: loginclient.IdentityReadMembership},
		{name: "invalid admission", body: strings.Replace(valid, `"eligible"`, `"unassessed"`, 1), purpose: loginclient.IdentityReadMembership},
		{name: "duplicate", body: strings.Replace(valid, `"state":`, `"state":"inactive","state":`, 1), purpose: loginclient.IdentityReadMembership},
		{name: "recased", body: strings.Replace(valid, `"state":`, `"State":`, 1), purpose: loginclient.IdentityReadMembership},
		{name: "unknown field", body: strings.TrimSuffix(valid, "}") + `,"privateEmail":"synthetic@example.invalid"}`, purpose: loginclient.IdentityReadMembership},
		{name: "profile disclosure on membership", body: strings.TrimSuffix(valid, "}") + `,"profile":{"displayName":"Synthetic Staff","email":"synthetic@example.invalid"}}`, purpose: loginclient.IdentityReadMembership},
		{name: "null", body: `null`, purpose: loginclient.IdentityReadMembership},
		{name: "truncated", body: `{`, purpose: loginclient.IdentityReadMembership},
		{name: "second document", body: valid + `{}`, purpose: loginclient.IdentityReadMembership},
		{name: "oversized", body: valid + strings.Repeat(" ", 8193), purpose: loginclient.IdentityReadMembership},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/internal/v1/customer-signups/identity-projection" || r.Header.Get("Authorization") != "Bearer synthetic-workload" {
					t.Error("escaped workload boundary")
				}
				var input map[string]any
				wantFields := 4
				if tc.purpose == loginclient.IdentityReadProfile {
					wantFields = 5
				}
				if json.NewDecoder(r.Body).Decode(&input) != nil || len(input) != wantFields || input["tenantId"] != tenant || input["subject"] != subject || input["requestId"] == "" {
					t.Error("incorrect private identity scope")
				}
				if (input["includeTenantName"] == true) != (tc.purpose == loginclient.IdentityReadProfile) {
					t.Error("tenant name must be requested only for an explicit profile read")
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer provider.Close()
			token := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(token, []byte("synthetic-workload"), 0600); err != nil {
				t.Fatal(err)
			}
			h := &Handlers{base: provider.URL, tokenFile: token, client: provider.Client()}
			got, err := h.TenantIdentitySnapshot(t.Context(), tenant, subject, tc.purpose)
			if tc.state == 0 {
				if !errors.Is(err, loginclient.ErrUnavailable) || got.State != 0 || got.Profile != nil || got.MembershipEligible {
					t.Fatalf("unsafe response accepted: %#v %v", got, err)
				}
				if strings.Contains(err.Error(), "synthetic@example.invalid") {
					t.Fatal("private data exposed in error")
				}
			} else if err != nil || got.State != tc.state || got.MembershipEligible != tc.eligible || (got.Profile != nil) != tc.profile {
				t.Fatalf("wrong projection: %#v %v", got, err)
			}
			if tc.profile && got.Profile != nil && got.Profile.TenantName == "" {
				t.Fatal("verified tenant name was lost")
			}
		})
	}
}

type assignmentIdentityTransport func(*http.Request) (*http.Response, error)

func (f assignmentIdentityTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAssignmentCandidatePurposeUsesSeparateMinimalPrivateProjection(t *testing.T) {
	const testTenant = "11111111-1111-4111-8111-111111111111"
	token := filepath.Join(t.TempDir(), "workload-token")
	require.NoError(t, os.WriteFile(token, []byte("synthetic-workload"), 0600))
	called := false
	owner := &Handlers{base: "https://owner.example.invalid", tokenFile: token, client: &http.Client{Transport: assignmentIdentityTransport(func(r *http.Request) (*http.Response, error) {
		called = true
		var in customercontract.IdentityProjectionInput
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		require.Equal(t, "assignment_candidate", in.Purpose)
		require.False(t, in.IncludeTenantName)
		raw := `{"purpose":"assignment_candidate","tenantId":"` + testTenant + `","organizationId":"helivanta-` + testTenant + `","subject":"synthetic-user","state":"active","admission":"unassessed","candidate":{"displayName":"Synthetic Staff","email":"synthetic@example.invalid"}}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}, nil
	})}}
	snapshot, err := owner.TenantIdentitySnapshot(t.Context(), testTenant, "synthetic-user", loginclient.IdentityReadAssignmentCandidate)
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, loginclient.UserStateActive, snapshot.State)
	require.Nil(t, snapshot.Profile)
	require.False(t, snapshot.MembershipEligible)
}

func TestAssignmentCandidatePurposeRejectsMisboundOrMalformedProjection(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	const valid = `{"purpose":"assignment_candidate","tenantId":"` + tenant + `","organizationId":"helivanta-` + tenant + `","subject":"synthetic-user","state":"active","admission":"unassessed","candidate":{"displayName":"Synthetic Staff","email":"synthetic@example.invalid"}}`
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{name: "wrong subject", body: strings.Replace(valid, "synthetic-user", "another-user", 1)},
		{name: "wrong tenant", body: strings.ReplaceAll(valid, tenant, "22222222-2222-4222-8222-222222222222")},
		{name: "foreign organization", body: strings.Replace(valid, "helivanta-"+tenant, "another-organization", 1)},
		{name: "grant authority", body: strings.Replace(valid, "unassessed", "eligible", 1)},
		{name: "inactive disclosure", body: strings.Replace(valid, `"active"`, `"inactive"`, 1)},
		{name: "self-profile purpose", body: strings.Replace(valid, "assignment_candidate", "profile", 1)},
		{name: "recased candidate", body: strings.Replace(valid, `"candidate":`, `"Candidate":`, 1)},
		{name: "duplicate name", body: strings.Replace(valid, `"displayName":`, `"displayName":"Other","displayName":`, 1)},
		{name: "missing address", body: strings.Replace(valid, `,"email":"synthetic@example.invalid"`, "", 1)},
		{name: "provider refusal", body: `{}`, status: 403},
		{name: "provider unavailable", body: `{}`, status: 503},
		{name: "bounded projection", body: valid + strings.Repeat(" ", 8193)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := filepath.Join(t.TempDir(), "workload-token")
			require.NoError(t, os.WriteFile(token, []byte("synthetic-workload"), 0600))
			owner := &Handlers{base: "https://owner.example.invalid", tokenFile: token, client: &http.Client{Transport: assignmentIdentityTransport(func(r *http.Request) (*http.Response, error) {
				status := tc.status
				if status == 0 {
					status = 200
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}}
			result, err := owner.TenantIdentitySnapshot(t.Context(), tenant, "synthetic-user", loginclient.IdentityReadAssignmentCandidate)
			require.ErrorIs(t, err, loginclient.ErrUnavailable)
			require.NotContains(t, err.Error(), "synthetic@example.invalid")
			require.Nil(t, result.Candidate)
			require.Nil(t, result.Profile)
		})
	}
}
