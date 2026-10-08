package loginclient

import (
	"context"
	"errors"
	"fmt"
)

// Outcome says what Helivanta may do with a session it has just established: it
// is the ONLY thing that decides whether the OIDC auth request gets
// finalized. It exists as a type rather than a bool so that every answer is a
// constant a compiler forces every switch to consider, not a second bool
// someone can forget to read.
//
// There is no "hand off to Zitadel's hosted login" outcome. There used to be
// (OutcomeHandoff); it was deleted by #947 because Helivanta's users never see
// Zitadel's UI, and because the handoff URL stranded them on Zitadel's
// "You are signed in" page. See
// docs/superpowers/specs/2026-10-07-no-hosted-login-handoff-design.md.
type Outcome int

const (
	// OutcomeRefused is the zero value ON PURPOSE. Anything that constructs
	// a Result without deciding — a future code path, a partially
	// initialised struct, a test double — lands on "do not complete this
	// login". The opposite default would cost an MFA bypass, and per D4 of
	// docs/superpowers/specs/2026-08-16-hms-login-client-design.md that
	// asymmetry decides which value gets to be zero. A refusal always
	// carries a RefusalReason; a zero Result carries RefusalUnspecified,
	// which the login handlers treat as a refusal like any other.
	OutcomeRefused Outcome = iota
	// OutcomeComplete means the session satisfied everything Helivanta knows how
	// to check and the auth request was finalized; CallbackURL is set.
	OutcomeComplete
	// OutcomeFactorRequired means the session is password-only, the user
	// has enrolled TOTP and nothing ELSE Helivanta cannot collect, and
	// Helivanta should prompt for a TOTP code natively. Factors names which
	// factor(s) to collect (today, always exactly ["totp"]).
	OutcomeFactorRequired
	// OutcomeEnrollmentRequired means the session is password-only, the
	// user's org forces MFA, and the user has NOTHING enrolled — not TOTP,
	// not anything uncollectible. Helivanta should enrol a TOTP factor
	// natively and then collect its first code (#948, spec D1). Factors
	// names which factor(s) can be enrolled (today, always exactly
	// ["totp"]). It replaced RefusalMFAEnrollmentRequired; it is added
	// AFTER OutcomeFactorRequired so OutcomeRefused stays the zero value.
	OutcomeEnrollmentRequired
	// OutcomePasswordChangeRequired means every factor is proven but the
	// user's password must change before the login completes (#856): an
	// administrator set passwordChangeRequired, or the password outlived the
	// org's expiry policy. Nothing was finalized. PasswordChange says why and
	// carries the complexity policy; the caller holds the session and
	// collects a new password (ChangePassword, then
	// CompleteAfterPasswordChange).
	OutcomePasswordChangeRequired
)

// String makes test failures and log lines name the outcome rather than
// print "0"/"1" — the difference between the two is the difference between
// a working login and an authentication bypass, so it must never be read
// off an integer.
func (o Outcome) String() string {
	switch o {
	case OutcomeRefused:
		return "refused"
	case OutcomeComplete:
		return "complete"
	case OutcomeFactorRequired:
		return "factor_required"
	case OutcomeEnrollmentRequired:
		return "enrollment_required"
	case OutcomePasswordChangeRequired:
		return "password_change_required"
	default:
		return fmt.Sprintf("Outcome(%d)", int(o))
	}
}

// RefusalReason says why a session whose password Zitadel ACCEPTED still
// cannot be completed by Helivanta. It is what the login handlers log and what
// decides which Helivanta-branded message the browser is shown; it is never a
// credential failure (those are errors, ErrBadCredentials/ErrUserNotFound)
// and never "we could not tell" (that is an ErrUnavailable error, answered as
// a retryable 503 — spec D1).
type RefusalReason int

const (
	// RefusalUnspecified is the zero value: a Result nobody filled in. It is
	// still a refusal (fail closed); the handlers answer it with the most
	// conservative message and log it as a defect.
	RefusalUnspecified RefusalReason = iota
	// RefusalFactorUnsupported: the user has enrolled a method Helivanta
	// cannot collect natively (passkey, U2F, OTP email/SMS, a linked IdP).
	// Completing on the password alone would silently skip a factor the
	// user configured. Result.EnrolledMethods names what was enrolled.
	RefusalFactorUnsupported
	// RefusalFactorNotVerified: on the factor path, the session does not
	// carry the verified TOTP factor (or TOTP is no longer enrolled) even
	// though a code was accepted — the enrolment or session changed under
	// the login. Nothing to finalize; the user starts again.
	RefusalFactorNotVerified
	// RefusalPasswordChangeUnconfirmed: Zitadel accepted a new password, but
	// the re-read after it still says a change is due (#856 spec D5). Asking
	// again could loop forever, so the login is refused and logged; it should
	// never happen, because Zitadel's user read is read-your-writes.
	RefusalPasswordChangeUnconfirmed
)

// String is the stable, machine-readable name logged as refusal_reason.
func (r RefusalReason) String() string {
	switch r {
	case RefusalUnspecified:
		return "unspecified"
	case RefusalFactorUnsupported:
		return "factor_unsupported"
	case RefusalFactorNotVerified:
		return "factor_not_verified"
	case RefusalPasswordChangeUnconfirmed:
		return "password_change_unconfirmed"
	default:
		return fmt.Sprintf("RefusalReason(%d)", int(r))
	}
}

// Result is what CompleteIfSufficient, CompleteAfterFactor and
// CompleteAfterPasswordChange answer with. CallbackURL is set if and only if
// Outcome is OutcomeComplete. Factors is non-empty if and only if Outcome is
// OutcomeFactorRequired or OutcomeEnrollmentRequired. PasswordChange is set
// if and only if Outcome is OutcomePasswordChangeRequired. Reason is
// meaningful if and only if Outcome is OutcomeRefused, and EnrolledMethods is
// set for RefusalFactorUnsupported so the refusal can be logged with what the
// account actually has configured (spec D5) — Zitadel's method type names,
// never a credential.
type Result struct {
	Outcome         Outcome
	CallbackURL     string
	Factors         []string
	Reason          RefusalReason
	EnrolledMethods []string
	PasswordChange  PasswordChange
}

func refused(reason RefusalReason) Result {
	return Result{Outcome: OutcomeRefused, Reason: reason}
}

// sufficient is proof that a session was evaluated by one of this
// package's three classification paths (CompleteIfSufficient,
// CompleteAfterFactor, CompleteAfterPasswordChange) and found adequate to
// finalize.
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
// sites. The three functions that legitimately produce one are named
// above; this task's report
// (.superpowers/sdd/2026-08-17-native-mfa-auth-components/task-3-report.md,
// "Fix round 1 — Finding 3") records the compiler output proving both
// halves of this claim: the omission IS a compile error, and a
// hand-written `sufficient{}` DOES compile. That second half is closed in
// CI by archtest's TestSufficientWitnessConstructionIsPinned, which pins
// `sufficient{` construction to this file AND to exactly those three
// functions — a fourth, check-free constructor fails the build.
type sufficient struct{}

// classifyEnrolledMethods reads sessionID's subject (sessionSubject,
// client.go) and its enrolled authentication methods
// (enrolledMethodTypes, client.go), and classifies the latter into two
// independent questions both CompleteIfSufficient and CompleteAfterFactor
// need answered: totpEnrolled (did the user configure TOTP — the one
// factor VerifyTOTP, Task 2, lets Helivanta collect natively?) and
// uncollectible (did they ALSO configure anything else?). Per spec D1, a
// user with both TOTP and, say, OTP_EMAIL enrolled must still be refused —
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
// sessionSubject is the ONLY subject read any caller performs. All three
// Complete* functions call this method and hand the subject to passwordGate
// (#856), which reads that user's password state and that org's expiry
// policy; CompleteIfSufficient and CompleteAfterPasswordChange also scope
// their login policy read to its OrgID.
//
// Fails closed like every other read in this file: an error from either
// sessionSubject or enrolledMethodTypes means "cannot prove this session
// is sufficient", not "no factor found", so callers must return an
// ErrUnavailable error (never complete, never refuse as if unsupported) rather
// than risk a bypass on an unreadable answer.
func (c *Client) classifyEnrolledMethods(ctx context.Context, sessionID string) (subject sessionSubject, methodTypes []string, totpEnrolled, uncollectible bool, err error) {
	subject, err = c.sessionSubject(ctx, sessionID)
	if err != nil {
		return sessionSubject{}, nil, false, false, err
	}
	methodTypes, err = c.enrolledMethodTypes(ctx, subject.UserID)
	if err != nil {
		return sessionSubject{}, nil, false, false, err
	}
	for _, methodType := range methodTypes {
		switch methodType {
		case passwordOnlyMethodType:
			// Not a second factor at all — every session already has this.
		case idpMethodType:
			// Not a second factor either (#950, spec D1): a linked external
			// identity provider is an ALTERNATIVE way to establish the
			// first factor, and Zitadel finalizes a password session
			// without asking for it. Refusing on it does not close any
			// bypass — the policy and second-factor checks below run
			// identically — it only refuses a password Zitadel already
			// accepted, which is what locked IdP-linked accounts out of
			// production on 2026-10-08. See idpMethodType's doc comment
			// for why this is neutral while PASSKEY, also a first factor,
			// is not.
		case totpMethodType:
			totpEnrolled = true
		default:
			uncollectible = true
		}
	}
	return subject, methodTypes, totpEnrolled, uncollectible, nil
}

// CompleteIfSufficient is the ONLY way to finalize an OIDC auth request
// from outside this package: finalize itself is unexported and requires a
// `sufficient` witness only this function, CompleteAfterFactor and
// CompleteAfterPasswordChange produce, so a caller cannot complete a login
// without one of these decisions running first. That is structural on purpose. The spike (§2 of
// docs/superpowers/spikes/2026-08-16-zitadel-login-client.md) proved that
// Zitadel, for a login client, issues an authorization code for a
// password-only session even when the org policy sets forceMfa — it
// neither refuses nor signals a missing factor. So the sufficiency
// decision is Helivanta's, and a version of it that anyone could bypass by
// calling the finalize endpoint directly would not be a control at all:
// the failure is completely silent, every user's login still appears to
// work while skipping a required factor.
//
// It fails closed. If the enrolled factors or the login policy cannot be
// READ, the answer is an ErrUnavailable error (a retryable 503 to the
// browser), never complete: loginPolicy deliberately never returns a zero
// value with a nil error (see its doc comment) precisely so an unreachable
// Zitadel cannot be mistaken here for a policy that says "MFA off". A
// retry that was not strictly necessary costs the user a moment; a
// completion that was not warranted is an authentication bypass, and that
// asymmetry decides the direction. Every case Helivanta cannot complete
// even with a full answer is a reasoned refusal (Result.Reason) — never a
// redirect to Zitadel's hosted login, which #947 removed.
//
// # Enrolled-method classification runs BEFORE the policy check — #867 fix round 1, Finding 1
//
// An earlier version of this function checked policy.ForceMFA first and
// handed off immediately when it was true, before ever
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
// # A password that must change is checked last — #856
//
// Zitadel signals neither human.passwordChangeRequired nor an expired
// password to a login client anywhere on the session or finalize wire
// (verified live 2026-08-16, #854 Task 8): it would finalize such a login
// like any other. So the last thing before finalize, on every finalizing
// path, is passwordGate — after every factor is proven, never before (a
// change step offered before TOTP would let someone holding only the
// password rotate it). A due change answers OutcomePasswordChangeRequired;
// see docs/superpowers/specs/2026-10-08-password-change-required-design.md.
// This used to be KNOWN LIMITATIONS §1 here.
//
// # The policy read is now org-scoped (#913)
//
// It used to be read unscoped — against the login client PAT's own
// resource owner, regardless of which org the authenticating user
// actually belonged to, on the mistaken belief that Helivanta ran a
// single org and the two were therefore always the same. That belief
// stopped being true: the PRODUCTION instance gained a second and third
// org, verified 2026-08-19 (#913) — three orgs exist there today, only
// one of which (TESSERIX) has any Helivanta users. The unscoped read
// became a live bypass: a user in an org that forces MFA could be judged
// by a different org's policy and completed on a password-only session.
// The LOCAL DEV instance, by contrast, still holds exactly one org
// (Helivanta) as of the same date — a different instance, with org ids in
// a visibly different range from production's — which is why no local
// test can observe this condition without creating a second org itself,
// as the integration test does. The read below is now
// scoped with c.LoginPolicyForOrg(ctx, subject.OrgID), where subject came
// off the SAME GET /v2/sessions/{id} response classifyEnrolledMethods
// already read above — no additional round trip — and an absent org id
// on that response refuses the policy read (LoginPolicyForOrg's own
// empty-org guard) rather than silently falling back to an unscoped one,
// landing in the same fail-closed ErrUnavailable branch immediately below
// as every other unreadable-policy case. See design spec
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
	subject, methodTypes, totpEnrolled, uncollectible, err := c.classifyEnrolledMethods(ctx, s.ID)
	if err != nil {
		// Fails closed as a RETRYABLE error, not a refusal (#947 spec D1):
		// "we could not tell" must never tell a clinician their account is
		// unsupported, and must never complete the login either.
		return Result{}, unreadable("enrolled factors", err)
	}
	if uncollectible {
		// Whatever else CompleteIfSufficient learns, an uncollectible
		// enrolled factor is decisive on its own (spec D1) — not worth
		// spending a policy round trip on.
		return Result{Outcome: OutcomeRefused, Reason: RefusalFactorUnsupported, EnrolledMethods: methodTypes}, nil
	}
	if totpEnrolled {
		// Ask for the factor natively — the
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
		// An unreadable policy is not a failed login and not an
		// unsupported account: it is a login Helivanta is not qualified to
		// complete right now. Retryable error, fail closed (#947 spec D1;
		// login-client design spec D4). The error text is safe to log:
		// this package never puts a credential, a session token or
		// Zitadel's raw error body into one (see readZitadelErrorID).
		return Result{}, unreadable("login policy", err)
	}
	if policy.ForceMFA {
		// The session this package can build is password-only
		// (CreatePasswordSession is its only session constructor), and
		// nothing was enrolled that Helivanta could ask for natively (the
		// TOTP-only case already returned above) — nor anything it could
		// NOT (the uncollectible case returned before that). So the user
		// can be enrolled natively (#948, spec D1): the caller registers a
		// TOTP (RegisterTOTP), collects its first code, and finishes
		// through CompleteAfterFactor, which re-checks every one of the
		// conditions above before finalizing. This is NOT a completion,
		// and finalize is NOT called here.
		return Result{Outcome: OutcomeEnrollmentRequired, Factors: []string{"totp"}}, nil
	}

	// Every factor is proven; the password itself may still have to change
	// (#856). Last, so it never precedes a factor.
	change, due, err := c.passwordGate(ctx, subject)
	if err != nil {
		return Result{}, err
	}
	if due {
		return Result{Outcome: OutcomePasswordChangeRequired, PasswordChange: change}, nil
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
// the three Complete* functions in this file produce, and a
// CompleteAfterFactor that finalized on the caller's say-so, or without
// re-checking the uncollectible set, would be exactly the kind of bypass
// that control exists to prevent.
//
// Fails closed the same way CompleteIfSufficient does: an unreadable
// answer from either check means "cannot prove this session is
// sufficient", not "it is not", so it hands off rather than risking a
// bypass on an unreadable answer.
func (c *Client) CompleteAfterFactor(ctx context.Context, authRequestID string, s Session) (Result, error) {
	// The subject (user and org id) is what passwordGate below reads the
	// password state and the org's expiry policy for (#856). This function
	// reads no LOGIN policy — see its doc comment.
	subject, methodTypes, totpEnrolled, uncollectible, err := c.classifyEnrolledMethods(ctx, s.ID)
	if err != nil {
		return Result{}, unreadable("enrolled factors", err)
	}
	if uncollectible {
		// Spec D1: an uncollectible factor is decisive regardless of what
		// TOTP verification did or did not achieve — see this function's
		// doc comment, Finding 2.
		return Result{Outcome: OutcomeRefused, Reason: RefusalFactorUnsupported, EnrolledMethods: methodTypes}, nil
	}
	if !totpEnrolled {
		// The enrolment this call sees no longer matches what
		// CompleteIfSufficient saw (TOTP was removed, or this path was
		// reached without ever going through CompleteIfSufficient at
		// all). There is nothing here to natively verify against.
		return refused(RefusalFactorNotVerified), nil
	}

	factors, err := c.SessionFactors(ctx, s.ID)
	if err != nil {
		return Result{}, unreadable("session factors", err)
	}
	if !factors.TOTP {
		return refused(RefusalFactorNotVerified), nil
	}

	// The TOTP is proven; only now may a due password change be asked for
	// (#856 spec D2).
	change, due, err := c.passwordGate(ctx, subject)
	if err != nil {
		return Result{}, err
	}
	if due {
		return Result{Outcome: OutcomePasswordChangeRequired, PasswordChange: change}, nil
	}

	callbackURL, err := c.finalize(ctx, authRequestID, s, sufficient{})
	if err != nil {
		return Result{}, fmt.Errorf("loginclient: finalize after factor verification: %w", err)
	}
	return Result{Outcome: OutcomeComplete, CallbackURL: callbackURL}, nil
}

// CompleteAfterPasswordChange is the last step of the
// OutcomePasswordChangeRequired flow (#856 spec D5): after ChangePassword
// succeeded, it decides again — from scratch — whether this session may be
// finalized, and only then finalizes it.
//
// It trusts nothing the earlier decision concluded, for the same reason
// CompleteAfterFactor re-classifies (#867 fix round 1, Finding 2): time has
// passed and the enrolment, the policy or the session may have changed. So
// it re-runs every check the first decision made:
//
//  1. Enrolled methods: an uncollectible factor refuses.
//  2. The second factor: when TOTP is enrolled, the session must carry a
//     VERIFIED TOTP (SessionFactors), exactly as CompleteAfterFactor
//     requires; otherwise the user's org must not force MFA (if it does,
//     the factor it requires is unproven: RefusalFactorNotVerified).
//  3. The password gate: after a change Zitadel accepted it must say "not
//     due". If it still says due, that is RefusalPasswordChangeUnconfirmed —
//     never a second change prompt, which could loop forever.
//
// It is the third and last producer of the `sufficient` witness finalize
// requires; every check above runs before it, and every unreadable answer is
// an ErrUnavailable error (fail closed), as on the other two paths.
func (c *Client) CompleteAfterPasswordChange(ctx context.Context, authRequestID string, s Session) (Result, error) {
	subject, methodTypes, totpEnrolled, uncollectible, err := c.classifyEnrolledMethods(ctx, s.ID)
	if err != nil {
		return Result{}, unreadable("enrolled factors", err)
	}
	if uncollectible {
		return Result{Outcome: OutcomeRefused, Reason: RefusalFactorUnsupported, EnrolledMethods: methodTypes}, nil
	}
	if totpEnrolled {
		factors, err := c.SessionFactors(ctx, s.ID)
		if err != nil {
			return Result{}, unreadable("session factors", err)
		}
		if !factors.TOTP {
			return refused(RefusalFactorNotVerified), nil
		}
	} else {
		policy, err := c.LoginPolicyForOrg(ctx, subject.OrgID)
		if err != nil {
			return Result{}, unreadable("login policy", err)
		}
		if policy.ForceMFA {
			// The org requires a second factor and this session proves none:
			// nothing enrolled now, though a row at the password-change stage
			// is only written after every factor was proven — the enrolment
			// or the policy changed under the login. Enrolment (#948) is the
			// password step's business, never this one's, so this refuses.
			return refused(RefusalFactorNotVerified), nil
		}
	}

	_, due, err := c.passwordGate(ctx, subject)
	if err != nil {
		return Result{}, err
	}
	if due {
		return refused(RefusalPasswordChangeUnconfirmed), nil
	}

	callbackURL, err := c.finalize(ctx, authRequestID, s, sufficient{})
	if err != nil {
		return Result{}, fmt.Errorf("loginclient: finalize after password change: %w", err)
	}
	return Result{Outcome: OutcomeComplete, CallbackURL: callbackURL}, nil
}

// passwordGate is passwordChangeDue with its failure made the same retryable,
// never-completing error every other unreadable sufficiency read is
// (unreadable). On error, due is false and every caller returns the zero
// Result with the error — never a decision to finalize on.
func (c *Client) passwordGate(ctx context.Context, subject sessionSubject) (PasswordChange, bool, error) {
	change, due, err := c.passwordChangeDue(ctx, subject)
	if err != nil {
		return PasswordChange{}, false, unreadable("password state", err)
	}
	return change, due, nil
}

// unreadable wraps a failed sufficiency read as ErrUnavailable, so the login
// handlers answer it with the retryable 503 they already give an unreachable
// Zitadel — never a refusal, never a completion (#947 spec D1). what names the
// read for the log line; err keeps the underlying status and Zitadel error id.
func unreadable(what string, err error) error {
	if errors.Is(err, ErrUnavailable) {
		return fmt.Errorf("loginclient: %s unreadable: %w", what, err)
	}
	return fmt.Errorf("loginclient: %s unreadable: %w: %w", what, ErrUnavailable, err)
}
