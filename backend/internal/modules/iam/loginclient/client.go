// Package loginclient speaks the four Zitadel v2 "login client" HTTP calls
// HMS's own login page needs to drive a session end to end: read an OIDC
// auth request, create a password-checked session, finalize the auth
// request into a callback URL, and read the org login policy. It makes NO
// authorization decision — whether a session is SUFFICIENT to finalize
// (e.g. whether MFA is required and present) lives in sufficiency.go
// (Task 3), not here. This package only knows how to make the four calls
// and translate their observed error shapes into typed sentinels; it has
// no opinion on what a caller should do with them.
//
// Every JSON shape and error mapping here is pinned to what was OBSERVED
// against a live Zitadel v4.15.3 instance, recorded in
// docs/superpowers/spikes/2026-08-16-zitadel-login-client.md — not to what
// the docs say the API should return.
package loginclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Sentinel errors callers can compare with errors.Is. Wrapping preserves
// the underlying status/body context for logs while keeping the sentinel
// stable for callers that only care about the category.
var (
	// ErrBadCredentials is returned for a wrong password (observed: HTTP
	// 400, COMMAND-3M0fs). It is deliberately kept distinct from
	// ErrUserNotFound even though Task 4 must answer both identically to
	// the browser (spike §3) — that collapsing is Task 4's job, done at
	// the point it logs which one actually happened, not lost here.
	ErrBadCredentials = errors.New("loginclient: bad credentials")
	// ErrUserNotFound is returned for an unknown loginName (observed:
	// HTTP 404, QUERY-Dfbg2).
	ErrUserNotFound = errors.New("loginclient: user not found")
	// ErrAuthRequestInvalid is returned when Zitadel does not recognize
	// the auth request id — either it never existed, already completed,
	// or expired.
	ErrAuthRequestInvalid = errors.New("loginclient: auth request invalid")
	// ErrUnavailable covers 5xx responses and transport failures: Zitadel
	// could not answer at all, as opposed to answering with a refusal.
	ErrUnavailable = errors.New("loginclient: zitadel unavailable")
)

// defaultTimeout bounds every call this client makes. Zitadel's own
// observed latency for a WRONG password is ~0.7s (the password hash is
// actually computed); 10s leaves generous room above that without letting
// a stalled Zitadel hang HMS's login handler indefinitely.
const defaultTimeout = 10 * time.Second

// maxSuccessBodyBytes bounds every 2xx response body this client decodes.
// The largest real payload observed (the spike's auth-request response)
// is a few hundred bytes; session ids/tokens are similarly small. 64KiB
// is generous headroom above any real payload while still capping memory
// use if a broken or compromised Zitadel streamed an unbounded response —
// mirroring the same reasoning readZitadelErrorID already applies to the
// error path (io.LimitReader(r, 4096)), just sized for larger legitimate
// bodies. TestDoBoundsSuccessResponseSize pins that this bound is
// actually applied, not just documented.
const maxSuccessBodyBytes = 64 * 1024

// Client speaks Zitadel's v2 login-client HTTP API, authenticating every
// call with a login client PAT (Personal Access Token) rather than an
// end-user credential — see the spike §1: all four calls in this package
// authenticate with the login client PAT, not the session being
// established.
type Client struct {
	baseURL string
	token   string
	hc      *http.Client
}

// New builds a Client against baseURL (Zitadel's own origin, not HMS's),
// authenticating with token (the login client PAT). hc is used as-is when
// non-nil so callers can inject their own transport (tests use
// httptest.Server's own client); a nil hc gets one built with
// defaultTimeout, because the zero-value http.Client has NO timeout and a
// stalled Zitadel would otherwise hang the caller forever.
func New(baseURL, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{baseURL: baseURL, token: token, hc: hc}
}

// AuthRequest is the subset of Zitadel's GET /v2/oidc/auth_requests/{id}
// response this client needs: enough to know which OIDC client and
// redirect the browser arrived for, and which scopes it asked for. Fields
// Zitadel returns that HMS has no use for (e.g. prompt, app) are dropped
// at the wire-decoding boundary rather than carried through.
type AuthRequest struct {
	ID          string
	ClientID    string
	RedirectURI string
	Scope       []string
}

// Session is the sessionId/sessionToken pair Zitadel returns from POST
// /v2/sessions and expects back, verbatim, in the finalize call. Token is
// a bearer-equivalent secret for this one session — it must be treated as
// a credential (never logged) by every caller, the same way this package
// never puts it in an error string.
type Session struct {
	ID    string
	Token string
}

// LoginPolicy is the subset of Zitadel's org login policy this client
// exposes. Only ForceMFA is modeled because it is the one field
// sufficiency.go (Task 3) needs to decide whether a password-only session
// is enough to finalize — see the spike §2: Zitadel does NOT itself
// refuse to finalize a password-only session against a forceMfa policy,
// so HMS must read this and enforce it structurally.
type LoginPolicy struct {
	ForceMFA bool
}

// AuthRequest fetches GET /v2/oidc/auth_requests/{id}. id is escaped with
// url.PathEscape before being placed in the URL — see the do call in
// finalize for why this matters: it originates as a browser-supplied
// query parameter (spike §1, "/login?authRequest=V2_..."), not a value
// this package minted itself.
func (c *Client) AuthRequest(ctx context.Context, id string) (AuthRequest, error) {
	var wire struct {
		AuthRequest struct {
			ID          string   `json:"id"`
			ClientID    string   `json:"clientId"`
			RedirectURI string   `json:"redirectUri"`
			Scope       []string `json:"scope"`
		} `json:"authRequest"`
	}
	if err := c.do(ctx, http.MethodGet, "/v2/oidc/auth_requests/"+url.PathEscape(id), nil, &wire, ErrAuthRequestInvalid); err != nil {
		return AuthRequest{}, err
	}
	return AuthRequest{
		ID:          wire.AuthRequest.ID,
		ClientID:    wire.AuthRequest.ClientID,
		RedirectURI: wire.AuthRequest.RedirectURI,
		Scope:       wire.AuthRequest.Scope,
	}, nil
}

// CreatePasswordSession creates a Zitadel session by checking loginName
// and password together (POST /v2/sessions, body shape from the spike
// §1/§3). A wrong password and an unknown user are mapped to different
// sentinels (ErrBadCredentials vs ErrUserNotFound) — see ErrBadCredentials'
// doc comment for why that distinction survives here even though the
// browser-facing answer must not.
func (c *Client) CreatePasswordSession(ctx context.Context, loginName, password string) (Session, error) {
	body := map[string]any{
		"checks": map[string]any{
			"user":     map[string]any{"loginName": loginName},
			"password": map[string]any{"password": password},
		},
	}
	var wire struct {
		SessionID    string `json:"sessionId"`
		SessionToken string `json:"sessionToken"`
	}
	if err := c.do(ctx, http.MethodPost, "/v2/sessions", body, &wire, ErrUserNotFound); err != nil {
		return Session{}, err
	}
	return Session{ID: wire.SessionID, Token: wire.SessionToken}, nil
}

// finalize hands the created session to the auth request (POST
// /v2/oidc/auth_requests/{id}, body shape from the spike §1) and returns
// the callbackUrl Zitadel computes — the SAME callback
// apps/shell/app/api/auth/callback/page.tsx already handles (spike §1),
// so this method's return value needs no further transformation by the
// caller; it can be redirected to as-is.
//
// It is UNEXPORTED on purpose, and that is the structural half of spec
// D4. The spike §2 proved Zitadel issues an authorization code for a
// password-only session even under a forceMfa policy — it does not
// enforce MFA for a login client at all. So whether a session is
// sufficient to finalize is HMS's decision, and the only way to reach
// this call from outside the package is CompleteIfSufficient, which makes
// that decision first. A future contributor adding a second completion
// path has to defeat the package boundary deliberately rather than merely
// forget a convention; archtest's TestFinalizeCallSiteIsUnique pins that
// the HTTP call itself stays in this one file.
//
// authRequestID is escaped with url.PathEscape before being placed in the
// URL. Like AuthRequest's id, it traces back to a browser query
// parameter (spike §1) — attacker-influenced input, even though every id
// observed so far is alphanumeric-plus-underscore. Unescaped, a value
// containing "/" or ".." could alter which path this request actually
// hits; PathEscape turns any such character into a literal path segment
// rather than a separator. TestFinalizeEscapesAdversarialAuthRequestID
// and TestAuthRequestEscapesAdversarialID pin this against both an
// adversarial id and a real spike-observed id, so the escaping cannot
// quietly corrupt a legitimate id either.
func (c *Client) finalize(ctx context.Context, authRequestID string, s Session) (string, error) {
	body := map[string]any{
		"session": map[string]any{
			"sessionId":    s.ID,
			"sessionToken": s.Token,
		},
	}
	var wire struct {
		CallbackURL string `json:"callbackUrl"`
	}
	if err := c.do(ctx, http.MethodPost, "/v2/oidc/auth_requests/"+url.PathEscape(authRequestID), body, &wire, ErrAuthRequestInvalid); err != nil {
		return "", err
	}
	return wire.CallbackURL, nil
}

// LoginPolicy reads the org's login policy (GET
// /management/v1/policies/login). There is no meaningful 404 case for
// this endpoint — a login policy always exists — so a 404 here falls
// through to ErrUnavailable rather than being given a dedicated sentinel.
//
// Every error path returns a zero LoginPolicy{} alongside a non-nil
// error, and NEVER a zero value with err == nil: ForceMFA's zero value is
// false, which reads as "no MFA required". Spec D4's fail-closed
// requirement means a caller that cannot read this policy must be able to
// see that it could not, rather than being handed a value
// indistinguishable from a real "MFA off" answer.
// TestLoginPolicyErrorsRatherThanReportingNoMFA pins this.
//
// That promise is why field absence has always been treated as an error
// rather than a default, and why — see the "verified live" and "rename
// detection" sections below — that promise now spans TWO different kinds
// of absence, not one.
//
// # ForceMFA absent does NOT mean "unrecognized" — verified live 2026-08-16
//
// This task's integration test (loginui_integration_test.go) against the
// real dev Zitadel v4.15.3 discovered that a genuine, healthy org login
// policy — `{"policy":{"allowUsernamePassword":true,...,"isDefault":true}}`
// — never sends `forceMfa` AT ALL when it is false: Zitadel's own
// protojson marshaling elides zero-value boolean fields by default, the
// same way `secondFactors`/`multiFactors` would be elided as empty
// arrays if unset. The original design (require an EXPLICIT `forceMfa`,
// full stop) therefore treated the ORDINARY, non-MFA-enforcing case as
// "cannot understand this response" on every single real login —
// CompleteIfSufficient's fail-closed branch then handed EVERY login off
// to Zitadel's hosted UI, which is not a conservative failure mode here,
// it is the feature not working at all.
//
// The fix is an ANCHOR, not a relaxed default:
// `policy.passwordCheckLifetime` is a `google.protobuf.Duration` field,
// which — unlike a scalar bool — protojson gives explicit-presence
// semantics: it is emitted whenever the policy message itself is
// present, never elided at its zero value the way `forceMfa`/
// `allowUsernamePassword` are. Its presence is therefore evidence "this
// really is Zitadel's login policy object", independent of what any
// individual boolean field happens to be set to — including, notably,
// an org that has legitimately set `allowUsernamePassword` to false,
// which an anchor built on THAT field would have misread as
// unrecognized. ForceMFA absent alongside the anchor PRESENT decodes to
// false; the anchor absent (with or without ForceMFA) still refuses,
// exactly as the original design did for every shape it covered.
//
// # Rename/re-casing detection — closes the residual the anchor alone leaves open
//
// The anchor closes "forceMfa absent because it is false". It does
// nothing for "forceMfa absent because Zitadel renamed or re-cased it" —
// `{"policy":{"passwordCheckLifetime":"864000s","force_mfa":true}}`
// anchors as recognized AND has no field literally named "forceMfa", so
// naively reading `wire.Policy["forceMfa"]` as absent-therefore-false
// would silently complete a login Zitadel is telling HMS requires MFA.
// That is the exact fail-open Task 3 was written to close, re-opened by
// the anchor fix above if nothing else changed.
//
// The policy object is therefore decoded into a `map[string]any`
// (rather than a fixed struct) so every key Zitadel actually sent is
// visible, and every key is checked: any key whose name NORMALIZES
// (lowercased, underscores stripped) to "forcemfa" but is not spelled
// exactly "forceMfa" is treated as unrecognized and refuses, on the
// theory that Zitadel renaming or re-casing this one field is far more
// likely than it inventing an unrelated field that happens to normalize
// the same way. This also catches `forceMfa` changing TYPE (e.g. a
// future Zitadel encoding it as a string) — the type assertion to `bool`
// below fails the same way absence does.
//
// KNOWN RESIDUAL: this detects a rename/re-casing of the FIELD NAME or a
// change to a non-bool wire TYPE. It does NOT detect Zitadel silently
// changing the MEANING of `forceMfa` while keeping both its name and its
// bool type (e.g. inverting the polarity to "allowPasswordOnly"-shaped
// semantics under the same key) — no purely structural check can catch a
// semantic change with an unchanged shape. That gap is why Finding 2's
// live integration test (loginui_integration_test.go,
// TestIntegration_ForceMFAPolicy_HandsOffInsteadOfCompleting) exists
// alongside this structural guard rather than instead of it: a real
// forceMfa=true policy read through the real decode path is the only
// thing that can prove the MEANING, not just the shape, still holds.
// TestLoginPolicyRejectsBodiesItCannotUnderstand,
// TestLoginPolicyTreatsAbsentForceMFAAsFalseWhenPolicyIsRecognizable, and
// TestLoginPolicyRejectsARenamedOrRecasedForceMFA pin this file's half.
func (c *Client) LoginPolicy(ctx context.Context) (LoginPolicy, error) {
	var wire struct {
		Policy map[string]any `json:"policy"`
	}
	if err := c.do(ctx, http.MethodGet, "/management/v1/policies/login", nil, &wire, ErrUnavailable); err != nil {
		return LoginPolicy{}, err
	}
	if _, anchored := wire.Policy["passwordCheckLifetime"]; !anchored {
		// ErrUnavailable rather than a new sentinel: from the caller's
		// point of view an answer it cannot interpret and no answer at all
		// are the same situation — Zitadel did not tell HMS whether MFA is
		// required — and both must reach the same fail-closed branch.
		return LoginPolicy{}, fmt.Errorf("GET /management/v1/policies/login: 200 without a recognizable policy object: %w", ErrUnavailable)
	}
	for key := range wire.Policy {
		if key == "forceMfa" {
			continue
		}
		if normalizePolicyKey(key) == "forcemfa" {
			return LoginPolicy{}, fmt.Errorf(
				"GET /management/v1/policies/login: policy object has a field %q that looks like a "+
					"renamed or re-cased forceMfa but is not spelled exactly that way: %w", key, ErrUnavailable)
		}
	}
	rawForceMFA, present := wire.Policy["forceMfa"]
	if !present {
		return LoginPolicy{ForceMFA: false}, nil
	}
	forceMFA, isBool := rawForceMFA.(bool)
	if !isBool {
		return LoginPolicy{}, fmt.Errorf(
			"GET /management/v1/policies/login: policy.forceMfa is %T, not a bool: %w", rawForceMFA, ErrUnavailable)
	}
	return LoginPolicy{ForceMFA: forceMFA}, nil
}

// normalizePolicyKey collapses a JSON object key to the form
// LoginPolicy's rename/re-casing check compares against: lowercased,
// underscores stripped. "forceMfa", "force_mfa", "ForceMFA", and
// "FORCE_MFA" all normalize to "forcemfa"; unrelated keys
// ("allowUsernamePassword", "passwordCheckLifetime") do not.
func normalizePolicyKey(key string) string {
	return strings.ToLower(strings.ReplaceAll(key, "_", ""))
}

// zitadelError is the subset of Zitadel's gRPC-gateway error envelope this
// client needs to extract an error id for logging — see the spike §3 for
// the observed shapes. details[].id carries e.g. "COMMAND-3M0fs" or
// "QUERY-Dfbg2"; details[].failedAttempts is deliberately NOT decoded into
// any field this package keeps, let alone put in an error string —
// TestCreatePasswordSessionMapsWrongPasswordToErrBadCredentials pins that
// it never reaches err.Error().
type zitadelError struct {
	Message string `json:"message"`
	Details []struct {
		ID string `json:"id"`
	} `json:"details"`
}

// do issues one request against the Zitadel API and decodes a 2xx JSON
// response into out (skipped entirely when out is nil, for endpoints
// whose response body carries nothing the caller needs). notFound is the
// sentinel a 404 response maps to; it varies per call (ErrUserNotFound
// for the session endpoint, ErrAuthRequestInvalid for the auth-request
// endpoints, ErrUnavailable for the policy endpoint) because the SAME
// status code means a different failure depending on which resource was
// being addressed.
//
// The response body is parsed only far enough to pull an error id via
// readZitadelErrorID on every non-2xx path — it is never embedded raw —
// so no path in this package can accidentally surface a
// credential-adjacent detail (like failedAttempts) in a returned error
// string.
func (c *Client) do(ctx context.Context, method, path string, body, out any, notFound error) error {
	var reqBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("loginclient: encode request: %w", err)
		}
		reqBody = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("loginclient: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		// A transport failure (timeout, connection refused, DNS) is
		// Zitadel being unreachable, not a refusal — ErrUnavailable, not
		// one of the credential sentinels.
		return fmt.Errorf("%s %s: %w: %w", method, path, ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errID := readZitadelErrorID(resp.Body)
		switch resp.StatusCode {
		case http.StatusBadRequest:
			return fmt.Errorf("%s %s: status %d id=%s: %w", method, path, resp.StatusCode, errID, ErrBadCredentials)
		case http.StatusNotFound:
			return fmt.Errorf("%s %s: status %d id=%s: %w", method, path, resp.StatusCode, errID, notFound)
		default:
			// Covers 5xx and any other unexpected status (e.g. 401/403 —
			// a login client PAT problem is an operational failure HMS
			// cannot resolve per-request, not a credential refusal for
			// the end user).
			return fmt.Errorf("%s %s: status %d id=%s: %w", method, path, resp.StatusCode, errID, ErrUnavailable)
		}
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxSuccessBodyBytes)).Decode(out); err != nil {
		return fmt.Errorf("loginclient: decode response: %w", err)
	}
	return nil
}

// readZitadelErrorID reads and best-effort parses a Zitadel error body,
// returning just the error id (e.g. "COMMAND-3M0fs") for logging — never
// the raw body, which is exactly where failedAttempts lives (spike §3). A
// body that fails to parse, or carries no details/id, yields "" rather
// than an error: this is best-effort log enrichment, not something a
// caller should be able to fail on.
func readZitadelErrorID(r io.Reader) string {
	limited := io.LimitReader(r, 4096)
	var parsed zitadelError
	if err := json.NewDecoder(limited).Decode(&parsed); err != nil {
		return ""
	}
	if len(parsed.Details) > 0 {
		return parsed.Details[0].ID
	}
	return ""
}
