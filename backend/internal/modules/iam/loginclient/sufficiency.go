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

// sufficient is proof that a session was evaluated by one of this
// package's two classification paths and found adequate to finalize.
// finalize requires one as a parameter; a call that omits it does not
// compile, which closes the gap #867 fix round 1 found: nothing
// previously stopped a THIRD, future function in this package from
// calling finalize directly and skipping every check below.
//
// STATE THE LIMIT OF THIS PLAINLY rather than oversell it: `sufficient`
// is an unexported EMPTY struct, and Go permits the zero-value composite
// literal `sufficient{}` from ANY code in this package regardless of
// field visibility — there is no language feature that makes a struct
// literal only constructible from specific functions within the SAME
// package. A determined (or careless-in-a-specific-way) contributor could
// still write `c.finalize(ctx, id, s, sufficient{})` next to a brand-new
// check-free function, and it would compile. What this control actually
// buys is narrower and still real: a function that calls finalize with
// NO third argument at all — the far more likely shape of "someone added
// a caller and forgot the check entirely" — is now a compile error
// instead of a silent bypass a reviewer has to catch by reading call
// sites. See CompleteIfSufficient's and CompleteAfterFactor's doc
// comments for the two functions that legitimately produce one, and this
// task's report
// (.superpowers/sdd/2026-08-17-native-mfa-auth-components/task-3-report.md,
// "Fix round 1 — Finding 3") for the actual compiler output proving both
// halves of this claim: the omission IS a compile error, and a
// hand-written `sufficient{}` DOES compile. The stronger, fully
// structural fix — an archtest analogous to TestFinalizeCallSiteIsUnique
// that pins `sufficient{` construction to exactly two call sites the same
// way that test pins finalize's own POST — is proposed there rather than
// implemented in this round: it needs a change in internal/archtest,
// outside this round's scope.
type sufficient struct{}

// classifyEnrolledMethods reads sessionID's subject (sessionSubject,
// client.go) and its enrolled authentication methods
// (enrolledMethodTypes, client.go), and classifies the latter into two
// independent questions both CompleteIfSufficient and CompleteAfterFactor
// need answered: totpEnrolled (did the user configure TOTP — the one
// factor VerifyTOTP, Task 2, lets Helivanta collect natively?) and
// uncollectible (did they ALSO configure anything else?). Per spec D1, a
// user with both TOTP and, say, OTP_EMAIL enrolled must still hand off —
// Helivanta can only collect one of the two, and completing on the
// strength of the one it can collect would silently skip the other one
// the user configured. This is shared, rather than inlined separately in
// each caller, specifically so CompleteAfterFactor re-runs the SAME
// uncollectible check CompleteIfSufficient did (#867 fix round 1, Finding
// 2) instead of drifting from it.
//
// The returned subject is what lets CompleteIfSufficient scope its policy
// read to the authenticating user's own org (design spec D1,
// docs/superpowers/specs/2026-08-20-login-policy-org-scope-design.md)
// without a second GET /v2/sessions/{id} — this method's ONE call to
// sessionSubject is the ONLY session read either caller performs. Both
// CompleteIfSufficient and CompleteAfterFactor call this method, but only
// CompleteIfSufficient uses the subject's OrgID; CompleteAfterFactor
// discards it because it reads no policy.
//
// Fails closed like every other read in this file: an error from either
// sessionSubject or enrolledMethodTypes means "cannot prove this session
// is sufficient", not "no factor found", so callers must hand off rather
// than risk a bypass on an unreadable answer.
func (c *Client) classifyEnrolledMethods(ctx context.Context, sessionID string) (subject sessionSubject, totpEnrolled, uncollectible bool, err error) {
	subject, err = c.sessionSubject(ctx, sessionID)
	if err != nil {
		return sessionSubject{}, false, false, err
	}
	methodTypes, err := c.enrolledMethodTypes(ctx, subject.UserID)
	if err != nil {
		return sessionSubject{}, false, false, err
	}
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
	return subject, totpEnrolled, uncollectible, nil
}

// CompleteIfSufficient is the ONLY way to finalize an OIDC auth request
// from outside this package: finalize itself is unexported and requires a
// `sufficient` witness only this function and CompleteAfterFactor produce,
// so a caller cannot complete a login without one of these two decisions
// running first. That is structural on purpose. The spike (§2 of
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
// # Enrolled-method classification runs BEFORE the policy check — #867 fix round 1, Finding 1
//
// An earlier version of this function checked policy.ForceMFA first and
// returned OutcomeHandoff immediately when it was true, before ever
// looking at what the user had enrolled. That silently missed the
// headline case this whole spec (#867) exists for: an org that forces
// MFA, with a user who has enrolled TOTP and nothing else, still got
// handed off to Zitadel's hosted page instead of the native prompt — spec
// D1's first row was simply never reached. It failed closed (no bypass),
// but the feature did not fire for the case it was built for. Enrolled
// methods are now classified FIRST: TOTP-only enrollment returns
// OutcomeFactorRequired regardless of ForceMFA, because Helivanta can
// satisfy that case itself once VerifyTOTP runs; ForceMFA only continues
// to matter for the "nothing enrolled at all" case, where a password-only
// session is insufficient and there is nothing to natively prompt for.
//
// KNOWN LIMITATIONS — read this before trusting the check to be more than
// it is. This is a gap in WHICH cases are covered, not in how the covered
// case behaves, and it is not silently assumed: it is stated here because
// the alternative is a future reader taking this for a complete MFA gate.
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
// THE POLICY READ IS NOW ORG-SCOPED (#913). It used to be read unscoped —
// against the login client PAT's own resource owner, regardless of which
// org the authenticating user actually belonged to, on the mistaken
// belief that Helivanta ran a single org and the two were therefore
// always the same. That belief stopped being true (the instance gained a
// second and third org) and the unscoped read became a live bypass: a
// user in an org that forces MFA could be judged by a different org's
// policy and completed on a password-only session. The read below is now
// scoped with c.LoginPolicyForOrg(ctx, subject.OrgID), where subject came
// off the SAME GET /v2/sessions/{id} response classifyEnrolledMethods
// already read above — no additional round trip — and an absent org id
// on that response refuses the policy read (LoginPolicyForOrg's own
// empty-org guard) rather than silently falling back to an unscoped one,
// landing in the same fail-closed handoff branch immediately below as
// every other unreadable-policy case. See design spec
// docs/superpowers/specs/2026-08-20-login-policy-org-scope-design.md for
// the full history and the live-verified facts this rests on.
func (c *Client) CompleteIfSufficient(ctx context.Context, authRequestID string, s Session) (Result, error) {
	// The org may not force MFA, but an individual user can still have
	// VOLUNTARILY enrolled a second factor (spike "Per-user enrolled
	// factors" §, #854 Task 8) — and if the org DOES force MFA, this same
	// read is what tells TOTP-only enrollment apart from "nothing
	// enrolled", which decides whether there is anything to natively
	// prompt for at all. Either way this has to run before the policy
	// check, not after (see this function's "classification runs BEFORE
	// the policy check" doc section above).
	subject, totpEnrolled, uncollectible, err := c.classifyEnrolledMethods(ctx, s.ID)
	if err != nil {
		slog.WarnContext(ctx, "enrolled-factor check unreadable: handing off rather than completing the login (fails closed, same as an unreadable policy)",
			"err", err)
		return Result{Outcome: OutcomeHandoff}, nil
	}
	if uncollectible {
		// Whatever else CompleteIfSufficient learns, an uncollectible
		// enrolled factor is decisive on its own (spec D1) — not worth
		// spending a policy round trip on.
		return Result{Outcome: OutcomeHandoff}, nil
	}
	if totpEnrolled {
		// Ask for the factor natively instead of handing off — the
		// headline case this whole spec exists for, and the one Finding 1
		// found was unreachable when forceMfa was also true. Nothing is
		// finalized here — the login completes only once
		// CompleteAfterFactor confirms VerifyTOTP actually succeeded
		// against this session, independent of what ForceMFA says.
		return Result{Outcome: OutcomeFactorRequired, Factors: []string{"totp"}}, nil
	}

	// Nothing beyond a password is enrolled. Whether that is sufficient
	// now depends entirely on the AUTHENTICATING USER'S OWN ORG's policy —
	// subject.OrgID came off the same GET /v2/sessions/{id} response
	// classifyEnrolledMethods already read above, so scoping this call
	// costs no additional round trip (design spec D1,
	// docs/superpowers/specs/2026-08-20-login-policy-org-scope-design.md).
	// LoginPolicyForOrg refuses an empty subject.OrgID before issuing any
	// request (spec D2) and returns an ErrUnavailable-wrapped error, which
	// lands in the SAME fail-closed branch immediately below as every
	// other unreadable-policy case — there is deliberately no second,
	// separate empty-org check here: one control point, not two that can
	// drift apart.
	policy, err := c.LoginPolicyForOrg(ctx, subject.OrgID)
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
		// (CreatePasswordSession is its only session constructor), and
		// nothing was enrolled that Helivanta could ask for natively (the
		// TOTP-only case already returned above), so under forceMfa this
		// session is insufficient with nothing left to offer but a
		// handoff.
		return Result{Outcome: OutcomeHandoff}, nil
	}

	callbackURL, err := c.finalize(ctx, authRequestID, s, sufficient{})
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
// It re-evaluates BOTH questions CompleteIfSufficient did, not just the
// TOTP-verified one, so it can never finalize a case CompleteIfSufficient
// itself would have handed off:
//
//  1. Enrolled methods, again (#867 fix round 1, Finding 2). A session can
//     reach OutcomeFactorRequired because TOTP was the only enrolled
//     method at the time CompleteIfSufficient ran, but this call happens
//     LATER — after the user round-trips through a native TOTP prompt —
//     and this package does not control what happens to the user's
//     enrollment in between. Checking only factors.TOTP here would
//     finalize a user who has TOTP verified AND, say, OTP_EMAIL also
//     enrolled: exactly the D1 case CompleteIfSufficient's own
//     uncollectible check exists to catch. Trusting that the FIRST call
//     already ruled this out is exactly the kind of drift-between-calls
//     bug re-running the same classification closes.
//  2. Verified session factors. It does NOT trust the caller's own
//     account of what happened — a caller that merely believes VerifyTOTP
//     succeeded is not proof the session itself carries a verified TOTP
//     factor (a caller bug, a stale Session value from before a rotated
//     token, or a race with a second request could all make that belief
//     wrong). Instead it re-reads the session's ACTUAL verified factors
//     (SessionFactors, Task 2) and finalizes only if that read reports
//     TOTP true.
//
// This is what keeps finalize reachable solely through a sufficiency
// decision even on this second path: finalize requires a `sufficient`
// witness (see its doc comment and `sufficient`'s in this file) that only
// CompleteIfSufficient and CompleteAfterFactor produce, and a
// CompleteAfterFactor that finalized on the caller's say-so, or without
// re-checking the uncollectible set, would be exactly the kind of bypass
// that control exists to prevent.
//
// Fails closed the same way CompleteIfSufficient does: an unreadable
// answer from either check means "cannot prove this session is
// sufficient", not "it is not", so it hands off rather than risking a
// bypass on an unreadable answer.
func (c *Client) CompleteAfterFactor(ctx context.Context, authRequestID string, s Session) (Result, error) {
	// classifyEnrolledMethods also returns the session's subject (its org
	// id alongside the user id), which this function deliberately
	// discards: CompleteAfterFactor reads no policy — see this function's
	// doc comment — so it has no use for the org id CompleteIfSufficient
	// needs to scope LoginPolicyForOrg with (design spec D1).
	_, totpEnrolled, uncollectible, err := c.classifyEnrolledMethods(ctx, s.ID)
	if err != nil {
		slog.WarnContext(ctx, "enrolled-factor check unreadable: handing off rather than completing the login (fails closed, same as an unreadable policy)",
			"err", err)
		return Result{Outcome: OutcomeHandoff}, nil
	}
	if uncollectible {
		// Spec D1: an uncollectible factor is decisive regardless of what
		// TOTP verification did or did not achieve — see this function's
		// doc comment, Finding 2.
		return Result{Outcome: OutcomeHandoff}, nil
	}
	if !totpEnrolled {
		// The enrollment this call sees no longer matches what
		// CompleteIfSufficient saw (TOTP was removed, or this path was
		// reached without ever going through CompleteIfSufficient at
		// all). Either way there is nothing here to natively verify
		// against, so it hands off rather than guessing.
		return Result{Outcome: OutcomeHandoff}, nil
	}

	factors, err := c.SessionFactors(ctx, s.ID)
	if err != nil {
		slog.WarnContext(ctx, "session factors unreadable: handing off rather than completing the login (fails closed, same as an unreadable policy)",
			"err", err)
		return Result{Outcome: OutcomeHandoff}, nil
	}
	if !factors.TOTP {
		return Result{Outcome: OutcomeHandoff}, nil
	}

	callbackURL, err := c.finalize(ctx, authRequestID, s, sufficient{})
	if err != nil {
		return Result{}, fmt.Errorf("loginclient: finalize after factor verification: %w", err)
	}
	return Result{Outcome: OutcomeComplete, CallbackURL: callbackURL}, nil
}
