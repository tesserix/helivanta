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

func TestTenantLoginLookupUsesPrivateWorkloadAndRejectsForeignBinding(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	for _, tc := range []struct {
		name, body string
		status     int
		want       error
	}{
		{"own", `{"tenantId":"` + tenant + `","organizationId":"helivanta-` + tenant + `","subject":"staff"}`, 200, nil},
		{"foreign", `{"tenantId":"22222222-2222-4222-8222-222222222222","organizationId":"helivanta-22222222-2222-4222-8222-222222222222","subject":"staff"}`, 200, loginclient.ErrUnavailable},
		{"oversized", `{"tenantId":"` + tenant + `","organizationId":"helivanta-` + tenant + `","subject":"staff"}` + strings.Repeat(" ", 9000), 200, loginclient.ErrUnavailable},
		{"unknown", `{}`, 404, loginclient.ErrUserNotFound}, {"outage", `{}`, 503, loginclient.ErrUnavailable}, {"wrong workload", `{}`, 401, loginclient.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/internal/v1/customer-signups/login-target" || r.Header.Get("Authorization") != "Bearer synthetic-workload" {
					t.Error("lookup escaped private workload boundary")
				}
				var body map[string]string
				if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 3 || body["tenantId"] != tenant || body["email"] != "same@example.test" || body["requestId"] == "" {
					t.Error("lookup sent an incorrect target or credential")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer native.Close()
			tokenFile := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(tokenFile, []byte("synthetic-workload"), 0600); err != nil {
				t.Fatal(err)
			}
			h := &Handlers{base: native.URL, tokenFile: tokenFile, client: native.Client()}
			subject, err := h.TenantLoginSubject(t.Context(), "helivanta-"+tenant, "same@example.test")
			if tc.want != nil {
				if !errors.Is(err, tc.want) || subject != "" {
					t.Fatalf("unsafe lookup accepted: %v", err)
				}
			} else if err != nil || subject != "staff" {
				t.Fatalf("own account refused: %v", err)
			}
		})
	}
}

func TestTenantLoginTargetsUsesPrivateWorkloadAndRejectsAnythingOffContract(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	own := `{"tenantId":"` + tenant + `","organizationId":"helivanta-` + tenant + `","subject":"staff","name":"City Hospital","account":"same@example.test"}`
	for _, tc := range []struct {
		name, body string
		status     int
		targets    int
		truncated  bool
		want       error
	}{
		{"one", `{"targets":[` + own + `],"truncated":false}`, 200, 1, false, nil},
		{"none", `{"targets":[],"truncated":false}`, 200, 0, false, nil},
		{"truncated", `{"targets":[],"truncated":true}`, 200, 0, true, nil},
		{"truncated with bindings", `{"targets":[` + own + `],"truncated":true}`, 200, 0, false, loginclient.ErrUnavailable},
		{"foreign binding", `{"targets":[{"tenantId":"` + tenant + `","organizationId":"helivanta-22222222-2222-4222-8222-222222222222","subject":"staff","name":"X","account":"same@example.test"}],"truncated":false}`, 200, 0, false, loginclient.ErrUnavailable},
		{"unknown field", `{"targets":[],"truncated":false,"extra":1}`, 200, 0, false, loginclient.ErrUnavailable},
		{"null list", `{"targets":null,"truncated":false}`, 200, 0, false, loginclient.ErrUnavailable},
		{"rejected input", `{}`, 400, 0, false, nil},
		{"outage", `{}`, 503, 0, false, loginclient.ErrUnavailable},
		{"wrong workload", `{}`, 401, 0, false, loginclient.ErrUnavailable},
		{"not deployed", `{}`, 404, 0, false, loginclient.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/internal/v1/customer-signups/login-targets" || r.Header.Get("Authorization") != "Bearer synthetic-workload" {
					t.Error("lookup escaped private workload boundary")
				}
				var body map[string]string
				if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 2 || body["email"] != "same@example.test" || body["requestId"] == "" {
					t.Error("lookup sent an incorrect email or request")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer native.Close()
			tokenFile := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(tokenFile, []byte("synthetic-workload"), 0600); err != nil {
				t.Fatal(err)
			}
			h := &Handlers{base: native.URL, tokenFile: tokenFile, client: native.Client()}
			targets, truncated, err := h.TenantLoginTargets(t.Context(), "same@example.test")
			if tc.want != nil {
				if !errors.Is(err, tc.want) || len(targets) != 0 {
					t.Fatalf("off-contract answer accepted: %v %v", targets, err)
				}
				return
			}
			if err != nil || len(targets) != tc.targets || truncated != tc.truncated {
				t.Fatalf("got %v truncated=%v err=%v", targets, truncated, err)
			}
			if tc.targets == 1 && (targets[0] != loginclient.LoginTarget{TenantID: tenant, OrganizationID: "helivanta-" + tenant, Subject: "staff", Name: "City Hospital", Account: "same@example.test"}) {
				t.Fatalf("target mangled: %+v", targets[0])
			}
		})
	}
}
