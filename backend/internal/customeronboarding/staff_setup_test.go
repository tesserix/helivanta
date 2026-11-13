package customeronboarding

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"github.com/tesserix/onboarding-service/api/staffcontract"
)

func TestStaffSetupRejectsForeignOriginsAndMissingRateBoundaryBeforeHandoff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handlers{origin: "https://helivanta.app"}
	r := gin.New()
	r.POST("/v1/auth/staff-setup/:action", h.StaffSetup(StaffSetupDeps{Limit: ratelimit.Rule{Rate: 20, Burst: 10, Per: time.Minute}}))
	for _, origin := range []string{"", "https://evil.example", "https://helivanta.app.evil.example", "https://helivanta.app"} {
		req := httptest.NewRequest("POST", "/v1/auth/staff-setup/status", strings.NewReader(`{}`))
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		want := 403
		if origin == h.origin {
			want = 503
		}
		if w.Code != want {
			t.Fatalf("origin boundary: got %d want %d", w.Code, want)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("private proof response is cacheable")
		}
	}
}

type staffSetupTransport func(*http.Request) (*http.Response, error)

func (f staffSetupTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStaffSetupBoundsPrivateHandoffAndNeverEchoesUpstreamSecrets(t *testing.T) {
	for _, tc := range []struct {
		name, body, retry string
		status, want      int
	}{
		{"complete", `{"state":"complete"}`, "", 200, 200},
		{"missing binding", `{"state":"email_action_required"}`, "", 200, 503},
		{"secret field", `{"state":"complete","secret":"private-upstream-secret"}`, "", 200, 503},
		{"malformed", `private-upstream-secret`, "", 200, 503},
		{"oversized", strings.Repeat(" ", 8193), "", 200, 503},
		{"redirect", `private-upstream-secret`, "", 302, 503},
		{"limit", `private-upstream-secret`, "60", 429, 429},
		{"unsafe retry", `private-upstream-secret`, "999999999", 429, 503},
		{"invalid", `private-upstream-secret`, "", 400, 400},
		{"expired", `private-upstream-secret`, "", 409, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := t.TempDir() + "/token"
			if err := os.WriteFile(key, []byte("test-workload"), 0600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			h := &Handlers{origin: "https://helivanta.app", base: "http://identity.invalid", tokenFile: key, client: &http.Client{Transport: staffSetupTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.String() != "http://identity.invalid/internal/v1/customer-signups/staff-invitations/status" || req.Header.Get("Authorization") != "Bearer test-workload" || req.Header.Get("X-Tenant-ID") != "" || req.Header.Get("Cookie") != "" {
					t.Fatal("browser authority leaked into private request")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": {tc.retry}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}}
			r := gin.New()
			r.POST("/v1/auth/staff-setup/:action", h.StaffSetup(StaffSetupDeps{Limiter: ratelimit.NewMemory(100), Limit: ratelimit.Rule{Rate: 20, Burst: 10, Per: time.Minute}}))
			in := staffcontract.StaffRecipientInput{InvitationID: uuid.Must(uuid.NewV7()), Ticket: base64.RawURLEncoding.EncodeToString(make([]byte, 32))}
			body, _ := json.Marshal(in)
			req := httptest.NewRequest("POST", "/v1/auth/staff-setup/status", strings.NewReader(string(body)))
			req.Header.Set("Origin", h.origin)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Tenant-ID", "forged")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want || calls != 1 || strings.Contains(w.Body.String(), "private-upstream-secret") {
				t.Fatalf("private handoff: status=%d calls=%d", w.Code, calls)
			}
		})
	}
}

func TestStaffSetupCountsMalformedRequestsInTheSharedPublicBudget(t *testing.T) {
	h := &Handlers{origin: "https://helivanta.app"}
	r := gin.New()
	r.POST("/v1/auth/staff-setup/:action", h.StaffSetup(StaffSetupDeps{Limiter: ratelimit.NewMemory(10), Limit: ratelimit.Rule{Rate: 1, Burst: 1, Per: time.Hour}}))
	for i, action := range []string{"status", "password"} {
		req := httptest.NewRequest("POST", "/v1/auth/staff-setup/"+action, strings.NewReader(`{}`))
		req.Header.Set("Origin", h.origin)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		want := 400
		if i == 1 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("shared budget: got %d want %d", w.Code, want)
		}
	}
}
