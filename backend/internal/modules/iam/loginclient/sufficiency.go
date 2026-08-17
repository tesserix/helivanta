package loginclient

import (
	"context"
	"fmt"
	"log/slog"
)

// Outcome says what Helivanta may do with a session it has just established: it
// is the ONLY thing that decides whether the OIDC auth request gets
// finalized. It exists as a type rather than a bool so that a third answer
// (e.g. "prompt for a second factor here", once #41 lands) is an added
// constant a compiler forces every switch to consider, not a second bool
// someone can forget to read.
type Outcome int

const (
	// OutcomeHandoff is the zero value ON PURPOSE. Anything that
	// constructs a Result without deciding — a future code path, a
	// partially-initialised struct, a test double — lands on "do not
	// complete this login", which costs a redirect. The opposite default
	// would cost an MFA bypass, and per spec D4 that asymmetry decides
	// which value gets to be zero.
	OutcomeHandoff Outcome = iota
	// OutcomeComplete means the session satisfied everything Helivanta knows how
	// to check and the auth request was finalized; CallbackURL is set.
	OutcomeComplete
	// OutcomeFactorRequired means the session is password-only, the user
	// has enrolled TOTP and nothing ELSE Helivanta cannot collect, and
	// Helivanta should prompt for a TOTP code natively rather than hand
	// off to Zitadel's hosted UI. Factors names which factor(s) to
	// collect (today, always exactly ["totp"]). It MUST be added after
	// OutcomeComplete, never before OutcomeHandoff: OutcomeHandoff stays
	// the iota zero value on purpose (see its own doc comment), and this
	// is a third answer, not a replacement for either existing one.
	OutcomeFactorRequired
)

// String makes test failures and log lines name the outcome rather than
// print "0"/"1" — the difference between the two is the difference between
// a working login and an authentication bypass, so it must never be read
// off an integer.
func (o Outcome) String() string {
	switch o {
	case OutcomeHandoff:
		return "handoff"
	case OutcomeComplete:
		return "complete"
	case OutcomeFactorRequired:
		return "factor_required"
	default:
		return fmt.Sprintf("Outcome(%d)", int(o))
	}
}

// Result is what CompleteIfSufficient and CompleteAfterFactor answer with.
// CallbackURL is set if and only if Outcome is OutcomeComplete; on a
// handoff or a factor-required answer it is empty — on a handoff because
// there is deliberately nothing for the caller to redirect to (the caller
// must send the browser to Zitadel's own login UI to collect the factors
// Helivanta cannot), and on factor-required because the login is not
// finished yet. Factors is non-empty if and only if Outcome is
// OutcomeFactorRequired; it names which factor(s) the caller must collect
// next (today, always exactly ["totp"]).
type Result struct {
	Outcome     Outcome
	CallbackURL string
	Factors     []string
}

// CompleteIfSufficient is the ONLY way to finalize an OIDC auth request
// from outside this package: finalize itself is unexported, so a caller
// cannot complete a login without this decision running first. That is
// structural on purpose. The spike (§2 of
// docs/superpowers/spikes/2026-08-16-zitadel-login-client.md) proved that
// Zitadel, for a login client, issues an authorization code for a
// password-only session even when the org policy sets forceMfa — it
// neither refuses nor signals a missing factor. So the sufficiency
// decision is Helivanta's, and a version of it that anyone could bypass by
// calling the finalize endpoint directly would not be a control at all:
// the failure is completely silent, every user's login still appears to
// work while skipping a required factor.
//
// It fails closed. If the login policy cannot be READ, the answer is
// handoff, not complete: LoginPolicy deliberately never returns a zero
// value with a nil error (see its doc comment) precisely so an unreachable
// Zitadel cannot be mistaken here for a policy that says "MFA off". A
// handoff that was not strictly necessary costs the user one redirect; a
// completion that was not warranted is an authentication bypass, and that
// asymmetry decides the direction.
//
// KNOWN LIMITATIONS — read these before trusting the check to be more
// than it is. Both are gaps in WHICH cases are covered, not in how the
// covered cases behave, and neither is silently assumed: they are stated
// here because the alternative is a future reader taking this for a
// complete MFA gate.
//
//  1. PASSWORD-CHANGE-REQUIRED IS NOT CHECKED. Verified live 2026-08-16
//     (#854 Task 8): a user imported via
//     POST /management/v1/users/human/_import with
//     passwordChangeRequired:true — confirmed to have taken effect via
//     GET /v2/users/{id} echoing human.passwordChangeRequired:true —
//     produces a session create (POST /v2/sessions) and a finalize
//     (POST /v2/oidc/auth_requests/{id}) that are BYTE-IDENTICAL in
//     shape to a normal user's: no field on either response says the
//     password must change. Helivanta's own POST /v1/auth/login/password
//     against this user returned 200 with a valid callback_url — the
//     same as any other successful login. Zitadel does not signal this
//     case to a login client at all, so there is nothing in this
//     package's wire responses to branch on. Filed as its own issue,
//     #856, rather than fixed here: closing it needs
//     either a users/{id} read before finalize (an extra round trip on
//     every login) or Zitadel exposing the flag on the session/finalize
//     response, which is outside Helivanta's control. Documented rather than
//     silently accepted.
//
//  2. THE POLICY IS READ UNSCOPED. GET /management/v1/policies/login
//     resolves against the login client PAT's own resource owner, because
//     the request carries no x-zitadel-orgid header. Helivanta runs a single
//     org today, so the policy read and the authenticating user are
//     necessarily the same org. In a multi-org instance they would not
//     be: a user in org B would be judged by org A's policy, and org B's
//     forceMfa would never reach the branch above — failing OPEN for that
//     user. Scoping it needs the session's own org id and confirmation
//     that Zitadel honours the header on this endpoint; the spike
//     recorded neither (it captured only `factors: {user, password}` from
//     the session response, not an organizationId), so it is documented
//     rather than guessed at. Adding a second org to this instance
//     REQUIRES fixing this first.
func (c *Client) CompleteIfSufficient(ctx context.Context, authRequestID string, s Session) (Result, error) {
	policy, err := c.LoginPolicy(ctx)
	if err != nil {
		// Deliberately not returned as an error: an unreadable policy is
		// not a failed login, it is a login Helivanta is not qualified to
		// complete, and handing off lets Zitadel's own UI finish the flow.
		// But it must not be silent either — a Zitadel whose policy
		// endpoint is broken would otherwise send every user through an
		// unexplained redirect with nothing in the logs saying why. The
		// error text is safe to log: this package never puts a credential,
		// a session token or Zitadel's raw error body into one (see
		// readZitadelErrorID).
		slog.WarnContext(ctx, "login policy unreadable: handing off rather than completing the login (spec D4 fails closed)",
			"err", err)
		return Result{Outcome: OutcomeHandoff}, nil
	}
	if policy.ForceMFA {
		// The session this package can build is password-only
		// (CreatePasswordSession is its only session constructor), so
		// under forceMfa it is by construction insufficient. When Helivanta
		// learns to collect a second factor (#41), this is the branch that
		// grows a "session already carries an MFA factor" case — it must
		// not become a reason to delete the check.
		return Result{Outcome: OutcomeHandoff}, nil
	}

	// The org may not force MFA, but an individual user can still have
	// VOLUNTARILY enrolled a second factor (spike "Per-user enrolled
	// factors" §, #854 Task 8). A password-only session bypasses that
	// factor unless Helivanta checks for it here — the org policy alone is not
	// the whole story. Fails closed the same way the policy read above
	// does: an error from enrolledMethodTypes means "cannot prove this
	// session is sufficient", not "no factor found", so it hands off
	// rather than risking a bypass on an unreadable answer.
	methodTypes, err := c.enrolledMethodTypes(ctx, s.ID)
	if err != nil {
		slog.WarnContext(ctx, "enrolled-factor check unreadable: handing off rather than completing the login (fails closed, same as an unreadable policy)",
			"err", err)
		return Result{Outcome: OutcomeHandoff}, nil
	}
	// Classify what was found: totpEnrolled tracks whether the user
	// configured TOTP (the one factor VerifyTOTP, Task 2, lets Helivanta
	// collect natively); uncollectible tracks whether they ALSO configured
	// anything else. Per spec D1, a user with both TOTP and, say,
	// OTP_EMAIL enrolled must still hand off — Helivanta can only collect
	// one of the two, and completing on the strength of the one it can
	// collect would silently skip the other one the user configured.
	var totpEnrolled, uncollectible bool
	for _, methodType := range methodTypes {
		switch methodType {
		case passwordOnlyMethodType:
			// Not a second factor at all — every session already has this.
		case totpMethodType:
			totpEnrolled = true
		default:
			uncollectible = true
		}
	}
	switch {
	case uncollectible:
		return Result{Outcome: OutcomeHandoff}, nil
	case totpEnrolled:
		// Ask for the factor natively instead of handing off: this is the
		// whole point of this task. Nothing is finalized here — the login
		// completes only once CompleteAfterFactor confirms VerifyTOTP
		// actually succeeded against this session.
		return Result{Outcome: OutcomeFactorRequired, Factors: []string{"totp"}}, nil
	}

	callbackURL, err := c.finalize(ctx, authRequestID, s)
	if err != nil {
		return Result{}, fmt.Errorf("loginclient: finalize after sufficiency check: %w", err)
	}
	return Result{Outcome: OutcomeComplete, CallbackURL: callbackURL}, nil
}

// CompleteAfterFactor is the second half of the OutcomeFactorRequired
// flow: after CompleteIfSufficient asks for a TOTP code and the caller
// checks one against the session (VerifyTOTP, Task 2), this function
// decides whether that check actually succeeded and, only then, finalizes
// the login.
//
// It does NOT trust the caller's own account of what happened — a caller
// that merely believes VerifyTOTP succeeded is not proof the session
// itself carries a verified TOTP factor (a caller bug, a stale Session
// value from before a rotated token, or a race with a second request
// could all make that belief wrong). Instead it re-reads the session's
// ACTUAL verified factors (SessionFactors, Task 2) and finalizes only if
// that read reports TOTP true. This is what keeps finalize reachable
// solely through a sufficiency decision even on this second path:
// finalize is unexported specifically so nothing outside this package can
// complete a login without a check like this one running first (see
// CompleteIfSufficient's doc comment and TestFinalizeCallSiteIsUnique in
// internal/archtest), and a CompleteAfterFactor that finalized on the
// caller's say-so rather than on its own read would be exactly the kind
// of bypass that control exists to prevent.
//
// Fails closed the same way CompleteIfSufficient does: an unreadable
// SessionFactors answer means "cannot prove TOTP was verified", not "TOTP
// was not verified", so it hands off rather than risking a bypass on an
// unreadable answer.
func (c *Client) CompleteAfterFactor(ctx context.Context, authRequestID string, s Session) (Result, error) {
	factors, err := c.SessionFactors(ctx, s.ID)
	if err != nil {
		slog.WarnContext(ctx, "session factors unreadable: handing off rather than completing the login (fails closed, same as an unreadable policy)",
			"err", err)
		return Result{Outcome: OutcomeHandoff}, nil
	}
	if !factors.TOTP {
		return Result{Outcome: OutcomeHandoff}, nil
	}

	callbackURL, err := c.finalize(ctx, authRequestID, s)
	if err != nil {
		return Result{}, fmt.Errorf("loginclient: finalize after factor verification: %w", err)
	}
	return Result{Outcome: OutcomeComplete, CallbackURL: callbackURL}, nil
}
