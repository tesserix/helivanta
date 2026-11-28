package customeronboarding

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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

type parkCall struct{ user, org, password string }

type fakeParker struct {
	calls []parkCall
	err   error
}

func (f *fakeParker) ParkSetupSession(_ context.Context, user, org, password string) (*http.Cookie, error) {
	f.calls = append(f.calls, parkCall{user, org, password})
	if f.err != nil {
		return nil, f.err
	}
	return &http.Cookie{Name: "helivanta_setup_handle", Value: "parked-handle", Path: "/", HttpOnly: true, Secure: true}, nil
}

func setupToken(t *testing.T) string {
	t.Helper()
	key := t.TempDir() + "/token"
	if err := os.WriteFile(key, []byte("test-workload"), 0o600); err != nil {
		t.Fatal(err)
	}
	return key
}

// #1778 D2: a complete staff receipt names the account for this server only;
// the proxy parks the session for the password just set and strips the names.
func TestStaffSetupParksTheSessionAndNeverForwardsTheAccount(t *testing.T) {
	const upstream = `{"state":"complete","subject":"staff-user-1","organization_id":"helivanta-11111111-1111-4111-8111-111111111111"}`
	for _, tc := range []struct {
		name, action string
		password     string
		parkErr      error
		parks        bool
	}{
		{"one-step email", "email", "private-password", nil, true},
		{"password link", "password", "private-password", nil, true},
		{"status never parks", "status", "", nil, false},
		{"park failure keeps setup successful", "email", "private-password", errors.New("zitadel down"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parker := &fakeParker{err: tc.parkErr}
			h := &Handlers{origin: "https://helivanta.app", base: "http://identity.invalid", tokenFile: setupToken(t), parker: parker,
				client: &http.Client{Transport: staffSetupTransport(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(upstream))}, nil
				})}}
			r := gin.New()
			r.POST("/v1/auth/staff-setup/:action", h.StaffSetup(StaffSetupDeps{Limiter: ratelimit.NewMemory(100), Limit: ratelimit.Rule{Rate: 20, Burst: 10, Per: time.Minute}}))
			in := staffcontract.StaffRecipientInput{InvitationID: uuid.Must(uuid.NewV7()), Ticket: base64.RawURLEncoding.EncodeToString(make([]byte, 32))}
			if tc.action != "status" {
				in.Code, in.Password = "opaque+Code/==#", tc.password
			}
			body, _ := json.Marshal(in)
			req := httptest.NewRequest("POST", "/v1/auth/staff-setup/"+tc.action, strings.NewReader(string(body)))
			req.Header.Set("Origin", h.origin)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != 200 || strings.Contains(w.Body.String(), "staff-user-1") || strings.Contains(w.Body.String(), "organization_id") {
				t.Fatalf("browser answer: %d %s", w.Code, w.Body)
			}
			if !tc.parks {
				if len(parker.calls) != 0 || w.Header().Get("Set-Cookie") != "" {
					t.Fatalf("parked without a password: %+v", parker.calls)
				}
				return
			}
			want := parkCall{"staff-user-1", "helivanta-11111111-1111-4111-8111-111111111111", "private-password"}
			if len(parker.calls) != 1 || parker.calls[0] != want {
				t.Fatalf("park calls %+v", parker.calls)
			}
			if got := w.Header().Get("Set-Cookie"); (tc.parkErr == nil) != strings.Contains(got, "helivanta_setup_handle=parked-handle") {
				t.Fatalf("cookie %q with park error %v", got, tc.parkErr)
			}
		})
	}
}

// An owner's proof names owner-<tenant>, whose organisation is helivanta-<tenant>.
func TestOwnerProofParksOnlyForTheProvenOwner(t *testing.T) {
	tenant := uuid.New().String()
	for _, tc := range []struct {
		name, body string
		want       *parkCall
	}{
		{"email link with password", `{"userId":"owner-` + tenant + `","code":"proof","password":"private-password"}`, &parkCall{"owner-" + tenant, "helivanta-" + tenant, "private-password"}},
		{"email link without password", `{"userId":"owner-` + tenant + `","code":"proof"}`, nil},
		{"not an owner subject", `{"userId":"someone-else","code":"proof","password":"private-password"}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Signup-Command", uuid.New().String())
				_, _ = w.Write([]byte(`{"state":"provisioning"}`))
			}))
			defer owner.Close()
			parker := &fakeParker{}
			h := &Handlers{origin: "https://helivanta.app", base: owner.URL, tokenFile: setupToken(t), client: owner.Client(), parker: parker}
			router := gin.New()
			router.POST("/v1/auth/signup/verify-email", h.VerifyEmail)
			request := httptest.NewRequest(http.MethodPost, "/v1/auth/signup/verify-email", strings.NewReader(tc.body))
			request.Header.Set("Origin", "https://helivanta.app")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != 200 {
				t.Fatalf("proof %d %s", response.Code, response.Body)
			}
			if tc.want == nil {
				if len(parker.calls) != 0 {
					t.Fatalf("parked %+v", parker.calls)
				}
				return
			}
			if len(parker.calls) != 1 || parker.calls[0] != *tc.want {
				t.Fatalf("park calls %+v", parker.calls)
			}
		})
	}
}
