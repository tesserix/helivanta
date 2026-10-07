import { ApiError, apiFetch } from "@helivanta/api";

// The wire shape GET /v1/auth/login/request/:id answers with —
// authRequestResponse in loginui.go. Fetched directly via useApiQuery in
// apps/shell/app/login/page.tsx's ValidatedCredentialForm (no wrapper
// function here, unlike checkPassword below — a plain GET with no body
// to build and no discriminated outcome to narrow needs nothing this
// file would add over calling useApiQuery with this type directly, the
// same pattern every other read in this codebase follows, e.g.
// apps/medicore/components/visit-panel.tsx's `useApiQuery<Visit[]>(...)`).
//
// Helivanta's login form reads this on mount, before rendering the credential
// fields, so that an auth request that is already unknown, expired, or
// already used (a browser left on the login page overnight — spec D3's
// "errors and failure handling" section calls this reachable in normal
// use) is caught and shown as "start again" BEFORE a clinician types a
// credential into a form that can only ever fail. That submit-time
// handling (checkPassword below) stays in place as the backstop for an
// auth request that goes stale in the gap between this read succeeding
// and the credential being submitted.
// authPoliciesInfo is the wire shape of authRequestResponse's `policies`
// field (backend/internal/modules/iam/loginui.go's authPoliciesResponse,
// #867 spec D5/D8) — the provider-neutral subset of the org's login
// policy the form needs to decide, up front, whether to advertise an MFA
// step. Kept snake_case here, matching the wire, exactly like
// AuthRequestInfo's other fields; the mapping to @tesserix/web's
// camelCase AuthMethodPolicy happens once, in page.tsx, right where the
// component that consumes it lives — this file stays a plain read with
// no shape-translating logic, per its header comment above.
//
// requireMfaLocalOnly has no field here, deliberately: spec D5 explains
// why the backend folds it into RequireMFA rather than exposing it
// separately, and this type must not invent a slot the wire never fills.
export interface AuthPoliciesInfo {
  allow_password: boolean;
  require_mfa: boolean;
  second_factors: string[];
  ignore_unknown_usernames: boolean;
}

export interface AuthRequestInfo {
  id: string;
  client_id: string;
  redirect_uri: string;
  scope: string[];
  policies: AuthPoliciesInfo;
}

// Typed wrapper around POST /v1/auth/login/password
// (backend/internal/modules/iam/loginui.go), the credential check that
// drives Zitadel's login-client API on Helivanta's behalf (spec D2, #854). This
// is NOT the raw-fetch exception documented in
// docs/standards/frontend.md §3 (that one is exchangeIdToken in
// lib/auth-exchange.ts, which posts to POST /v1/auth/login and installs
// the session cookie): this endpoint never touches a cookie, it only
// answers with a URL to go to next, so it is an ordinary `/api/v1` call
// through `apiFetch` like any other mutation in this codebase.
//
// checkPassword deliberately returns a DISCRIMINATED union rather than an
// object with optional fields. The backend answers with exactly one of
// `callback_url` (login is complete) or (#867, spec D8) `factor_required`
// (a native second-factor step this page can collect itself), or refuses
// with one of SIGN_IN_BLOCKED_CODES (#947: the password was right but
// Helivanta cannot complete this sign-in — see the outcome comment below).
// The union makes "read the wrong field" a compile error at every call
// site instead of an `undefined` that only surfaces at runtime.
//
// `factorRequired` and `blocked` are union members rather than thrown
// errors or boolean flags: neither is a credential failure (spec D8,
// #947), and a caller that pattern-matches an exhaustive switch over this
// type cannot forget to handle them the way a caller checking `if (result.
// callback_url)` and falling through to an implicit "else, it must be a
// wrong password" could.
//
// There is no `handoff` outcome. There used to be: the API answered
// `handoff_url` and this page navigated to Zitadel's hosted login, which
// stranded clinicians on Zitadel's "You are signed in" page. #947 removed
// it — Helivanta's users never see Zitadel's UI.
export type PasswordCheckResult =
  | { outcome: "complete"; callbackUrl: string }
  | { outcome: "factorRequired"; factors: string[] }
  | SignInBlocked;

// The API's refusal codes for a sign-in whose password was ACCEPTED but
// which Helivanta cannot complete (#947 spec D2; `refusalCode*` in
// backend/internal/modules/iam/loginui.go). Answered as 403. The message
// carried alongside is the API's own wording — this file never re-hosts
// backend-owned text (see checkFactor's doc comment).
export const SIGN_IN_BLOCKED_CODES = [
  "sign_in_method_unsupported",
  "mfa_enrollment_required",
  "sign_in_incomplete",
] as const;

export type SignInBlockedReason = (typeof SIGN_IN_BLOCKED_CODES)[number];

export interface SignInBlocked {
  outcome: "blocked";
  reason: SignInBlockedReason;
  message: string;
}

// asSignInBlocked turns a thrown ApiError carrying one of the refusal codes
// above into the `blocked` outcome, and answers undefined for anything else
// so the caller rethrows it unchanged.
function asSignInBlocked(err: unknown): SignInBlocked | undefined {
  if (!(err instanceof ApiError)) return undefined;
  const reason = SIGN_IN_BLOCKED_CODES.find((code) => code === err.code);
  if (!reason) return undefined;
  return { outcome: "blocked", reason, message: err.message };
}

interface PasswordCheckParams {
  authRequestId: string;
  loginName: string;
  password: string;
}

// The raw 2xx wire shape POST /v1/auth/login/password answers with —
// `passwordSuccessResponse` or (#867) `factorRequiredResponse` in
// loginui.go, whichever the backend chose. Never both; see checkPassword's
// doc comment on why the return type does not mirror this directly.
// Reused by checkFactor below, whose success shape is the same
// `passwordSuccessResponse`.
interface PasswordCheckResponse {
  callback_url?: string;
  factor_required?: string[];
}

// checkPassword is the ONLY thing Helivanta's login form does with a
// credential: hand it to the API and act on the answer. A `blocked`
// outcome is NOT a wrong password — spec D4 is explicit that Zitadel does
// not enforce MFA for a login client, so the API decides sufficiency
// itself, and when a correct password alone is not enough and Helivanta
// cannot collect what else is required (MFA required by org policy with
// nothing enrolled, or a voluntarily enrolled factor Helivanta does not
// support yet — passkey, U2F, email/SMS code, a linked IdP) it REFUSES in
// its own words (#947). The caller renders that message with a way to
// start again; it never navigates anywhere for it. A policy the API could
// not read is NOT `blocked` — it is a retryable 503 thrown as an ApiError,
// like any other outage. A forced password change is NOT one of these
// either: verified live 2026-08-16 (#854 Task 8, spike §5), Zitadel
// signals `passwordChangeRequired` to a login client nowhere in the flow,
// so the API COMPLETES those logins instead — tracked as #856.
//
// A refused credential (wrong password or unknown user, answered
// identically per spec D5) surfaces as an `ApiError` thrown by
// `apiFetch` — its `.message` is the ONE shared refusal text
// ("email or password is incorrect"), never anything that names which
// field was wrong, so the caller can render it verbatim without
// reconstructing the enumeration oracle the backend already closed.
//
// `factorRequired` (#867, spec D1/D8) means the credential was CORRECT
// and the org's policy requires a second factor Helivanta can now collect
// natively (TOTP only, today — `factors` always answers `["totp"]`, per
// `nonNilFactors`' own doc comment on the Go side). This is the SAME
// "neither success nor failure" trap `blocked` documents above: rendering
// it as a failed sign-in would send a clinician to reset a password that
// was correct. The caller must transition to a factor-collection step
// (checkFactor below), never show an error.
//
// `factors` itself is NOT read by page.tsx today (review finding, fix
// round 1): `CredentialForm` hardcodes `factor: "totp"` when it calls
// `checkFactor`, per this task's own brief ("today it is always
// `["totp"]`, so render the single step directly and do not build a
// selector for one option"). The array survives the wire so a caller CAN
// read it once a second native factor exists — see `@tesserix/web`'s
// `AuthMfaSelector` for the component that would then consume it — but
// nothing reads it yet, and this comment must not claim otherwise.
export async function checkPassword(params: PasswordCheckParams): Promise<PasswordCheckResult> {
  let body: PasswordCheckResponse;
  try {
    body = await apiFetch<PasswordCheckResponse>("/auth/login/password", {
      method: "POST",
      body: JSON.stringify({
        auth_request_id: params.authRequestId,
        login_name: params.loginName,
        password: params.password,
      }),
    });
  } catch (err) {
    const blocked = asSignInBlocked(err);
    if (blocked) return blocked;
    throw err;
  }

  if (body.callback_url) {
    return { outcome: "complete", callbackUrl: body.callback_url };
  }
  if (body.factor_required) {
    return { outcome: "factorRequired", factors: body.factor_required };
  }
  // Neither key present is a contract violation by the API,
  // not a user error — there is no credential-shaped explanation for it,
  // so it must not be folded into the shared refusal message above.
  throw new Error("Sign-in could not be completed: the server sent an unexpected response.");
}

interface FactorCheckParams {
  authRequestId: string;
  factor: string;
  code: string;
}

// checkFactor is checkPassword's counterpart for the second step (#867,
// spec D8): POST /v1/auth/login/factor, which verifies a TOTP code
// against the Zitadel session `factorRequired` above started and either
// finalizes the login or refuses the code.
//
// Its return type discriminates FOUR outcomes rather than the three the
// design spec's D8 table lists (`callback_url` | shared refusal |
// attempt-expired), because loginui.go's Factor handler documents a
// fourth branch the table omits: `CompleteAfterFactor` re-runs the same
// uncollectible/enrolled checks `CompleteIfSufficient` did and CAN still
// refuse even after a CORRECT code — e.g. the user's enrolment changed
// between the password step and this one. That is checkPassword's own
// `blocked` outcome (#947), not a wrong code: treating it as `refused`
// would tell a clinician their correct code was rejected.
//
// `refused` and `expired` carry the API's own `message` rather than
// nothing, deliberately diverging from a bare discriminant tag: this file
// never re-hosts backend-owned wording (see `passwordFailureMessage` /
// `authRequestExpiredMessage` in loginui.go, which the caller here never
// reconstructs), the same reason `checkPassword`'s refusal is surfaced as
// an `ApiError.message` rather than a fixed string. `refused` and
// `expired` are resolved OUTCOMES here — not thrown, unlike checkPassword's
// refusal — because, unlike the credential form, the OTP step's caller
// must take a STRUCTURAL action on `expired` (return to the credential
// step; the pending login_attempt row is gone) that is different from
// `refused` (stay on the OTP step, let the clinician retry). A thrown
// `ApiError` only tells `useApiMutation` to populate `.error`; it cannot
// drive that branch without the caller re-inspecting `ApiError.code`
// itself, duplicating the classification this function already owns.
export type FactorCheckResult =
  | { outcome: "complete"; callbackUrl: string }
  | SignInBlocked
  | { outcome: "refused"; message: string }
  | { outcome: "expired"; message: string };

// A wrong TOTP code and an unknown/expired/exhausted auth_request_id
// answer with different HTTP statuses and error codes (spec D5/D6):
//   - wrong code → 401 `invalid_credentials`, `respondEqualisedFailure`
//     (the SAME literal wording a wrong password answers with — spec D5
//     deliberately reuses it rather than inventing "wrong code" text,
//     which would itself be a signal).
//   - missing/expired/exhausted attempt → 400 `auth_request_invalid`,
//     `respondAttemptExpired` (the SAME code and wording an expired auth
//     request answers with at the mount-time GET — ValidatedCredentialForm
//     above already renders this as "start again", so the OTP step routes
//     back to that exact state rather than inventing a second one).
// Anything else (e.g. `zitadel_unavailable`) is NOT one of checkFactor's
// modeled outcomes — it is rethrown, the same "not a credential-shaped
// answer" treatment checkPassword gives its own unmodeled case, so the
// caller's generic error surface handles it rather than this function
// mis-filing an outage as a wrong code.
export async function checkFactor(params: FactorCheckParams): Promise<FactorCheckResult> {
  let body: PasswordCheckResponse;
  try {
    body = await apiFetch<PasswordCheckResponse>("/auth/login/factor", {
      method: "POST",
      body: JSON.stringify({
        auth_request_id: params.authRequestId,
        factor: params.factor,
        code: params.code,
      }),
    });
  } catch (err) {
    if (err instanceof ApiError && err.code === "auth_request_invalid") {
      return { outcome: "expired", message: err.message };
    }
    if (err instanceof ApiError && err.code === "invalid_credentials") {
      return { outcome: "refused", message: err.message };
    }
    const blocked = asSignInBlocked(err);
    if (blocked) return blocked;
    throw err;
  }

  if (body.callback_url) {
    return { outcome: "complete", callbackUrl: body.callback_url };
  }
  // Same contract-violation treatment as checkPassword's fallback: not a
  // shape this endpoint's documented outcomes can produce.
  throw new Error("Sign-in could not be completed: the server sent an unexpected response.");
}
