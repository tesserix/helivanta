package loginclient

import (
	"context"
	"fmt"
	"log/slog"
)

// Outcome says what HMS may do with a session it has just established: it
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
	// OutcomeComplete means the session satisfied everything HMS knows how
	// to check and the auth request was finalized; CallbackURL is set.
	OutcomeComplete
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
	default:
		return fmt.Sprintf("Outcome(%d)", int(o))
	}
}

// Result is what CompleteIfSufficient answers with. CallbackURL is set if
// and only if Outcome is OutcomeComplete; on a handoff it is empty,
// because there is deliberately nothing for the caller to redirect to —
// the caller must send the browser to Zitadel's own login UI to collect
// the factors HMS cannot.
type Result struct {
	Outcome     Outcome
	CallbackURL string
}

// CompleteIfSufficient is the ONLY way to finalize an OIDC auth request
// from outside this package: finalize itself is unexported, so a caller
// cannot complete a login without this decision running first. That is
// structural on purpose. The spike (§2 of
// docs/superpowers/spikes/2026-08-16-zitadel-login-client.md) proved that
// Zitadel, for a login client, issues an authorization code for a
// password-only session even when the org policy sets forceMfa — it
// neither refuses nor signals a missing factor. So the sufficiency
// decision is HMS's, and a version of it that anyone could bypass by
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
//     password must change. HMS's own POST /v1/auth/login/password
//     against this user returned 200 with a valid callback_url — the
//     same as any other successful login. Zitadel does not signal this
//     case to a login client at all, so there is nothing in this
//     package's wire responses to branch on. Filed as its own issue
//     (#854 Task 8 follow-up) rather than fixed here: closing it needs
//     either a users/{id} read before finalize (an extra round trip on
//     every login) or Zitadel exposing the flag on the session/finalize
//     response, which is outside HMS's control. Documented rather than
//     silently accepted.
//
//  2. THE POLICY IS READ UNSCOPED. GET /management/v1/policies/login
//     resolves against the login client PAT's own resource owner, because
//     the request carries no x-zitadel-orgid header. HMS runs a single
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
		// not a failed login, it is a login HMS is not qualified to
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
		// under forceMfa it is by construction insufficient. When HMS
		// learns to collect a second factor (#41), this is the branch that
		// grows a "session already carries an MFA factor" case — it must
		// not become a reason to delete the check.
		return Result{Outcome: OutcomeHandoff}, nil
	}

	// The org may not force MFA, but an individual user can still have
	// VOLUNTARILY enrolled a second factor (spike "Per-user enrolled
	// factors" §, #854 Task 8). A password-only session bypasses that
	// factor unless HMS checks for it here — the org policy alone is not
	// the whole story. Fails closed the same way the policy read above
	// does: an error from HasEnrolledFactor means "cannot prove this
	// session is sufficient", not "no factor found", so it hands off
	// rather than risking a bypass on an unreadable answer.
	hasFactor, err := c.HasEnrolledFactor(ctx, s.ID)
	if err != nil {
		slog.WarnContext(ctx, "enrolled-factor check unreadable: handing off rather than completing the login (fails closed, same as an unreadable policy)",
			"err", err)
		return Result{Outcome: OutcomeHandoff}, nil
	}
	if hasFactor {
		return Result{Outcome: OutcomeHandoff}, nil
	}

	callbackURL, err := c.finalize(ctx, authRequestID, s)
	if err != nil {
		return Result{}, fmt.Errorf("loginclient: finalize after sufficiency check: %w", err)
	}
	return Result{Outcome: OutcomeComplete, CallbackURL: callbackURL}, nil
}
