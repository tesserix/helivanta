package loginclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type targetResolverFunc func(context.Context, string) ([]LoginTarget, bool, error)

func (f targetResolverFunc) TenantLoginTargets(ctx context.Context, email string) ([]LoginTarget, bool, error) {
	return f(ctx, email)
}

// emailNative serves an unscoped auth request and one password check per
// user, each taking delay. sessionOrg overrides where a user's session lands.
type emailNative struct {
	mu         sync.Mutex
	delay      time.Duration
	accept     map[string]bool
	fail       map[string]bool
	sessionOrg map[string]string
	deleted    []string
}

func (n *emailNative) client(t *testing.T, targets []LoginTarget) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/oidc/auth_requests/V2_test":
			_ = json.NewEncoder(w).Encode(map[string]any{"authRequest": map[string]any{"id": "V2_test", "clientId": "web", "scope": []string{"openid"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/v2/sessions":
			var in struct {
				Checks struct {
					User map[string]string `json:"user"`
				} `json:"checks"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			user := in.Checks.User["userId"]
			time.Sleep(n.delay)
			switch {
			case n.fail[user]:
				w.WriteHeader(http.StatusInternalServerError)
			case n.accept[user]:
				_, _ = w.Write([]byte(`{"sessionId":"s-` + user + `","sessionToken":"t-` + user + `"}`))
			default:
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code":3,"message":"Password is invalid (COMMAND-3M0fs)"}`))
			}
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v2/sessions/s-"):
			user := strings.TrimPrefix(r.URL.Path, "/v2/sessions/s-")
			org := n.sessionOrg[user]
			for _, target := range targets {
				if target.Subject == user && org == "" {
					org = target.OrganizationID
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"factors": map[string]any{"user": map[string]string{"id": user, "organizationId": org}}}})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			n.mu.Lock()
			n.deleted = append(n.deleted, strings.TrimPrefix(r.URL.Path, "/v2/sessions/"))
			n.mu.Unlock()
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected native request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	c := New(server.URL, "synthetic", server.Client())
	c.SetTenantTargetResolver(targetResolverFunc(func(context.Context, string) ([]LoginTarget, bool, error) { return targets, false, nil }))
	return c
}

func fiveTargets() []LoginTarget {
	var out []LoginTarget
	for i := range MaxEmailCandidates {
		tenant := fmt.Sprintf("11111111-1111-4111-8111-%012d", i+1)
		out = append(out, LoginTarget{TenantID: tenant, OrganizationID: "helivanta-" + tenant, Subject: fmt.Sprintf("u%d", i), Name: "Org", Account: "same@example.test"})
	}
	return out
}

// Spec D2/D5: candidates are checked at once, so five refusals take about
// one check, not five, and the response time does not count accounts.
func TestEmailSessionsChecksEveryCandidateConcurrently(t *testing.T) {
	native := &emailNative{delay: 300 * time.Millisecond}
	c := native.client(t, fiveTargets())
	started := time.Now()
	got, err := c.CreatePasswordSessionForRequest(t.Context(), "V2_test", "web", "same@example.test", "wrong")
	elapsed := time.Since(started)
	if !IsCredentialRefusal(err) || len(got) != 0 {
		t.Fatalf("five refusals must be one credential refusal: %v %v", got, err)
	}
	if elapsed > 2*native.delay {
		t.Fatalf("candidates checked one after another: %v for five checks of %v", elapsed, native.delay)
	}
}

// An outage on any candidate fails the whole submission closed and
// terminates sessions the other candidates already produced.
func TestEmailSessionsFailClosedOnAnyCandidateOutage(t *testing.T) {
	native := &emailNative{accept: map[string]bool{"u0": true}, fail: map[string]bool{"u1": true}}
	c := native.client(t, fiveTargets())
	got, err := c.CreatePasswordSessionForRequest(t.Context(), "V2_test", "web", "same@example.test", "right")
	if !errors.Is(err, ErrUnavailable) || len(got) != 0 {
		t.Fatalf("partial outage answered: %v %v", got, err)
	}
	if len(native.deleted) != 1 || native.deleted[0] != "s-u0" {
		t.Fatalf("accepted session left alive: %v", native.deleted)
	}
}

// A session Zitadel placed outside the candidate's organisation is never
// accepted, even with the right password.
func TestEmailSessionsRefuseASessionOutsideTheCandidateOrg(t *testing.T) {
	targets := fiveTargets()[:2]
	native := &emailNative{accept: map[string]bool{"u0": true, "u1": true}, sessionOrg: map[string]string{"u1": targets[0].OrganizationID}}
	c := native.client(t, targets)
	got, err := c.CreatePasswordSessionForRequest(t.Context(), "V2_test", "web", "same@example.test", "right")
	if !errors.Is(err, ErrAuthRequestInvalid) || len(got) != 0 {
		t.Fatalf("foreign-org session accepted: %v %v", got, err)
	}
	if len(native.deleted) != 2 {
		t.Fatalf("sessions left alive after a foreign-org answer: %v", native.deleted)
	}
}

func TestEmailSessionsReturnEveryAcceptedAccount(t *testing.T) {
	native := &emailNative{accept: map[string]bool{"u1": true, "u3": true}}
	c := native.client(t, fiveTargets())
	got, err := c.CreatePasswordSessionForRequest(t.Context(), "V2_test", "web", "same@example.test", "right")
	if err != nil || len(got) != 2 || got[0].Target.Subject != "u1" || got[1].Target.Subject != "u3" || got[0].Session.ID != "s-u1" {
		t.Fatalf("accepted accounts lost: %+v %v", got, err)
	}
}

func TestEmailSessionsRefuseAResolverAnswerOverTheBound(t *testing.T) {
	targets := append(fiveTargets(), fiveTargets()[0])
	targets[5].Subject = "u5"
	native := &emailNative{}
	c := native.client(t, targets)
	if _, err := c.CreatePasswordSessionForRequest(t.Context(), "V2_test", "web", "same@example.test", "right"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("over-bound answer used: %v", err)
	}
}
