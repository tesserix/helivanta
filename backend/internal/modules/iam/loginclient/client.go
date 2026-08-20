// Package loginclient speaks the Zitadel v2 "login client" HTTP calls
// Helivanta's own login page needs to drive a session end to end: read an OIDC
// auth request, create a password-checked session, finalize the auth
// request into a callback URL, read the org login policy, and (Task 8)
// read which authentication methods a user has enrolled. It makes NO
// authorization decision — whether a session is SUFFICIENT to finalize
// (e.g. whether MFA is required and present) lives in sufficiency.go
// (Task 3), not here. This package only knows how to make the calls and
// translate their observed error shapes into typed sentinels; it has no
// opinion on what a caller should do with them.
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
	// HTTP 404, QUERY-Dfbg2). UserState also passes this as do's notFound
	// sentinel for GET /v2/users/{id}, but never lets it escape as an
	// error to its own caller — a 404 there means the user was deleted
	// upstream, which UserState maps to UserStateInactive with a nil
	// error instead (see UserState's doc comment for why that collapses
	// cleanly with an explicit USER_STATE_INACTIVE).
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
// a stalled Zitadel hang Helivanta's login handler indefinitely.
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

// New builds a Client against baseURL (Zitadel's own origin, not Helivanta's),
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
// Zitadel returns that Helivanta has no use for (e.g. prompt, app) are dropped
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
// so Helivanta must read this and enforce it structurally.
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
// It is UNEXPORTED on purpose, and that is the structural half of D4
// (docs/superpowers/specs/2026-08-16-hms-login-client-design.md). The
// spike §2 proved Zitadel issues an authorization code for a
// password-only session even under a forceMfa policy — it does not
// enforce MFA for a login client at all. So whether a session is
// sufficient to finalize is Helivanta's decision, and the only way to reach
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
//
// # The _ sufficient parameter — #867 fix round 1
//
// finalize being unexported only stops a caller OUTSIDE this package;
// nothing stopped a future function INSIDE this package from calling
// finalize directly, skipping every check CompleteIfSufficient and
// CompleteAfterFactor run (sufficiency.go). The required parameter of the
// unexported type `sufficient` makes forgetting that check a COMPILE
// ERROR rather than a gap a reviewer has to notice: a new function that
// calls `c.finalize(ctx, id, s)` with no third argument does not compile,
// full stop — see sufficiency.go's doc comment on `sufficient` for the
// honest limit of what this actually proves (it stops ACCIDENTAL
// omission; it does NOT stop a determined caller writing `sufficient{}`
// by hand, which is a decorative-vs-real distinction this repo's
// standards require stating plainly rather than glossing over).
func (c *Client) finalize(ctx context.Context, authRequestID string, s Session, _ sufficient) (string, error) {
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

// VerifyTOTP checks a TOTP code against an existing session (PATCH
// /v2/sessions/{id}, body shape from the MFA spike §1 —
// docs/superpowers/spikes/2026-08-17-zitadel-login-client-mfa.md, a
// SEPARATE document from "the spike" this file's package doc anchors to
// (docs/superpowers/spikes/2026-08-16-zitadel-login-client.md); every
// citation in this method and SessionFactors below means the MFA spike,
// stated explicitly because both documents number their sections
// independently starting at §1). id is escaped with
// url.PathEscape before being placed in the URL for the same reason as
// finalize's authRequestID — but here the reasoning is defensive rather
// than required: s.ID reaches this method from a login_attempt row this
// server itself wrote (Task 1), not from a browser-supplied query
// parameter, so it is not attacker-influenced the way finalize's id is.
// Escaping stays anyway for consistency with every other id-in-path call
// in this file.
//
// PATCH is the correct method here — POST to a session id returns 405
// (MFA spike §3, confirmed live). The 405 was hit by mistake once during
// the spike, which is exactly why this comment calls it out.
//
// # The token rotates on every check — MFA spike §2
//
// The response to a successful PATCH carries a NEW sessionToken, not the
// one that authenticated the request. This is the single most important
// fact in this method: finalize (and any future check against this
// session) needs session:{sessionId, sessionToken} with the NEWEST token,
// so VerifyTOTP returns Session{ID: s.ID, Token: <token from THIS
// response>} — never the input s. Returning the input session keeps the
// stale creation token and makes finalize fail AFTER the clinician has
// already entered a correct code, which reads to them as "my TOTP was
// wrong" rather than the actual cause. TestVerifyTOTP_ReturnsTheRotatedToken
// pins this by making the fake Zitadel return a token that differs from
// the one it was given.
//
// A rejected code maps to the same ErrBadCredentials a wrong password
// does (observed: HTTP 400) — deliberately: callers cannot tell a wrong
// TOTP code from a wrong password by error type alone. The code itself,
// and both the current and rotated tokens, are credentials and must never
// reach an error string — the same discipline every other method in this
// file already keeps.
func (c *Client) VerifyTOTP(ctx context.Context, s Session, code string) (Session, error) {
	body := map[string]any{
		"sessionToken": s.Token,
		"checks": map[string]any{
			"totp": map[string]any{"code": code},
		},
	}
	var wire struct {
		SessionToken string `json:"sessionToken"`
	}
	if err := c.do(ctx, http.MethodPatch, "/v2/sessions/"+url.PathEscape(s.ID), body, &wire, ErrBadCredentials); err != nil {
		return Session{}, err
	}
	return Session{ID: s.ID, Token: wire.SessionToken}, nil
}

// Factors reports which authentication factors a session has actually
// verified — as opposed to enrolledMethodTypes, which reports what the
// user has configured. An absent factor decodes to false, not an error: a
// password-only session (TOTP absent) is a legitimate, expected state
// mid-login, and Task 3's sufficiency decision depends on being able to
// tell that apart from a session that has verified TOTP.
type Factors struct {
	Password bool
	TOTP     bool
}

// SessionFactors reads GET /v2/sessions/{id} (MFA spike §1 — see
// VerifyTOTP's doc comment for which document "MFA spike" means and why
// that has to be stated explicitly here) and reports which factors it has
// verified. sessionID is escaped with url.PathEscape for the same
// defensive-not-required reason as VerifyTOTP's id.
func (c *Client) SessionFactors(ctx context.Context, sessionID string) (Factors, error) {
	var wire struct {
		Session struct {
			Factors struct {
				Password *struct {
					VerifiedAt string `json:"verifiedAt"`
				} `json:"password"`
				TOTP *struct {
					VerifiedAt string `json:"verifiedAt"`
				} `json:"totp"`
			} `json:"factors"`
		} `json:"session"`
	}
	if err := c.do(ctx, http.MethodGet, "/v2/sessions/"+url.PathEscape(sessionID), nil, &wire, ErrUnavailable); err != nil {
		return Factors{}, err
	}
	return Factors{
		Password: wire.Session.Factors.Password != nil,
		TOTP:     wire.Session.Factors.TOTP != nil,
	}, nil
}

// loginPolicy reads a login policy (GET /management/v1/policies/login),
// scoped by whatever opts the caller supplies — org-scoped via withOrgID,
// or unscoped (the login client PAT's own resource owner) when opts is
// empty. It is unexported: the two exported wrappers below,
// LoginPolicyForOrg and InstanceLoginPolicyForDisplay, are the only ways to
// reach it, so the body-parsing, anchor-check and rename-guard behaviour
// documented below cannot drift between the enforcement path and the
// display-only path (D3) — both go through the exact same decode. There is
// no meaningful 404 case for this endpoint — a login policy always exists
// — so a 404 here falls through to ErrUnavailable rather than being given
// a dedicated sentinel.
//
// Every error path returns a zero LoginPolicy{} alongside a non-nil
// error, and NEVER a zero value with err == nil: ForceMFA's zero value is
// false, which reads as "no MFA required". D4's fail-closed requirement
// (docs/superpowers/specs/2026-08-16-hms-login-client-design.md) means a
// caller that cannot read this policy must be able to
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
// `policy.passwordCheckLifetime` is a `google.protobuf.Duration` field.
// It has been present on every response observed live against v4.15.3,
// including every shape this file's tests construct from real captures —
// that is the actual basis for using it, not a claim about protojson's
// general semantics for message-typed fields (an earlier version of this
// comment overstated that guarantee; see the review round that caught
// it). If it were ever absent, the result is this function refusing with
// ErrUnavailable — the SAME fail-closed branch an unrecognized body
// already takes — not a bypass, so the anchor's failure mode stays safe
// even if the "always present" premise it rests on ever turns out to be
// wrong. Its presence is evidence "this really is Zitadel's login policy
// object", independent of what any individual boolean field happens to
// be set to — including, notably, an org that has legitimately set
// `allowUsernamePassword` to false, which an anchor built on THAT field
// would have misread as unrecognized. `forceMfa`/`forceMfaLocalOnly`
// absent alongside the anchor PRESENT decodes to false; the anchor
// absent (with either present or not) still refuses, exactly as the
// original design did for every shape it covered.
//
// # Rename/re-casing detection — closes the residual the anchor alone leaves open
//
// The anchor closes "forceMfa absent because it is false". It does
// nothing for "forceMfa absent because Zitadel renamed or re-cased it" —
// `{"policy":{"passwordCheckLifetime":"864000s","force_mfa":true}}`
// anchors as recognized AND has no field literally named "forceMfa", so
// naively reading `wire.Policy["forceMfa"]` as absent-therefore-false
// would silently complete a login Zitadel is telling Helivanta requires MFA.
// That is the exact fail-open Task 3 was written to close, re-opened by
// the anchor fix above if nothing else changed.
//
// The policy object is therefore decoded into a `map[string]any`
// (rather than a fixed struct) so every key Zitadel actually sent is
// visible, and every key that matters (`forceMfa`, `forceMfaLocalOnly`)
// is checked: any OTHER key whose name NORMALIZES (lowercased,
// underscores stripped) to the SAME normalized form but is not spelled
// exactly that way is treated as unrecognized and refuses — e.g.
// "force_mfa" and "ForceMFA" both collide with "forceMfa"'s normalized
// form and refuse, but "forceMfaLocalOnly" normalizes to
// "forcemfalocalonly", which does NOT collide with "forceMfa"'s
// "forcemfa", so a real, unrelated `forceMfaLocalOnly` field is never
// mistaken for a renamed `forceMfa` (or vice versa) — verified live:
// setting only `forceMfaLocalOnly` does not trip `forceMfa`'s guard, and
// setting only `forceMfa` does not trip `forceMfaLocalOnly`'s. This also
// catches either field changing TYPE (e.g. a future Zitadel encoding it
// as a string) — the type assertion to `bool` fails the same way
// absence does.
//
// # forceMfaLocalOnly — a second, REAL field with the same shape of gap
//
// Verified live 2026-08-16 (this task's second fix round): setting
// `forceMfaLocalOnly:true` with `forceMfa:false` (a normal Zitadel
// configuration — "require MFA for local/password users, not for
// federated ones") answers with `forceMfa` elided entirely (per the
// section above) and `forceMfaLocalOnly:true` present. Reading only
// `forceMfa` therefore missed a REAL, supported way to require MFA — not
// a hypothetical drift, a config an operator can set today. Helivanta folds
// `forceMfaLocalOnly` into the SAME ForceMFA bool
// (`forceMfa || forceMfaLocalOnly`) rather than modeling it separately,
// on a documented, narrow assumption: `forceMfaLocalOnly` strictly means
// "force MFA for non-federated (local) users", and EVERY Helivanta user is
// local today — no external IdP is configured (spec D5) — so for Helivanta's
// purposes the two fields currently mean the same thing. This is an
// assumption, not a derived fact: if Helivanta ever configures an external
// IdP, this fold-together stops being correct for federated users and
// must be revisited (a future CompleteIfSufficient would need to know
// which kind of session it is evaluating, not just read one bool). Not
// built now because Helivanta has no federated login path to get it wrong on
// yet — see docs/standards/engineering-principles.md on not building for
// a case that cannot occur.
//
// KNOWN RESIDUAL: this detects a rename/re-casing of either FIELD NAME,
// a change to either field's non-bool wire TYPE, and (as of this
// section) `forceMfaLocalOnly` overriding `forceMfa`'s apparent "off"
// value through a name this file already knows to read. It does NOT
// detect two other classes: (1) Zitadel silently changing the MEANING of
// `forceMfa`/`forceMfaLocalOnly` while keeping both name and bool type
// (e.g. inverting the polarity under the same key), and (2) a THIRD,
// not-yet-discovered field overriding MFA requirements the way
// `forceMfaLocalOnly` turned out to — this file only knows to guard the
// two fields discovered so far, and a structural check cannot enumerate
// fields it has never been told about. No purely structural check can
// close either gap. That is why Finding 2's live integration tests
// (loginui_integration_test.go,
// TestIntegration_ForceMFAPolicy_HandsOffInsteadOfCompleting and
// TestIntegration_ForceMFALocalOnlyPolicy_HandsOffInsteadOfCompleting)
// exist alongside this structural guard rather than instead of it: a
// real policy read through the real decode path is the only thing that
// can prove the MEANING, not just the shape, still holds — and a THIRD
// override field would show up there as a completed login when handoff
// was expected, the same way `forceMfaLocalOnly` itself was found.
// TestLoginPolicyRejectsBodiesItCannotUnderstand,
// TestLoginPolicyTreatsAbsentForceMFAAsFalseWhenPolicyIsRecognizable,
// TestLoginPolicyRejectsARenamedOrRecasedForceMFA, and
// TestLoginPolicyTreatsForceMFALocalOnlyAsRequiringMFA pin this file's
// half.
func (c *Client) loginPolicy(ctx context.Context, opts ...requestOption) (LoginPolicy, error) {
	var wire struct {
		Policy map[string]any `json:"policy"`
	}
	if err := c.do(ctx, http.MethodGet, "/management/v1/policies/login", nil, &wire, ErrUnavailable, opts...); err != nil {
		return LoginPolicy{}, err
	}
	if _, anchored := wire.Policy["passwordCheckLifetime"]; !anchored {
		// ErrUnavailable rather than a new sentinel: from the caller's
		// point of view an answer it cannot interpret and no answer at all
		// are the same situation — Zitadel did not tell Helivanta whether MFA is
		// required — and both must reach the same fail-closed branch.
		return LoginPolicy{}, fmt.Errorf("GET /management/v1/policies/login: 200 without a recognizable policy object: %w", ErrUnavailable)
	}

	// ONE loop over mfaPolicyKeys does BOTH halves for every key — the
	// rename guard AND the read — so that adding a third MFA-forcing key
	// to that list is genuinely a one-place change. An earlier version
	// iterated the list for the rename guard only and then called
	// readMFABool twice with hardcoded literals; a third key added to
	// the list would have been guarded against renames but NEVER read
	// into ForceMFA, which is a silent MFA bypass in the one file
	// written to prevent exactly that. TestLoginPolicyReadsEveryKeyIn
	// MFAPolicyKeys pins that the read really is list-driven.
	//
	// The keys are OR-ed together: forceMfaLocalOnly folds into the SAME
	// ForceMFA bool as forceMfa — see this function's doc comment
	// ("forceMfaLocalOnly — a second, REAL field") for why that is safe
	// today and what would have to change if it stops being safe. Any
	// third key discovered later is, by construction, folded the same
	// way; if a future key ever needs DIFFERENT semantics than "true
	// means MFA is forced", it does not belong in this list at all.
	forceMFA := false
	for _, key := range mfaPolicyKeys {
		if err := refuseIfKeyRenamedOrRecased(wire.Policy, key); err != nil {
			return LoginPolicy{}, err
		}
		forced, err := readMFABool(wire.Policy, key)
		if err != nil {
			return LoginPolicy{}, err
		}
		forceMFA = forceMFA || forced
	}
	return LoginPolicy{ForceMFA: forceMFA}, nil
}

// LoginPolicyForOrg reads orgID's login policy, scoped with the
// x-zitadel-orgid header (D4/D2 of
// docs/superpowers/specs/2026-08-20-login-policy-org-scope-design.md).
// This is the ENFORCEMENT path — the login policy this package treats as
// authoritative for a real login decision must resolve against the
// authenticating user's own org, never against the login client PAT's own
// resource owner. That distinction is the entire fix for #913: an
// unscoped read let a user in one org be judged by a different org's
// policy, and completed a password-only login that org's own policy
// required MFA for.
//
// orgID == "" refuses BEFORE any HTTP request is made, returning an error
// wrapping ErrUnavailable, rather than falling back to an unscoped read.
// A fallback is exactly the bug this method exists to close, and it would
// reproduce it for precisely the callers hardest to notice — whichever
// caller failed to determine an org id (a session response whose shape
// drifted, say) would silently get today's wrong-org answer back instead
// of a visible failure. ErrUnavailable rather than a new sentinel: from
// the caller's point of view "no org id to scope this with" and "Zitadel
// did not answer" are the same situation — Helivanta cannot say whether
// MFA is required — and both must reach CompleteIfSufficient's existing
// fail-closed branch (spec D2).
func (c *Client) LoginPolicyForOrg(ctx context.Context, orgID string) (LoginPolicy, error) {
	if orgID == "" {
		return LoginPolicy{}, fmt.Errorf("loginclient: LoginPolicyForOrg called with an empty org id, refusing rather than reading an unscoped policy: %w", ErrUnavailable)
	}
	return c.loginPolicy(ctx, withOrgID(orgID))
}

// InstanceLoginPolicyForDisplay reads the login-client PAT's own resource
// owner's login policy, UNSCOPED — no x-zitadel-orgid header. It exists
// for exactly one caller today: loginui.go's AuthRequest handler, which
// reads the policy BEFORE the user has typed a login name (spec D3), so
// there is no user org yet to scope this to. That is not an omission —
// there is no correct value to pass at that point in the flow.
//
// KNOWN LIMITATION (spec D3): on a multi-org instance this can resolve
// against the wrong org, and the login form may then advertise "no MFA"
// to a user whose real org forces it. That is a cosmetic wrong hint, not
// a bypass: the enforcement decision is made by LoginPolicyForOrg, called
// from CompleteIfSufficient now that the user's actual org is known, and
// that call always resolves against the right org regardless of what this
// method told the form. CompleteIfSufficient no longer calls this method
// at all — Task 2 (#913) replaced that call — so the enforcement path is
// fully org-scoped today; only this display read remains unscoped. This
// is exactly today's (pre-#913-fix) behaviour for the display case, so
// this change makes it no worse there. Making the display read org-aware
// needs a login-form flow change — re-reading the policy once the login
// name is known — and is filed as a follow-up rather than done here: see
// the design spec's "Out of scope" section and follow-up issue #917.
//
// The name is deliberately unmistakable for the enforcer's: a future
// contributor reaching for A login policy inside sufficiency.go must not
// be able to grab this unscoped one by accident. sufficiency.go's
// CompleteIfSufficient no longer calls this method at all — Task 2 (#913)
// replaced that call with LoginPolicyForOrg(ctx, subject.OrgID), once
// classifyEnrolledMethods started returning the session's org id
// alongside its two booleans — and Task 3 adds the archtest that forbids
// this method from ever being referenced by sufficiency.go again, so a
// future contributor who reaches for it there fails CI, not review.
func (c *Client) InstanceLoginPolicyForDisplay(ctx context.Context) (LoginPolicy, error) {
	return c.loginPolicy(ctx)
}

// nonPasswordFactorPrefix is what an enrolled Zitadel authentication
// method type looks like when it is NOT the password itself —
// AUTHENTICATION_METHOD_TYPE_PASSWORD is the one value
// classifyEnrolledMethods (sufficiency.go) must ignore; every other
// observed value (AUTHENTICATION_METHOD_TYPE_
// OTP_EMAIL, _TOTP, _U2F, _PASSKEY, _IDP, _OTP_SMS, _RECOVERY_CODE — see
// GET /v2/users/{id}/authentication_methods, verified live 2026-08-16
// against v4.15.3 with the login-client PAT, spike §"Per-user enrolled
// factors") means the user configured something a password-only session
// cannot satisfy.
const passwordOnlyMethodType = "AUTHENTICATION_METHOD_TYPE_PASSWORD"

// totpMethodType is the one non-password enrolled method type Helivanta
// can natively ask a user to satisfy — VerifyTOTP (Task 2) checks exactly
// this factor. Every other non-password value in nonPasswordFactorPrefix's
// list (_OTP_EMAIL, _U2F, _PASSKEY, _IDP, _OTP_SMS, _RECOVERY_CODE) has no
// corresponding collection path in Helivanta today, so its presence — even
// alongside an enrolled TOTP — must still hand off to Zitadel's hosted UI
// (spec D1): completing on the strength of the one factor Helivanta CAN
// collect would silently skip the other one the user configured.
const totpMethodType = "AUTHENTICATION_METHOD_TYPE_TOTP"

// sessionSubject is who a session's password factor authenticated, and
// which org they authenticate as (design spec D1,
// docs/superpowers/specs/2026-08-20-login-policy-org-scope-design.md).
// Both fields come off the SAME GET /v2/sessions/{id} response
// (factors.user.{id,organizationId}) that classifyEnrolledMethods
// (sufficiency.go) already has to read to learn who to ask about enrolled
// methods — carrying OrgID alongside UserID here, rather than reading it
// separately, is what keeps CompleteIfSufficient's org-scoped policy read
// (#913) from costing a second Zitadel round trip per login.
//
// OrgID is NOT validated for emptiness anywhere this type is produced
// (see the sessionSubject method's doc comment) — only UserID is, because
// only UserID has every caller of this type depending on it being
// non-empty. Validating OrgID here too would duplicate the ONE place that
// actually needs to refuse an empty org id, LoginPolicyForOrg (spec D2):
// CompleteAfterFactor's call path never reads a policy at all and would
// pay for a check its own caller has no use for.
type sessionSubject struct {
	UserID string
	OrgID  string
}

// sessionSubject reads GET /v2/sessions/{id} to recover the session's
// user id and org id — CreateSessionResponse (POST /v2/sessions) does
// NOT carry either (confirmed against the v4.15.3 proto and live: the
// create response is only {details, sessionId, sessionToken}), so
// classifyEnrolledMethods (sufficiency.go) needs this extra round trip to
// learn both. Verified live: the login-client PAT alone (no session
// token) is sufficient to read an arbitrary session it created
// (2026-08-16), and that same response carries
// factors.user.organizationId beside factors.user.id (2026-08-19, #913 —
// the spike had recorded only `factors: {user, password}` and never this
// field, which is why it was believed absent when sufficiency.go's KNOWN
// LIMITATIONS §2 was first written).
//
// Only UserID is checked for emptiness and fails closed with
// ErrUnavailable, matching the fail-closed contract every other read in
// this file has for its callers (see enrolledMethodTypes's own doc
// comment): an error here must mean "cannot prove this session is
// sufficient", not "no user found". OrgID is returned exactly as the wire
// reported it, including empty — deliberately unchecked here; see
// sessionSubject's (the type's) doc comment for why.
func (c *Client) sessionSubject(ctx context.Context, sessionID string) (sessionSubject, error) {
	var wire struct {
		Session struct {
			Factors struct {
				User struct {
					ID             string `json:"id"`
					OrganizationID string `json:"organizationId"`
				} `json:"user"`
			} `json:"factors"`
		} `json:"session"`
	}
	if err := c.do(ctx, http.MethodGet, "/v2/sessions/"+url.PathEscape(sessionID), nil, &wire, ErrUnavailable); err != nil {
		return sessionSubject{}, err
	}
	if wire.Session.Factors.User.ID == "" {
		return sessionSubject{}, fmt.Errorf("GET /v2/sessions/%s: no factors.user.id in response: %w", sessionID, ErrUnavailable)
	}
	return sessionSubject{
		UserID: wire.Session.Factors.User.ID,
		OrgID:  wire.Session.Factors.User.OrganizationID,
	}, nil
}

// enrolledMethodTypes reads GET /v2/users/{id}/authentication_methods,
// verified live 2026-08-16 against v4.15.3 with the login-client PAT:
// enrolling OTP_EMAIL on a test user made authMethodTypes read
// ["AUTHENTICATION_METHOD_TYPE_OTP_EMAIL","AUTHENTICATION_METHOD_TYPE_PASSWORD"],
// where a password-only user reads just
// ["AUTHENTICATION_METHOD_TYPE_PASSWORD"]. This is DIFFERENT from what a
// session's own `factors` report (session.proto's Factors is what was
// CHECKED in this one session, not what the user has available) — a
// password-only session always has just a password factor even when the
// user separately enrolled TOTP, which is exactly the gap
// classifyEnrolledMethods (sufficiency.go) closes for both
// CompleteIfSufficient and CompleteAfterFactor. (An earlier version of
// this comment pointed at "sufficiency.go's KNOWN LIMITATIONS §1"; that
// section is password-change-required, an unrelated and still-OPEN gap
// tracked as #856. This gap is CLOSED, so it is not in that list at all.)
//
// Takes userID rather than a session id: classifyEnrolledMethods already
// resolves the session to a sessionSubject (this file's sessionSubject
// method) to learn the org id CompleteIfSufficient needs, so this method
// no longer needs to make that same GET /v2/sessions/{id} call itself —
// doing so would be a second, redundant session read per login.
//
// This exists to be called from CompleteIfSufficient's and
// CompleteAfterFactor's fail-closed paths: any error here (transport
// failure, unreadable body) must read as "cannot prove the session is
// sufficient", not "no factor found" — so it always returns a non-nil
// error alongside a nil slice rather than ever answering an empty list by
// swallowing a failure. Callers must hand off, not complete, when err !=
// nil.
//
// There used to be a HasEnrolledFactor wrapper here that only answered
// "anything besides password?" as a bool. #867 fix round 1 removed it:
// once CompleteIfSufficient needed to tell TOTP apart from every other
// enrolled type (not just "enrolled vs not"), the bool-returning wrapper
// had no remaining caller — sufficiency.go's classifyEnrolledMethods reads
// this method's slice directly instead of going through an intermediate
// that would have thrown the distinction away.
func (c *Client) enrolledMethodTypes(ctx context.Context, userID string) ([]string, error) {
	var wire struct {
		AuthMethodTypes []string `json:"authMethodTypes"`
	}
	if err := c.do(ctx, http.MethodGet, "/v2/users/"+url.PathEscape(userID)+"/authentication_methods", nil, &wire, ErrUnavailable); err != nil {
		return nil, err
	}
	return wire.AuthMethodTypes, nil
}

// UserState is the answer to "is this subject still active upstream in
// Zitadel", modeled as its own type rather than a bare bool on purpose —
// see the UserState method's doc comment below for the full argument. Its
// zero value, userStateUnset, is never returned alongside a nil error by
// that method; only UserStateActive and UserStateInactive are, and
// IsActive is defined for every value so a caller that forgets to check
// the error still gets "not active" rather than a false "active" out of
// the zero value — the same fail-closed shape LoginPolicy's ForceMFA
// established for this package (see loginPolicy's doc comment) applied to
// a type that cannot silently zero-value its way to "active" the way a
// bool could.
type UserState int

const (
	// userStateUnset is UserState's zero value — unexported because no
	// caller outside this package has any legitimate reason to construct
	// or compare against it; every path that can produce it also produces
	// a non-nil error, so a caller only ever observes it by mishandling
	// that error.
	userStateUnset UserState = iota
	// UserStateActive is USER_STATE_ACTIVE, verified live against dev
	// Zitadel v4.15.3 on 2026-08-20 as the state a normal user reads.
	UserStateActive
	// UserStateInactive covers both USER_STATE_INACTIVE (verified live:
	// deactivating a user via POST /v2/users/{id}/deactivate flips the
	// same GET this method makes from USER_STATE_ACTIVE to this value)
	// and a 404 from the same GET (the user was deleted upstream, not
	// merely deactivated) — see the UserState method's doc comment for
	// why those two upstream conditions are deliberately collapsed into
	// one caller-visible answer rather than kept apart.
	UserStateInactive
)

// IsActive reports whether s represents a subject Zitadel still considers
// active. It is false for UserStateInactive AND for the unexported zero
// value userStateUnset — the latter matters only if a caller manages to
// observe a UserState without checking the UserState method's error
// return first, which should not happen, but IsActive still answers
// "not active" rather than panicking or, worse, reading true, if it does.
func (s UserState) IsActive() bool {
	return s == UserStateActive
}

// UserState reads GET /v2/users/{id} with the login-client PAT and reports
// whether id is still active upstream. Verified live against dev Zitadel
// v4.15.3 on 2026-08-20: the login-client PAT is permitted for this read
// (HTTP 200, no new credential needed — the same PAT every other method in
// this file already uses); the response nests state on the user object
// ({"user":{"userId":...,"state":...,"username":...,"human":{...}}});
// a normal user's read is "USER_STATE_ACTIVE"; and deactivating that same
// user (POST /v2/users/{id}/deactivate) flips the SAME read to
// "USER_STATE_INACTIVE" — the live proof that this check can actually
// fail, which is what makes it a control and not decoration. This is the
// one new Zitadel read server-side renewal needs (design spec D1/D3,
// docs/superpowers/specs/2026-08-20-server-side-session-renewal-design.md):
// today's browser-driven renewal never re-checks this at all, because
// Zitadel's session cookie is SameSite=Lax and the hidden iframe that was
// supposed to carry it never sends it (spec D2).
//
// id is escaped with url.PathEscape before being placed in the URL, same
// as every other id-in-path call in this file (see AuthRequest's doc
// comment for the escaping rationale) — defensive here, not required: id
// reaches this method from this server's own session/principal state, not
// a browser-supplied parameter.
//
// A 404 maps to UserStateInactive, with a NIL error, not to ErrUnavailable
// — a 404 here means the user was deleted upstream, and a deleted user
// must never renew any more than an explicitly deactivated one does; that
// answer is exactly as definite as an explicit USER_STATE_INACTIVE, so it
// gets the same caller-visible value rather than being folded into the
// "cannot tell" error branch below.
//
// Every OTHER failure path — transport error, a 5xx, a 200 body with no
// recognizable state field, or a state string this method does not
// recognize — returns userStateUnset alongside a non-nil error wrapping
// ErrUnavailable, never UserStateActive and never UserStateInactive. This
// is the crux of the fail-closed contract the brief calls for: an
// unreadable answer is not evidence of "not active" any more than it is
// evidence of "active" — collapsing it into UserStateInactive would let a
// caller that only calls IsActive() (and does not separately check err)
// end an active clinician's session on a transient Zitadel blip, while
// collapsing it into UserStateActive is the literal authentication bypass
// this method exists to prevent. Treating an unrecognized state string
// (Zitadel could add one) as an error rather than as active follows the
// same reasoning: assuming an unknown value is fine is exactly the wrong
// default for the one call standing between a deactivated clinician and a
// live session.
func (c *Client) UserState(ctx context.Context, id string) (UserState, error) {
	var wire struct {
		User struct {
			State string `json:"state"`
		} `json:"user"`
	}
	if err := c.do(ctx, http.MethodGet, "/v2/users/"+url.PathEscape(id), nil, &wire, ErrUserNotFound); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return UserStateInactive, nil
		}
		return userStateUnset, err
	}
	switch wire.User.State {
	case "USER_STATE_ACTIVE":
		return UserStateActive, nil
	case "USER_STATE_INACTIVE":
		return UserStateInactive, nil
	case "":
		return userStateUnset, fmt.Errorf("GET /v2/users/%s: 200 response with no recognizable state field: %w", id, ErrUnavailable)
	default:
		return userStateUnset, fmt.Errorf("GET /v2/users/%s: unrecognized state %q: %w", id, wire.User.State, ErrUnavailable)
	}
}

// mfaPolicyKeys is every wire key loginPolicy reads to decide ForceMFA —
// forceMfa and forceMfaLocalOnly today. loginPolicy's single loop over
// this list does BOTH the rename guard (refuseIfKeyRenamedOrRecased) and
// the read (readMFABool) for every entry, and OR-s the results into
// ForceMFA, so a THIRD MFA-forcing field discovered later (see the KNOWN
// RESIDUAL section above) really is added in exactly one place: appending
// it here both guards and reads it. TestLoginPolicyReadsEveryKeyIn
// MFAPolicyKeys is the CI half of that claim — it appends a third key to
// this list and asserts a policy body setting only that key produces
// ForceMFA:true, so a future edit that re-hardcodes the reads fails
// rather than silently dropping the new key.
//
// Every entry must mean "true forces MFA"; a hypothetical future policy
// field with different semantics (e.g. one that RELAXES a requirement)
// cannot be expressed by appending to this list and must not be.
var mfaPolicyKeys = []string{"forceMfa", "forceMfaLocalOnly"}

// refuseIfKeyRenamedOrRecased scans policy for any key that NORMALIZES
// (normalizePolicyKey) to the same form as wantKey but is not spelled
// exactly wantKey — see loginPolicy's "Rename/re-casing detection" doc
// comment for the full reasoning and the live verification that this
// does not false-positive between forceMfa and forceMfaLocalOnly (their
// normalized forms, "forcemfa" and "forcemfalocalonly", differ).
func refuseIfKeyRenamedOrRecased(policy map[string]any, wantKey string) error {
	want := normalizePolicyKey(wantKey)
	for key := range policy {
		if key == wantKey {
			continue
		}
		if normalizePolicyKey(key) == want {
			return fmt.Errorf(
				"GET /management/v1/policies/login: policy object has a field %q that looks like a "+
					"renamed or re-cased %s but is not spelled exactly that way: %w", key, wantKey, ErrUnavailable)
		}
	}
	return nil
}

// readMFABool reads policy[key] as the bool loginPolicy needs it to be:
// absent decodes to false (the elision case — see loginPolicy's doc
// comment), present-but-not-a-bool refuses the same way a rename does,
// present-and-bool returns as is.
func readMFABool(policy map[string]any, key string) (bool, error) {
	raw, present := policy[key]
	if !present {
		return false, nil
	}
	b, isBool := raw.(bool)
	if !isBool {
		return false, fmt.Errorf(
			"GET /management/v1/policies/login: policy.%s is %T, not a bool: %w", key, raw, ErrUnavailable)
	}
	return b, nil
}

// normalizePolicyKey collapses a JSON object key to the form
// loginPolicy's rename/re-casing check compares against: lowercased,
// underscores stripped. "forceMfa", "force_mfa", "ForceMFA", and
// "FORCE_MFA" all normalize to "forcemfa"; "forceMfaLocalOnly" normalizes
// to "forcemfalocalonly" — a DIFFERENT string, so it is never mistaken
// for a renamed forceMfa (verified live, see loginPolicy's doc comment).
// Unrelated keys ("allowUsernamePassword", "passwordCheckLifetime") do
// not collide with either.
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

// requestOptions accumulates per-request settings that do — and only do —
// apply to the outgoing *http.Request. It exists so a caller can scope a
// single call (e.g. x-zitadel-orgid for LoginPolicyForOrg) without do
// taking a header map, and without a requestOption being able to reach the
// *http.Request directly — see requestOption's doc comment for why that
// shape is rejected rather than merely discouraged. LoginPolicyForOrg is
// the first caller that needs this; it will not be the last (org-scoped
// user reads, org-scoped policy writes are named in D4 of
// docs/superpowers/specs/2026-08-20-login-policy-org-scope-design.md as
// expected future callers).
type requestOptions struct {
	orgID string
}

// requestOption mutates requestOptions before do builds the request. The
// shape is DELIBERATE: func(*requestOptions), not func(*http.Request). A
// func(*http.Request) option would let a future option set or overwrite
// ANY header on the request — including Authorization, which do already
// sets from the login client PAT a few lines above where opts are applied.
// That would make "an option quietly clobbers the auth header" a bug a
// reviewer has to keep checking for by hand; requestOptions makes it
// unrepresentable instead — do is the only code that ever turns a
// requestOptions field into a header value.
type requestOption func(*requestOptions)

// withOrgID scopes a request to a specific Zitadel org via the
// x-zitadel-orgid header (D4 of
// docs/superpowers/specs/2026-08-20-login-policy-org-scope-design.md). do
// sets the header itself from the
// accumulated orgID — see requestOption's doc comment for why the header
// name and value are do's decision, not an option's. LoginPolicyForOrg is
// the only caller today, and it refuses an empty org id itself, before
// ever constructing this option (spec D2) — withOrgID does not defend
// against an empty orgID a second time, because no path in this package
// can reach it with one.
func withOrgID(orgID string) requestOption {
	return func(o *requestOptions) { o.orgID = orgID }
}

// do issues one request against the Zitadel API and decodes a 2xx JSON
// response into out (skipped entirely when out is nil, for endpoints
// whose response body carries nothing the caller needs). notFound is the
// sentinel a 404 response maps to; it varies per call (ErrUserNotFound
// for the session endpoint, ErrAuthRequestInvalid for the auth-request
// endpoints, ErrUnavailable for the policy endpoint) because the SAME
// status code means a different failure depending on which resource was
// being addressed. opts is variadic so the existing call sites that need
// no per-request scoping are unchanged — this keeps the org-scoping diff
// about the security fix rather than about churn across every call site.
//
// The response body is parsed only far enough to pull an error id via
// readZitadelErrorID on every non-2xx path — it is never embedded raw —
// so no path in this package can accidentally surface a
// credential-adjacent detail (like failedAttempts) in a returned error
// string.
func (c *Client) do(ctx context.Context, method, path string, body, out any, notFound error, opts ...requestOption) error {
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

	var ro requestOptions
	for _, opt := range opts {
		opt(&ro)
	}
	if ro.orgID != "" {
		req.Header.Set("x-zitadel-orgid", ro.orgID)
	}

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
			// a login client PAT problem is an operational failure Helivanta
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
