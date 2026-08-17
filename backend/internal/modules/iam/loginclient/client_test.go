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
	s, err := c.CreatePasswordSession(context.Background(), "test@helivanta.dev", "HmsDev123!")
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
	_, err := c.CreatePasswordSession(context.Background(), "test@helivanta.dev", "wrong")
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
	_, err := c.CreatePasswordSession(context.Background(), "nobody@helivanta.dev", "x")
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

// policyAnchor is the passwordCheckLifetime value every fixture in this
// file that wants to be RECOGNIZED carries — see LoginPolicy's doc
// comment on why this specific field, not a boolean, is the anchor.
const policyAnchor = `"passwordCheckLifetime":"864000s"`

func TestLoginPolicyReportsForceMFA(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"policy":{` + policyAnchor + `,"forceMfa":true}}`))
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

// A body that does not even carry the passwordCheckLifetime anchor is not
// recognizable as a login policy at all — see LoginPolicy's doc comment
// on why that field, not a boolean, is what this client uses to tell "a
// genuine policy whose forceMfa is unpopulated because it is false"
// apart from "a shape this client does not understand". Every fixture
// here lacks that anchor. These are the shapes a Zitadel upgrade could
// plausibly produce; a RENAMED forceMfa with the anchor PRESENT is
// covered separately by TestLoginPolicyRejectsARenamedOrRecasedForceMFA,
// since that shape must be rejected for a different reason (the rename
// check, not the anchor check).
func TestLoginPolicyRejectsBodiesItCannotUnderstand(t *testing.T) {
	bodies := map[string]string{
		"empty object":                            `{}`,
		"null body":                                `null`,
		"forceMfa un-nested":                       `{"forceMfa":true}`,
		"forceMfa renamed, anchor also absent":     `{"policy":{"force_mfa":true}}`,
		"policy present but no recognizable field": `{"policy":{"isDefault":true}}`,
		"policy explicitly null":                   `{"policy":null}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(body))
			})
			got, err := c.LoginPolicy(context.Background())
			if err == nil {
				t.Fatalf("LoginPolicy() = %+v, error = nil; a body without a recognizable policy object must not read as MFA off", got)
			}
			if !errors.Is(err, ErrUnavailable) {
				t.Errorf("error = %v, want ErrUnavailable so it reaches the same fail-closed branch as an unreachable Zitadel", err)
			}
		})
	}
}

// The shape that IS understood must still be accepted — decoding must
// not reject a legitimate "MFA off" answer, which would make every login
// a handoff.
func TestLoginPolicyAcceptsAnExplicitFalse(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"policy":{` + policyAnchor + `,"forceMfa":false}}`))
	})
	p, err := c.LoginPolicy(context.Background())
	if err != nil {
		t.Fatalf("LoginPolicy() error = %v, want an explicit false to be accepted", err)
	}
	if p.ForceMFA {
		t.Error("ForceMFA = true, want false")
	}
}

// TestLoginPolicyTreatsAbsentForceMFAAsFalseWhenPolicyIsRecognizable pins
// the finding this task's integration test made live against the real
// dev Zitadel v4.15.3: a genuine, healthy login policy elides `forceMfa`
// ENTIRELY when it is false (protojson's default zero-value omission),
// rather than sending an explicit `false` the way this package's earlier
// fixtures assumed. The anchor present with ForceMFA absent must decode
// to LoginPolicy{ForceMFA: false}, not an error — see LoginPolicy's doc
// comment for the full story.
func TestLoginPolicyTreatsAbsentForceMFAAsFalseWhenPolicyIsRecognizable(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"policy":{"allowUsernamePassword":true,"allowRegister":true,"isDefault":true,` + policyAnchor + `}}`))
	})
	p, err := c.LoginPolicy(context.Background())
	if err != nil {
		t.Fatalf("LoginPolicy() error = %v, want an absent-but-recognizable forceMfa to be accepted as false", err)
	}
	if p.ForceMFA {
		t.Error("ForceMFA = true, want false")
	}
}

// TestLoginPolicyRejectsARenamedOrRecasedForceMFA is the fix for the
// Critical review finding on this task: the anchor above closes "forceMfa
// absent because it is false", but on its own does NOTHING for "forceMfa
// absent because Zitadel renamed or re-cased it" — a body that anchors as
// recognized AND carries a differently-spelled MFA-forcing field must
// still be refused, not silently read as "MFA off". Before this fix, the
// realistic rename shape here (anchor present, field renamed) decoded to
// LoginPolicy{ForceMFA:false} and the login COMPLETED — exactly the
// fail-open Task 3 exists to prevent, re-entered through the elision
// workaround. See LoginPolicy's "Rename/re-casing detection" doc comment
// section for the full mechanism and its own documented residual.
func TestLoginPolicyRejectsARenamedOrRecasedForceMFA(t *testing.T) {
	bodies := map[string]string{
		"snake_case":                `{"policy":{` + policyAnchor + `,"force_mfa":true}}`,
		"PascalCase":                `{"policy":{` + policyAnchor + `,"ForceMfa":true}}`,
		"SCREAMING_SNAKE_CASE":      `{"policy":{` + policyAnchor + `,"FORCE_MFA":true}}`,
		"forceMfa present but not a bool (type drift)": `{"policy":{` + policyAnchor + `,"forceMfa":"true"}}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(body))
			})
			got, err := c.LoginPolicy(context.Background())
			if err == nil {
				t.Fatalf("LoginPolicy() = %+v, error = nil; a renamed/re-cased/retyped forceMfa must not read as MFA off", got)
			}
			if !errors.Is(err, ErrUnavailable) {
				t.Errorf("error = %v, want ErrUnavailable so it reaches the same fail-closed branch as an unreachable Zitadel", err)
			}
		})
	}
}

// TestLoginPolicyTreatsForceMFALocalOnlyAsRequiringMFA pins Finding 4 of
// this task's second review round: forceMfaLocalOnly is a REAL Zitadel
// login-policy field ("require MFA for local/password users, not
// federated ones" — a normal configuration, not a hypothetical), and
// before this fix a policy with forceMfaLocalOnly:true and forceMfa
// elided (the ordinary "false" shape, per LoginPolicy's doc comment)
// decoded to LoginPolicy{ForceMFA:false} — a real, reachable MFA bypass
// through supported Zitadel configuration, not upstream drift. See
// LoginPolicy's "forceMfaLocalOnly — a second, REAL field" doc comment
// section for the fold-together decision and its documented assumption
// (every HMS user is local today).
func TestLoginPolicyTreatsForceMFALocalOnlyAsRequiringMFA(t *testing.T) {
	// forceMfa absent entirely, matching the real elision shape — the
	// realistic body a Zitadel org with forceMfaLocalOnly configured and
	// forceMfa left off actually sends (verified live, see this
	// function's doc comment).
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"policy":{` + policyAnchor + `,"forceMfaLocalOnly":true}}`))
	})
	p, err := c.LoginPolicy(context.Background())
	if err != nil {
		t.Fatalf("LoginPolicy() error = %v, want forceMfaLocalOnly to be read, not refused", err)
	}
	if !p.ForceMFA {
		t.Error("ForceMFA = false, want true: forceMfaLocalOnly:true must require MFA")
	}
}

// TestLoginPolicyReadsEveryKeyInMFAPolicyKeys is the CI half of
// mfaPolicyKeys' claim that adding a THIRD MFA-forcing field is a
// one-place change. It is not a test of a shape Zitadel sends today —
// there is no third field yet — it is a test that the MECHANISM is
// list-driven: it appends a key to mfaPolicyKeys (restoring it
// afterwards) and asserts a policy body setting ONLY that key produces
// ForceMFA:true.
//
// This exists because an earlier version of LoginPolicy iterated
// mfaPolicyKeys for the rename guard but called readMFABool twice with
// HARDCODED literals, while mfaPolicyKeys' own doc comment claimed both
// halves were list-driven. A contributor following that comment would
// have registered a third key, gotten the rename guard, and had the
// field never read into ForceMFA at all — a silent MFA bypass in the one
// file written to prevent exactly that. Proven to fail against that
// implementation before it passed against this one (see the branch's
// final-fixes report for the failure output).
func TestLoginPolicyReadsEveryKeyInMFAPolicyKeys(t *testing.T) {
	// Mutating a package-level var is safe here only because Go runs
	// tests within a package sequentially unless t.Parallel() is called,
	// and nothing in this file calls it. Restored via t.Cleanup so a
	// failure cannot leak the extra key into a later test.
	original := mfaPolicyKeys
	t.Cleanup(func() { mfaPolicyKeys = original })
	mfaPolicyKeys = append(append([]string{}, original...), "forceMfaHypothetical")

	// forceMfa and forceMfaLocalOnly are BOTH absent (the ordinary
	// elision shape), so the only thing that can produce ForceMFA:true
	// here is the newly registered key actually being read.
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"policy":{` + policyAnchor + `,"forceMfaHypothetical":true}}`))
	})
	p, err := c.LoginPolicy(context.Background())
	if err != nil {
		t.Fatalf("LoginPolicy() error = %v, want the newly registered key to be read, not refused", err)
	}
	if !p.ForceMFA {
		t.Error("ForceMFA = false, want true: a key registered in mfaPolicyKeys must be READ into ForceMFA, " +
			"not merely guarded against renames — otherwise adding a third MFA-forcing field is a silent MFA bypass")
	}
}

// TestLoginPolicyForceMFAAndForceMFALocalOnlyDoNotFalsePositiveOnEachOther
// proves the rename guard extension for forceMfaLocalOnly does not break
// the ordinary case where only ONE of the two fields is set — a body
// with forceMfaLocalOnly present must not trip forceMfa's rename check
// (and vice versa), since "forceMfaLocalOnly" and "forceMfa" normalize
// to different strings ("forcemfalocalonly" vs "forcemfa"). Verified
// live against the real dev Zitadel as part of this fix (see this task's
// report); this test pins it structurally too.
func TestLoginPolicyForceMFAAndForceMFALocalOnlyDoNotFalsePositiveOnEachOther(t *testing.T) {
	t.Run("forceMfaLocalOnly alone does not trip forceMfa's guard", func(t *testing.T) {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"policy":{` + policyAnchor + `,"forceMfaLocalOnly":false}}`))
		})
		p, err := c.LoginPolicy(context.Background())
		if err != nil {
			t.Fatalf("LoginPolicy() error = %v, want a lone forceMfaLocalOnly:false to be accepted", err)
		}
		if p.ForceMFA {
			t.Error("ForceMFA = true, want false")
		}
	})
	t.Run("forceMfa alone does not trip forceMfaLocalOnly's guard", func(t *testing.T) {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"policy":{` + policyAnchor + `,"forceMfa":false}}`))
		})
		p, err := c.LoginPolicy(context.Background())
		if err != nil {
			t.Fatalf("LoginPolicy() error = %v, want a lone forceMfa:false to be accepted", err)
		}
		if p.ForceMFA {
			t.Error("ForceMFA = true, want false")
		}
	})
}

// TestLoginPolicyRejectsARenamedOrRecasedForceMFALocalOnly applies the
// same rigour Finding 4 asked for: forceMfaLocalOnly gets the identical
// rename/re-casing/type-drift guard forceMfa already had, not a weaker
// one, since a renamed forceMfaLocalOnly would reopen exactly the same
// class of bypass this whole fix exists to close.
func TestLoginPolicyRejectsARenamedOrRecasedForceMFALocalOnly(t *testing.T) {
	bodies := map[string]string{
		"snake_case":           `{"policy":{` + policyAnchor + `,"force_mfa_local_only":true}}`,
		"PascalCase":           `{"policy":{` + policyAnchor + `,"ForceMfaLocalOnly":true}}`,
		"SCREAMING_SNAKE_CASE": `{"policy":{` + policyAnchor + `,"FORCE_MFA_LOCAL_ONLY":true}}`,
		"forceMfaLocalOnly present but not a bool (type drift)": `{"policy":{` + policyAnchor + `,"forceMfaLocalOnly":"true"}}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(body))
			})
			got, err := c.LoginPolicy(context.Background())
			if err == nil {
				t.Fatalf("LoginPolicy() = %+v, error = nil; a renamed/re-cased/retyped forceMfaLocalOnly must not read as MFA off", got)
			}
			if !errors.Is(err, ErrUnavailable) {
				t.Errorf("error = %v, want ErrUnavailable so it reaches the same fail-closed branch as an unreachable Zitadel", err)
			}
		})
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
