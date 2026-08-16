package loginclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL, "test-pat", srv.Client())
}

func TestCreatePasswordSessionReturnsSessionOnSuccess(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-pat" {
			t.Errorf("Authorization = %q, want Bearer test-pat", got)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"sessionId":"386477864129658887","sessionToken":"tok-abc"}`))
	})
	s, err := c.CreatePasswordSession(context.Background(), "test@hms.dev", "HmsDev123!")
	if err != nil {
		t.Fatalf("CreatePasswordSession() error = %v", err)
	}
	if s.ID != "386477864129658887" || s.Token != "tok-abc" {
		t.Errorf("session = %+v, want id/token from body", s)
	}
}

// Observed: HTTP 400, COMMAND-3M0fs, with a failedAttempts counter.
func TestCreatePasswordSessionMapsWrongPasswordToErrBadCredentials(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":3,"message":"Password is invalid (COMMAND-3M0fs)","details":[{"@type":"type.googleapis.com/zitadel.v1.CredentialsCheckError","id":"COMMAND-3M0fs","message":"Password is invalid","failedAttempts":1}]}`))
	})
	_, err := c.CreatePasswordSession(context.Background(), "test@hms.dev", "wrong")
	if !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("error = %v, want ErrBadCredentials", err)
	}
	// failedAttempts must not survive into the error text — it reaches a log line.
	if got := err.Error(); strings.Contains(got, "failedAttempts") || strings.Contains(got, "1") {
		t.Errorf("error text %q leaks the attempt counter", got)
	}
}

// Observed: HTTP 404, QUERY-Dfbg2.
func TestCreatePasswordSessionMapsUnknownUserToErrUserNotFound(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"code":5,"message":"User could not be found (QUERY-Dfbg2)"}`))
	})
	_, err := c.CreatePasswordSession(context.Background(), "nobody@hms.dev", "x")
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("error = %v, want ErrUserNotFound", err)
	}
}

func TestFinalizeReturnsCallbackURL(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"callbackUrl":"http://localhost:4301/api/auth/callback?code=abc&state=s"}`))
	})
	got, err := c.finalize(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	if err != nil {
		t.Fatalf("finalize() error = %v", err)
	}
	if got != "http://localhost:4301/api/auth/callback?code=abc&state=s" {
		t.Errorf("callbackUrl = %q", got)
	}
}

func TestAuthRequestParsesClientAndRedirect(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"authRequest":{"id":"V2_386477922262777863","clientId":"386401161701228551","scope":["openid"],"redirectUri":"http://localhost:4301/api/auth/callback"}}`))
	})
	ar, err := c.AuthRequest(context.Background(), "V2_386477922262777863")
	if err != nil {
		t.Fatalf("AuthRequest() error = %v", err)
	}
	if ar.ClientID != "386401161701228551" || ar.ID != "V2_386477922262777863" {
		t.Errorf("authRequest = %+v", ar)
	}
}

func TestLoginPolicyReportsForceMFA(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"policy":{"allowUsernamePassword":true,"forceMfa":true}}`))
	})
	p, err := c.LoginPolicy(context.Background())
	if err != nil {
		t.Fatalf("LoginPolicy() error = %v", err)
	}
	if !p.ForceMFA {
		t.Error("ForceMFA = false, want true")
	}
}

// A policy read that fails must NOT report "no MFA required" — spec D4 fails closed.
func TestLoginPolicyErrorsRatherThanReportingNoMFA(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := c.LoginPolicy(context.Background()); err == nil {
		t.Fatal("LoginPolicy() error = nil, want an error so the caller can fail closed")
	}
}

// A broken or compromised Zitadel streaming an unbounded 200 response must
// not be decoded without a size cap (review finding 1). The fake server
// here writes a well-formed JSON object whose redirectUri field alone is
// far larger than maxSuccessBodyBytes; bounded decoding truncates the
// body mid-value and Decode fails, whereas an unbounded decode would
// succeed. This is deliberately size, not shape: a passing test here
// means the bound is applied, not that the client rejects malformed JSON.
func TestDoBoundsSuccessResponseSize(t *testing.T) {
	oversized := strings.Repeat("a", maxSuccessBodyBytes*2)
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"authRequest":{"id":"V2_1","clientId":"c","redirectUri":"%s","scope":["openid"]}}`, oversized)
	})
	if _, err := c.AuthRequest(context.Background(), "V2_1"); err == nil {
		t.Fatal("AuthRequest() error = nil, want an error from a response body larger than maxSuccessBodyBytes")
	}
}

// A real spike-observed auth request id (alphanumeric plus underscore)
// must survive url.PathEscape unchanged — the fix for review finding 2
// must not corrupt a legitimate id while closing the path-injection gap.
func TestAuthRequestPathEscapePreservesValidID(t *testing.T) {
	const id = "V2_386477922262777863"
	var gotRequestURI string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotRequestURI = r.RequestURI
		w.Write([]byte(`{"authRequest":{"id":"V2_386477922262777863","clientId":"c","redirectUri":"r","scope":["openid"]}}`))
	})
	if _, err := c.AuthRequest(context.Background(), id); err != nil {
		t.Fatalf("AuthRequest() error = %v", err)
	}
	want := "/v2/oidc/auth_requests/" + id
	if gotRequestURI != want {
		t.Errorf("request URI = %q, want %q (escaping must not alter a valid id)", gotRequestURI, want)
	}
}

// The auth request id is a browser-supplied query parameter (spike §1),
// so it is attacker-influenced input. An id containing path-traversal
// segments or a query string must not be able to redirect the request to
// a different path (review finding 2). This asserts on the literal
// request-target the fake server received, not on the returned error.
func TestAuthRequestEscapesAdversarialID(t *testing.T) {
	const adversarial = "../../v2/users?x=1"
	var gotRequestURI string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotRequestURI = r.RequestURI
		w.Write([]byte(`{"authRequest":{"id":"x","clientId":"c","redirectUri":"r","scope":[]}}`))
	})
	if _, err := c.AuthRequest(context.Background(), adversarial); err != nil {
		t.Fatalf("AuthRequest() error = %v", err)
	}
	want := "/v2/oidc/auth_requests/" + url.PathEscape(adversarial)
	if gotRequestURI != want {
		t.Errorf("request URI = %q, want %q (adversarial id must not add path segments or a query string)", gotRequestURI, want)
	}
	if strings.Count(gotRequestURI, "/") != strings.Count("/v2/oidc/auth_requests/", "/") {
		t.Errorf("request URI = %q contains an unescaped path separator from the id", gotRequestURI)
	}
}

// Same adversarial-id protection as TestAuthRequestEscapesAdversarialID,
// but for finalize's authRequestID parameter — a separate interpolation
// site (review finding 2 named both client.go:139 and client.go:189).
func TestFinalizeEscapesAdversarialAuthRequestID(t *testing.T) {
	const adversarial = "../../v2/users?x=1"
	var gotRequestURI string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotRequestURI = r.RequestURI
		w.Write([]byte(`{"callbackUrl":"http://localhost:4301/api/auth/callback?code=abc&state=s"}`))
	})
	if _, err := c.finalize(context.Background(), adversarial, Session{ID: "1", Token: "t"}); err != nil {
		t.Fatalf("finalize() error = %v", err)
	}
	want := "/v2/oidc/auth_requests/" + url.PathEscape(adversarial)
	if gotRequestURI != want {
		t.Errorf("request URI = %q, want %q (adversarial id must not add path segments or a query string)", gotRequestURI, want)
	}
}
