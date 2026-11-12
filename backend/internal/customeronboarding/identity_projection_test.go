package customeronboarding

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
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
