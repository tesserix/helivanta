"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { z } from "zod";
import {
  AuthCardCentered,
  AuthCardFooter,
  AuthCredentialForm,
  AuthLayoutCentered,
  AuthOtpStep,
  AuthSetPasswordForm,
  Button,
  type AuthCredentialValues,
  type AuthMethodPolicy,
  type PasswordPolicy,
} from "@tesserix/web";
import { ApiError, useApiMutation, useApiQuery } from "@helivanta/api";
import { IDLE_ENDED_MARK, SIGNED_OUT_MARK } from "@helivanta/ui";
import { QRCodeSVG } from "qrcode.react";

import { getUserManager } from "@/lib/oidc";
import {
  changePassword,
  checkFactor,
  checkPassword,
  verifyEnrollment,
  type AuthPoliciesInfo,
  type AuthRequestInfo,
  type FactorCheckResult,
  type PasswordChangeRequired,
  type PasswordChangeResult,
  type PasswordCheckResult,
  type PasswordPolicyInfo,
  type TotpEnrollment,
} from "@/lib/login-client";

// Helivanta's own sign-in page (spec D2/D6, #854 — supersedes D5a of
// 2026-08-15-zitadel-tenancy-topology-design.md). D5a said Helivanta must
// render no credential surface at all, because a compromised Helivanta
// frontend would then have a password to harvest; that spec has been
// DELIBERATELY REVERSED. Clinicians now sign in on a page with Helivanta's own
// theme, not Zitadel's stock hosted login — the Go API still holds the
// only credential-checking secret (the `IAM_LOGIN_CLIENT` PAT,
// backend/internal/modules/iam/loginui.go) and never hands it to the
// browser, so this page collects a credential but never verifies one
// itself. The trade-off D5a priced (a compromised frontend can now
// harvest a submitted password) is accepted deliberately in exchange for
// a Helivanta-branded sign-in; see the design spec for the full accounting.
//
// This page has two states, switched on whether `?authRequest=` is on
// the URL:
//
//  - ABSENT: today's landing page. Arriving at /login with no auth
//    request (a direct hit, or the post-logout redirect target) has
//    nothing yet to sign the caller into, so this renders the same
//    button it always has and starts one via signinRedirect() — see the
//    unchanged commentary below on why that is a button, not a
//    mount-time effect.
//  - PRESENT: Zitadel's own /oauth/v2/authorize redirect landed here with
//    a real auth request id. This renders the credential form and drives
//    POST /v1/auth/login/password directly against our API.
//
// The credential form's accessible names — `Email or username`,
// `Password`, and a button named `Sign in` — are a CONTRACT, not styling
// (spec D6/D7). The first was `Email` until #899; see credentialsSchema.
// e2e/tests/support/login.ts drives every one of this repo's twelve spec
// files by these exact names at the login step; renaming a label here
// fails all of them simultaneously, in a way that reads like a broken
// application rather than a renamed field. Do not "improve" the label
// text without updating that contract deliberately. #867 (spec D7) moved
// the credential fields onto `@tesserix/web`'s `AuthCredentialForm`,
// whose DEFAULT login-name label is `describeLoginName(methodPolicy)` and
// is policy-derived — CredentialForm below passes `loginNameLabel`,
// `passwordLabel`, and `submitLabel` explicitly for exactly this reason.
// The MFA step this same change adds (below) introduces NEW accessible
// names of its own; they are new, not renamed, so nothing existing
// depends on them yet, but the e2e spec added alongside this change pins
// them, so treat them as the same kind of contract from here on.
//
// MOST IMPORTANT: a password check alone is NOT sufficient authentication
// here (spec D4). Zitadel was observed, experimentally, NOT to enforce an
// org's `forceMfa` policy for a login-client session — a password-only
// session finalises with a valid authorization code even when a second
// factor is required. So Helivanta's own API decides sufficiency itself, and
// answers a submitted password with one of THREE shapes:
//
//  - `{ callback_url }` — the login is actually complete; navigate there.
//  - `{ factor_required: ["totp"] }` — (#867, spec D1/D8) the password was
//    correct and the org's policy requires a TOTP code, which Helivanta
//    collects natively. Neither success nor failure: rendering it as a
//    failed sign-in would send a clinician to reset a password that was
//    correct. See OtpStep below and login-client.ts's checkPassword doc
//    comment.
//  - a 403 `blocked` refusal (#947) — the password was correct, but
//    Helivanta cannot complete this sign-in: the org requires two-step
//    verification and none is set up, or the account uses a method
//    Helivanta does not support yet. Rendered in Helivanta's own words on
//    the start-again landing. This page NEVER navigates to Zitadel's
//    hosted login: it used to (`handoff_url`), and that stranded
//    clinicians on Zitadel's "You are signed in" page. A forced password
//    change is not among these — verified live 2026-08-16 (#854 Task 8,
//    spike §5), Zitadel signals `passwordChangeRequired` to a login client
//    nowhere in the flow, so the API COMPLETES those logins (#856).
//
// A genuinely refused credential (wrong password or unknown user) is a
// FOURTH, distinct outcome: the API answers both cases identically — same
// status, same body, same timing floor (spec D5) — specifically so a
// caller cannot learn which hospital accounts exist by trying login
// names. This page shows that ONE shared message in a single
// `role="alert"` element and never renders a per-field error for it
// (e.g. "no account with that email"), which would rebuild the same
// enumeration oracle the backend just closed. A wrong TOTP code (#867,
// spec D5/D6) answers with the IDENTICAL wording, one level deeper, for
// the same reason — see OtpStep below.
//
// A FIFTH outcome sits in front of all of the above: the auth request
// itself can be unknown, expired, or already used — reachable in normal
// use, per the design spec, by nothing more unusual than a browser left
// on this page overnight. ValidatedCredentialForm calls
// GET /v1/auth/login/request/:id on mount specifically to catch that
// BEFORE a clinician types a credential into a form that can only ever
// fail — the same expired-request error is also possible from the
// submit-time POST (checkPassword), which stays in place as the
// backstop for an auth request that goes stale in the gap between the
// mount-time check succeeding and the credential being submitted. Either
// path renders the SAME "start again" affordance: RedirectLanding's own
// Sign in button, which is a real, working control here — not just an
// error message with nowhere to go. #867 adds a SIXTH place this exact
// same situation can be discovered: a `login_attempt` row (the server-
// side record of an in-progress MFA check) can itself go stale between
// the password step and the factor step — see OtpStep's `onEnded`,
// which renders this identical landing rather than a second, different-
// looking dead end.
//
// apps/shell/app/api/auth/callback/page.tsx is unchanged: it is the
// target `callback_url` redirects to.
export default function LoginPage() {
  return (
    // Suspense boundary: useSearchParams() opts a page out of Next's
    // static shell unless something above it can suspend for the value.
    // The fallback renders the SAME auth chrome the two real states use
    // (AuthChromeLoading) rather than nothing — an earlier version of
    // this page used `fallback={null}`, which made /login's prerendered
    // HTML empty and left a blank card on screen until JS hydrated; on a
    // slow ward terminal that reads as a broken page, not a loading one.
    <Suspense fallback={<AuthChromeLoading />}>
      <LoginPageContent />
    </Suspense>
  );
}

function LoginPageContent() {
  const authRequestId = useSearchParams().get("authRequest");
  if (authRequestId) {
    return <ValidatedCredentialForm authRequestId={authRequestId} />;
  }
  return <RedirectLanding />;
}

// The shared auth chrome with nothing but a neutral "Loading…" line —
// used both as LoginPage's Suspense fallback and as
// ValidatedCredentialForm's pending state, so a caller waiting on either
// the search-param read or the auth-request GET sees the same page shell
// rather than a blank one.
function AuthChromeLoading() {
  return (
    <AuthLayoutCentered>
      <AuthCardCentered>
        <div className="space-y-2 text-center">
          <h1 className="text-2xl font-semibold tracking-tight">Helivanta</h1>
          <p className="text-sm text-muted-foreground">Loading…</p>
        </div>
      </AuthCardCentered>
    </AuthLayoutCentered>
  );
}

// Validates the auth request BEFORE rendering the credential form — see
// LoginPage's header comment on why this call exists at all
// (GET /v1/auth/login/request/:id was previously reachable and tested on
// the backend but never called by the frontend, so a stale auth request
// was only ever caught at submit time, one credential too late).
//
//  - pending → AuthChromeLoading. The read is a real network round trip
//    (unlike useSearchParams' synchronous one above), so this state is
//    reachable by a real browser, not just Next's build tooling.
//  - error → RedirectLanding, with the API's own message (e.g. "this
//    sign-in attempt has expired; start again") as the greeting and its
//    Sign in button doing double duty as "start again": clicking it
//    calls the exact same signinRedirect() a fresh visit to /login does,
//    which is genuinely what starting again means here.
//  - success → CredentialForm, unchanged.
function ValidatedCredentialForm({ authRequestId }: { authRequestId: string }) {
  const authRequest = useApiQuery<AuthRequestInfo>(
    ["auth-login-request", authRequestId],
    `/auth/login/request/${encodeURIComponent(authRequestId)}`,
  );

  if (authRequest.isPending) {
    return <AuthChromeLoading />;
  }

  if (authRequest.isError) {
    return (
      <RedirectLanding
        message={
          authRequest.error instanceof ApiError
            ? authRequest.error.message
            : "This sign-in attempt could not be verified. Start again to continue."
        }
      />
    );
  }

  return <LoginFlow authRequestId={authRequestId} policies={authRequest.data.policies} />;
}

// Maps login-client.ts's wire-shaped `AuthPoliciesInfo` (snake_case,
// unmodified from the backend — see that file's header comment on why it
// carries no translation logic of its own) onto `@tesserix/web`'s
// camelCase `AuthMethodPolicy`. The values are Helivanta's own sign-in
// capabilities, not an org's policy: no MFA requirement is mapped, because
// none is known before the user identifies (#917) — it only decides what
// this page RENDERS, never what it allows;
// loginclient.CompleteIfSufficient / CompleteAfterFactor on the backend
// remain the sole enforcers, unchanged by this page either way.
function toMethodPolicy(policies: AuthPoliciesInfo): AuthMethodPolicy {
  return {
    allowPassword: policies.allow_password,
    secondFactors: policies.second_factors as AuthMethodPolicy["secondFactors"],
    ignoreUnknownUsernames: policies.ignore_unknown_usernames,
  };
}

// "enroll" (#948) is reachable only after an `enrollmentRequired` outcome:
// the org forces MFA and the account has nothing enrolled, so the page
// renders the TOTP secret the password step minted and collects its first
// code. It is a sibling of "otp", not a sub-state of it: the two submit to
// different routes and the API refuses each other's attempt.
//
// The OTP, enrol and password-change steps carry the password typed at the
// credential step (#856): a password that must change is changed by proving
// the current one (the API's POST /v1/auth/login/password-change sends it to
// Zitadel as `currentPassword`), and the change can come AFTER the OTP or
// enrol step. It lives only here, in this component's memory, for as long as
// the flow needs it — never in storage, never in the URL — and is dropped the
// moment the flow ends. The enrol step's TOTP secret (#948, spec D5) lives
// the same way, in the step itself.
type LoginStep =
  | { kind: "credential" }
  | { kind: "otp"; currentPassword: string }
  | { kind: "enroll"; currentPassword: string; totp: TotpEnrollment }
  | { kind: "passwordChange"; currentPassword: string; change: PasswordChangeRequired };

// Owns the steps a validated auth request can be in: the credential step
// (rendered via `@tesserix/web`'s AuthCredentialForm), the OTP step
// (#867, spec D1/D8) after a `factorRequired` outcome, the enrol step
// (#948) after `enrollmentRequired`, and the password-change step (#856)
// after `passwordChangeRequired` from any of the other three.
// `endedMessage` is a further
// state this component can reach — not a step of the flow itself, but
// the SAME "start again" landing ValidatedCredentialForm's own
// mount-time check renders above: a pending `login_attempt` row (the
// server-side record of an in-progress MFA check) can go stale between
// the password step and the factor step exactly as the auth request
// itself can go stale before either step, and both must reach the
// identical RedirectLanding affordance rather than two different-looking
// dead ends for what is, from a clinician's point of view, the same
// situation: start again.
function LoginFlow({
  authRequestId,
  policies,
}: {
  authRequestId: string;
  policies: AuthPoliciesInfo;
}) {
  const [step, setStep] = useState<LoginStep>({ kind: "credential" });
  // Set by either step when the sign-in cannot continue: the attempt
  // expired, or (#947) Helivanta refused a correct password because it
  // cannot complete this sign-in. Both end on the SAME start-again
  // landing, carrying the API's own wording — never a navigation to
  // Zitadel's hosted login.
  const [endedMessage, setEndedMessage] = useState<string | null>(null);

  // Ending the flow also drops the password and any TOTP secret the later
  // steps were holding.
  function end(message: string) {
    setStep({ kind: "credential" });
    setEndedMessage(message);
  }

  if (endedMessage) {
    return <RedirectLanding message={endedMessage} />;
  }

  if (step.kind === "otp" || step.kind === "enroll") {
    const { currentPassword } = step;
    const toPasswordChange = (change: PasswordChangeRequired) =>
      setStep({ kind: "passwordChange", currentPassword, change });
    return step.kind === "otp" ? (
      <OtpStep
        authRequestId={authRequestId}
        onEnded={end}
        onPasswordChangeRequired={toPasswordChange}
      />
    ) : (
      <EnrollStep
        authRequestId={authRequestId}
        totp={step.totp}
        onEnded={end}
        onPasswordChangeRequired={toPasswordChange}
      />
    );
  }

  if (step.kind === "passwordChange") {
    return (
      <PasswordChangeStep
        authRequestId={authRequestId}
        currentPassword={step.currentPassword}
        change={step.change}
        onEnded={end}
      />
    );
  }

  return (
    <CredentialForm
      authRequestId={authRequestId}
      policies={policies}
      onFactorRequired={(currentPassword) => setStep({ kind: "otp", currentPassword })}
      onEnrollmentRequired={(currentPassword, totp) =>
        setStep({ kind: "enroll", currentPassword, totp })
      }
      onPasswordChangeRequired={(currentPassword, change) =>
        setStep({ kind: "passwordChange", currentPassword, change })
      }
      onBlocked={end}
    />
  );
}

// The login-name field carries a ZITADEL LOGIN NAME, not an email address.
// Zitadel derives login names from a user's `userName` (suffixed with an org
// domain only when the org's domain policy sets `userLoginMustBeDomain`,
// which this instance does not), so a perfectly valid account can have the
// login name `dr.patel` and never be reachable by any email at all.
//
// This previously carried `.email()`. That rejected such accounts in the
// BROWSER, before any request was made, so the operator saw "Enter a valid
// email address" from Helivanta while holding correct credentials that
// Zitadel would have accepted — a client-side rule the identity provider
// does not have (#899). `@tesserix/web`'s AuthCredentialForm agrees: its
// login-name input is `autoComplete="username"` with no `type="email"`.
//
// `.min(1)` stays and is doing real work — an empty submit must still be
// caught here rather than spending a round trip to be refused. Only the
// FORMAT assertion was wrong.
//
// Deliberately no format validation replaces it. Anything that decides
// which login names are plausible re-creates, in the client, the
// enumeration oracle the backend closes on purpose (see this file's header
// comment on the shared refusal message): a rule that rejects `dr.patel`
// but accepts `a@b.c` tells an attacker which shapes exist. Format is
// Zitadel's to judge, and its answer is deliberately unreadable to us.
const credentialsSchema = z.object({
  loginName: z.string().min(1, "Enter your email or username"),
  password: z.string().min(1, "Enter your password"),
});

// The credential step itself (spec D2/D6/D7). Renders only once
// ValidatedCredentialForm's mount-time check confirms the auth request is
// still good — see LoginPage's header comment for the full outcome
// contract (`callback_url` / `factor_required` / `blocked` / shared
// refusal / expired request) this drives against.
//
// Built on `@tesserix/web`'s AuthCredentialForm (#867, on top of #866)
// rather than hand-rolled `Field`/`Input` elements. AuthCredentialForm is
// a fully CONTROLLED component (`values` / `onValuesChange` / `onSubmit`,
// not react-hook-form's `register`/`handleSubmit`), so validation here
// runs as a plain `credentialsSchema.safeParse` on submit rather than
// through `useZodForm` — `useZodForm` is built around the uncontrolled
// register pattern this component does not use. AuthCredentialForm sets
// its own `noValidate` and renders its own `role="alert"` error surfaces
// (a per-field paragraph from its internal `AuthField`, and a form-level
// one from its internal `AuthError`) — this component supplies the text
// for those, not the markup.
function CredentialForm({
  authRequestId,
  policies,
  onFactorRequired,
  onEnrollmentRequired,
  onPasswordChangeRequired,
  onBlocked,
}: {
  authRequestId: string;
  policies: AuthPoliciesInfo;
  onFactorRequired: (currentPassword: string) => void;
  onEnrollmentRequired: (currentPassword: string, totp: TotpEnrollment) => void;
  onPasswordChangeRequired: (currentPassword: string, change: PasswordChangeRequired) => void;
  onBlocked: (message: string) => void;
}) {
  const [values, setValues] = useState<AuthCredentialValues>({ loginName: "", password: "" });
  const [loginNameError, setLoginNameError] = useState<string | undefined>();
  const [passwordError, setPasswordError] = useState<string | undefined>();

  // The result carries the password that was SUBMITTED, not whatever the
  // field holds by the time the answer arrives: the later steps (#856) must
  // prove exactly the password Zitadel just accepted.
  const submit = useApiMutation<
    { result: PasswordCheckResult; password: string },
    AuthCredentialValues
  >(
    (submitted) =>
      checkPassword({
        authRequestId,
        loginName: submitted.loginName,
        password: submitted.password,
      }).then((result) => ({ result, password: submitted.password })),
    {
      // `complete` is the ONLY outcome that navigates —
      // window.location.assign (not router.push), because /api/auth/
      // callback is a full-page OIDC callback. `factorRequired` (#867)
      // hands control to LoginFlow's OTP step; `passwordChangeRequired`
      // (#856) to its password-change step; `blocked` (#947) ends on
      // LoginFlow's start-again landing with the API's own message. No
      // outcome sends the browser to Zitadel's hosted login.
      onSuccess: ({ result, password }) => {
        switch (result.outcome) {
          case "complete":
            window.location.assign(result.callbackUrl);
            break;
          case "factorRequired":
            onFactorRequired(password);
            break;
          case "enrollmentRequired":
            // Neither success nor failure, like factorRequired: the
            // password was right and the next step is to set up the
            // authenticator the org requires (#948).
            onEnrollmentRequired(password, result.totp);
            break;
          case "passwordChangeRequired":
            onPasswordChangeRequired(password, result);
            break;
          case "blocked":
            onBlocked(result.message);
            break;
        }
      },
      // AuthCredentialForm's own `error` prop is this mutation's error
      // surface (spec D5/D6) — it sits next to the fields and is what a
      // screen reader user focused on the form expects. useApiMutation's
      // automatic toast.error(error.message) would show the EXACT SAME
      // text a second time through a second channel, which review
      // flagged as confusing rather than helpful. See
      // packages/api/src/hooks.ts's suppressErrorToast doc comment.
      suppressErrorToast: true,
    },
  );

  function handleSubmit(submitted: AuthCredentialValues) {
    const parsed = credentialsSchema.safeParse(submitted);
    if (!parsed.success) {
      const fieldErrors = parsed.error.flatten().fieldErrors;
      setLoginNameError(fieldErrors.loginName?.[0]);
      setPasswordError(fieldErrors.password?.[0]);
      return;
    }
    setLoginNameError(undefined);
    setPasswordError(undefined);
    submit.mutate(submitted);
  }

  return (
    <AuthLayoutCentered>
      <AuthCardCentered>
        <div className="space-y-2 text-center">
          <h1 className="text-2xl font-semibold tracking-tight">Helivanta</h1>
          <p className="text-sm text-muted-foreground">Sign in to continue.</p>
        </div>

        <AuthCredentialForm
          methodPolicy={toMethodPolicy(policies)}
          values={values}
          onValuesChange={setValues}
          onSubmit={handleSubmit}
          loading={submit.isPending}
          error={
            // ONE message, deliberately not attached to either field —
            // see the header comment on spec D5. submit.error is
            // whatever ApiError.message the API answered with: the
            // shared refusal text for a wrong password or unknown user
            // alike, or (for the auth-request-expired / Zitadel-
            // unavailable cases) the API's own distinct, non-enumerating
            // wording for those.
            submit.error instanceof ApiError
              ? submit.error.message
              : submit.error
                ? "Sign-in failed. Please try again."
                : undefined
          }
          loginNameError={loginNameError}
          passwordError={passwordError}
          // These three accessible names are a CONTRACT, not styling
          // (spec D6/D7) — see LoginPage's header comment. Passed
          // explicitly because AuthCredentialForm's DEFAULT login-name
          // label is `describeLoginName(methodPolicy)`, which is derived
          // from policy rather than fixed; relying on that default is the
          // concrete way this contract breaks. Do not "improve" this text
          // without updating that contract deliberately.
          //
          // Changed from "Email" in #899, together with
          // e2e/tests/support/login.ts, because the field accepts any
          // Zitadel login name and a label reading "Email" told the
          // operator the opposite — of the two ways to reconcile label
          // and behaviour, widening the label is the one that does not
          // lock out valid accounts.
          loginNameLabel="Email or username"
          passwordLabel="Password"
          submitLabel="Sign in"
        />

        <AuthCardFooter>
          <p className="text-xs text-muted-foreground">
            Your credential is checked directly by Helivanta.
          </p>
        </AuthCardFooter>
      </AuthCardCentered>
    </AuthLayoutCentered>
  );
}

// The native second-factor step (#867, spec D1/D7). Reachable only after
// CredentialForm's `factorRequired` outcome — never rendered up front —
// so the OTP prompt itself never becomes an enumeration oracle for "this
// account has TOTP" (spec D5): nothing on this page reveals it before a
// correct password.
//
// checkFactor's four outcomes (see its own doc comment in login-client.ts
// for why there are four, not the three the design spec's D8 table
// lists) map to visibly different behaviour here:
//
//  - `complete` navigates away, exactly like CredentialForm's own.
//  - `blocked` (#947) — a CORRECT code, but the user's enrolment changed
//    between the password step and this one, so Helivanta cannot complete
//    the sign-in. Ends on the start-again landing via `onEnded`, never as
//    a wrong-code message and never a navigation to Zitadel.
//  - `refused` (a wrong TOTP code, spec D5/D6) keeps THIS step visible
//    with the shared refusal wording and clears the code so the
//    clinician can retry — spec D6 allows five wrong codes before
//    exhaustion, so staying here is the normal case, not a dead end.
//  - `expired` (the `login_attempt` row missing, expired, or exhausted)
//    hands control back to `onEnded`, which renders the SAME
//    "start again" landing an expired auth request renders elsewhere on
//    this page — the clinician must re-enter their password either way,
//    and two different-looking dead ends for the same underlying
//    situation would be confusing, not helpful.
//
// Anything else `checkFactor` does not model (e.g. Zitadel unreachable)
// surfaces as a thrown `ApiError` — shown the same way, via `submit.
// error`, without a state transition, exactly like CredentialForm's own
// unmodeled-error fallback.
function OtpStep({
  authRequestId,
  onEnded,
  onPasswordChangeRequired,
}: {
  authRequestId: string;
  onEnded: (message: string) => void;
  onPasswordChangeRequired: (change: PasswordChangeRequired) => void;
}) {
  const [code, setCode] = useState("");
  // Set only on a `refused` outcome — a resolved mutation RESULT, not a
  // thrown error (see checkFactor's doc comment on why refusal is
  // modeled as data here, unlike checkPassword's). Cleared whenever the
  // clinician edits the code again, so a stale refusal message never
  // survives into a fresh attempt.
  const [refusedMessage, setRefusedMessage] = useState<string | undefined>();

  const submit = useApiMutation<FactorCheckResult, string>(
    (submittedCode) => checkFactor({ authRequestId, factor: "totp", code: submittedCode }),
    {
      onSuccess: (result) => {
        switch (result.outcome) {
          case "complete":
            window.location.assign(result.callbackUrl);
            break;
          case "passwordChangeRequired":
            // The code is proven and the password must now change (#856):
            // the API asks only now, after the factor, never before it.
            onPasswordChangeRequired(result);
            break;
          case "blocked":
            onEnded(result.message);
            break;
          case "refused":
            setRefusedMessage(result.message);
            setCode("");
            break;
          case "expired":
            onEnded(result.message);
            break;
        }
      },
      // Same reasoning as CredentialForm's suppressErrorToast above:
      // AuthOtpStep's own `error` prop is this mutation's dedicated error
      // surface.
      suppressErrorToast: true,
    },
  );

  return (
    <AuthLayoutCentered>
      <AuthCardCentered>
        <div className="space-y-2 text-center">
          <h1 className="text-2xl font-semibold tracking-tight">Helivanta</h1>
          <p className="text-sm text-muted-foreground">
            Enter the 6-digit code from your authenticator app.
          </p>
        </div>

        <AuthOtpStep
          // Review finding, fix round 1: unlike AuthCredentialForm,
          // AuthRegisterForm and AuthRecovery, AuthOtpStep does NOT set
          // `noValidate` on its own `<form>` — and its `<input>` carries
          // `pattern="\d{6}"` plus `maxLength`, both native-validation
          // attributes. Without this prop, pressing Enter on an
          // incomplete code fires the browser's native validation popup,
          // which docs/standards/frontend.md bans outright (§4/§5). This
          // IS accepted by AuthOtpStepProps (it extends
          // `FormHTMLAttributes<HTMLFormElement>` and spreads `...props`
          // onto the `<form>` — see @tesserix/web's source), so passing
          // it here is sufficient; no DOM workaround needed.
          noValidate
          value={code}
          onValueChange={(next) => {
            setCode(next);
            setRefusedMessage(undefined);
          }}
          onSubmit={(submittedCode) => submit.mutate(submittedCode)}
          loading={submit.isPending}
          error={
            refusedMessage ??
            (submit.error instanceof ApiError
              ? submit.error.message
              : submit.error
                ? "Sign-in failed. Please try again."
                : undefined)
          }
        />

        <AuthCardFooter>
          <p className="text-xs text-muted-foreground">
            Your code is checked directly by Helivanta.
          </p>
        </AuthCardFooter>
      </AuthCardCentered>
    </AuthLayoutCentered>
  );
}

// EnrollStep (#948, spec D5) is the "enroll" step: the org forces MFA and
// the account had nothing enrolled, so the password step registered a TOTP
// and handed this page its secret. The step shows the otpauth URI as a QR
// code (rendered here, in the browser — the API ships no image), the
// secret as text for manual entry, and the SAME OTP input the factor step
// uses, submitting to POST /v1/auth/login/enroll. Its outcomes are
// OtpStep's outcomes, handled identically: `complete` navigates, `refused`
// shows the API's message and clears the input, `expired` and `blocked`
// end on the start-again landing. A refresh loses the secret with the rest
// of the page state; the clinician signs in again and receives a new one
// (spec D2) — there is deliberately no way to re-fetch it.
function EnrollStep({
  authRequestId,
  totp,
  onEnded,
  onPasswordChangeRequired,
}: {
  authRequestId: string;
  totp: TotpEnrollment;
  onEnded: (message: string) => void;
  onPasswordChangeRequired: (change: PasswordChangeRequired) => void;
}) {
  const [code, setCode] = useState("");
  const [refusedMessage, setRefusedMessage] = useState<string | undefined>();

  const submit = useApiMutation<FactorCheckResult, string>(
    (submittedCode) => verifyEnrollment({ authRequestId, factor: "totp", code: submittedCode }),
    {
      onSuccess: (result) => {
        switch (result.outcome) {
          case "complete":
            window.location.assign(result.callbackUrl);
            break;
          case "passwordChangeRequired":
            // The code is proven and the password must now change (#856):
            // the API asks only now, after the factor, never before it.
            onPasswordChangeRequired(result);
            break;
          case "blocked":
            onEnded(result.message);
            break;
          case "refused":
            setRefusedMessage(result.message);
            setCode("");
            break;
          case "expired":
            onEnded(result.message);
            break;
        }
      },
      suppressErrorToast: true,
    },
  );

  return (
    <AuthLayoutCentered>
      <AuthCardCentered>
        <div className="space-y-2 text-center">
          <h1 className="text-2xl font-semibold tracking-tight">Set up two-step verification</h1>
          <p className="text-sm text-muted-foreground">
            Your organisation requires an authenticator app. Scan this code with your app, then
            enter the 6-digit code it shows.
          </p>
        </div>

        <div className="flex flex-col items-center gap-3">
          {/* role="img" + aria-label give the QR an accessible name; the
              secret below is the equivalent for anyone who cannot scan. */}
          <div
            className="rounded-md bg-background p-3"
            role="img"
            aria-label="Authenticator setup QR code"
          >
            <QRCodeSVG value={totp.uri} size={176} aria-hidden="true" />
          </div>
          <p className="text-center text-xs text-muted-foreground">
            Can&apos;t scan? Enter this key in your app:
          </p>
          <code
            className="select-all rounded-md border px-2 py-1 font-mono text-sm tracking-wider"
            aria-label="Authenticator setup key"
          >
            {totp.secret}
          </code>
        </div>

        <AuthOtpStep
          noValidate
          value={code}
          onValueChange={(next) => {
            setCode(next);
            setRefusedMessage(undefined);
          }}
          onSubmit={(submittedCode) => submit.mutate(submittedCode)}
          loading={submit.isPending}
          label="Confirmation code"
          submitLabel="Confirm and sign in"
          error={
            refusedMessage ??
            (submit.error instanceof ApiError
              ? submit.error.message
              : submit.error
                ? "Sign-in failed. Please try again."
                : undefined)
          }
        />

        <AuthCardFooter>
          <p className="text-xs text-muted-foreground">
            Your code is checked directly by Helivanta.
          </p>
        </AuthCardFooter>
      </AuthCardCentered>
    </AuthLayoutCentered>
  );
}

// toPasswordPolicy maps the API's snake_case policy onto @tesserix/web's
// PasswordPolicy, right where the component that consumes it lives (the
// same split toMethodPolicy makes above). A min_length of 0 means the API
// reported none; leaving it undefined keeps the component's own default.
function toPasswordPolicy(policy: PasswordPolicyInfo): PasswordPolicy {
  return {
    minLength: policy.min_length > 0 ? policy.min_length : undefined,
    requireUppercase: policy.requires_uppercase,
    requireLowercase: policy.requires_lowercase,
    requireNumber: policy.requires_number,
    requireSymbol: policy.requires_symbol,
  };
}

const PASSWORD_CHANGE_DESCRIPTION: Record<PasswordChangeRequired["reason"], string> = {
  required: "Your administrator requires a new password before you continue.",
  expired: "Your password has expired. Choose a new one to continue.",
};

// The password-change step (#856). Reachable only from a
// `passwordChangeRequired` outcome — after the password and, when enrolled,
// the TOTP code — so it never appears for a sign-in that has not proven every
// factor. Built from `@tesserix/web`'s AuthSetPasswordForm: it shows the
// policy as a live checklist and keeps its submit disabled until the new
// password meets it and the confirmation matches.
//
// The one rule the component cannot know is "different from the current
// password", so it is checked here before any request — the API refuses it
// too, but sending it would cost a round trip for an answer known locally.
//
// changePassword's outcomes:
//  - `complete` navigates, like every other step.
//  - `rejected` stays here with the API's message (which rule failed, or
//    that the password is unchanged); the attempt is still open.
//  - `expired` and `blocked` end on LoginFlow's start-again landing with the
//    API's message — including `password_changed_sign_in_again`, which tells
//    the user the change took effect.
// Anything else (an outage) is shown inline without a transition; the
// attempt is kept, so trying again is meaningful.
function PasswordChangeStep({
  authRequestId,
  currentPassword,
  change,
  onEnded,
}: {
  authRequestId: string;
  currentPassword: string;
  change: PasswordChangeRequired;
  onEnded: (message: string) => void;
}) {
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [inlineError, setInlineError] = useState<string | undefined>();

  const newPasswordSchema = z.string().refine((next) => next !== currentPassword, {
    message: "Choose a password different from your current one.",
  });

  const submit = useApiMutation<PasswordChangeResult, string>(
    (newPassword) => changePassword({ authRequestId, currentPassword, newPassword }),
    {
      onSuccess: (result) => {
        switch (result.outcome) {
          case "complete":
            window.location.assign(result.callbackUrl);
            break;
          case "rejected":
            setInlineError(result.message);
            break;
          case "expired":
          case "blocked":
            onEnded(result.message);
            break;
        }
      },
      // AuthSetPasswordForm's `error` prop is this mutation's error surface,
      // as on the other two steps.
      suppressErrorToast: true,
    },
  );

  function handleSubmit(newPassword: string) {
    const parsed = newPasswordSchema.safeParse(newPassword);
    if (!parsed.success) {
      setInlineError(parsed.error.issues[0]?.message);
      return;
    }
    setInlineError(undefined);
    submit.mutate(newPassword);
  }

  return (
    <AuthLayoutCentered>
      <AuthCardCentered>
        <div className="space-y-2 text-center">
          <h1 className="text-2xl font-semibold tracking-tight">Helivanta</h1>
          <p className="text-sm text-muted-foreground">
            {PASSWORD_CHANGE_DESCRIPTION[change.reason]}
          </p>
        </div>

        <AuthSetPasswordForm
          password={password}
          onPasswordChange={(next) => {
            setPassword(next);
            setInlineError(undefined);
          }}
          confirmPassword={confirmPassword}
          onConfirmPasswordChange={setConfirmPassword}
          onSubmit={handleSubmit}
          passwordPolicy={toPasswordPolicy(change.policy)}
          loading={submit.isPending}
          error={
            inlineError ??
            (submit.error instanceof ApiError
              ? submit.error.message
              : submit.error
                ? "Your password could not be changed. Please try again."
                : undefined)
          }
          title="Choose a new password"
          submitLabel="Change password"
          passwordLabel="New password"
          confirmLabel="Confirm new password"
        />

        <AuthCardFooter>
          <p className="text-xs text-muted-foreground">
            Your new password is set directly by Helivanta.
          </p>
        </AuthCardFooter>
      </AuthCardCentered>
    </AuthLayoutCentered>
  );
}

// The pre-#854 landing page — logic unchanged, now ALSO reused as the
// "start again" affordance for an expired/unknown/already-used auth
// request (see LoginPage's header comment and ValidatedCredentialForm).
// Shown whenever this route has no auth request to act on, OR the auth
// request it was given turned out not to be a good one. The common case
// for the former is arriving at /login directly, or as the post-logout
// redirect target, neither of which has an auth request to act on yet.
// Clicking the button below is what obtains one either way:
// getUserManager().signinRedirect() sends the browser to Zitadel's own
// /oauth/v2/authorize, which (per D1) redirects it right back here, this
// time WITH a fresh ?authRequest= set — landing in LoginPageContent's
// other branch, ValidatedCredentialForm.
//
// message overrides the default greeting ("Sign in to continue." / "You
// are signed out." / the idle-ended wording) with ValidatedCredentialForm's
// own wording (e.g. "this sign-in attempt has expired; start again") when
// this component is being reused as the expired-request state. When
// message is set, the SIGNED_OUT_MARK/IDLE_ENDED_MARK check below is
// skipped entirely — mixing one of those into an already-more-specific
// expired-request message would only confuse which outcome actually
// happened.
//
// This page distinguishes THREE states (#848 D6, on top of #850's
// sign-out/neither distinction): ended through inactivity, deliberately
// signed out, and neither (arrived here unauthenticated with no prior
// session event to report). They must never be collapsed into each
// other — #850 exists precisely because claiming a sign-out that did not
// happen is untrue, and telling an idle-ended session "you are signed
// out" is the same class of lie, worse on a shared terminal because it
// implies the previous clinician's session was ended deliberately.
//
// BOTH marks CAN be present in sessionStorage at once — this is not
// merely theoretical, and treating it as impossible was itself a defect
// (review finding): a SIGNED_OUT_MARK from an earlier sign-out survives
// if that sign-out's navigation to /login never completed in this tab
// (closed before the redirect landed, or the redirect failed and fell
// back — endZitadelSession's own fallback path is exactly such a case),
// and the SAME tab can then go on to idle-timeout a later session, which
// sets IDLE_ENDED_MARK without ever having cleared the stale one.
// IDLE_ENDED_MARK is checked and consumed FIRST, and — precisely because
// a stale SIGNED_OUT_MARK must not survive to lie on the NEXT render —
// this effect clears BOTH marks whenever it finds IDLE_ENDED_MARK, not
// only the one it acted on.
//
// Uses @tesserix/web's AuthLayout chrome without its credential parts —
// this is the one case on this page where that is still correct, because
// there is no auth request yet for a credential to check against.
//
// **The redirect is triggered by a button, not by mounting** (#847). This
// page used to call signinRedirect() from a useEffect on arrival, which
// caused two distinct defects:
//
//  1. Sign-out lands here (it is the registered post_logout_redirect_uri),
//     and an authorization request fired on mount raced Zitadel's own
//     session teardown. Signing in as a DIFFERENT user then failed with
//     "User not found in the system" — reproduced deterministically, and
//     hit by a human tester before that. On a shared ward terminal that is
//     the ordinary end-of-shift sequence: one clinician signs out, the
//     next cannot sign in, and the error blames their account rather than
//     the state.
//  2. Signing in became something that happened to you on page load rather
//     than an act you performed, which is the wrong default on a terminal
//     several people use.
//
// A button also gives the post-logout page somewhere to *be* — arriving
// here after signing out should look like "you are signed out", not like a
// spinner that immediately throws you at a login form.
//
// What this does NOT fix, and must not be mistaken for fixing: a clinician
// who walks away without signing out. Their Helivanta session cookie is still
// valid, so the next person never reaches this page at all. That is #848
// (idle timeout), and it is the control that actually covers the ward
// terminal.
function RedirectLanding({ message }: { message?: string }) {
  const [error, setError] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);
  // Whether the user actually signed out, as opposed to landing here
  // because middleware.ts redirects an unauthenticated request. Telling
  // someone who was never signed in that they are "signed out" is simply
  // untrue, and on a shared terminal it is worse than untrue — it implies
  // the previous person's session was ended when nothing of the sort
  // happened.
  //
  // Read in an effect, not during render: sessionStorage does not exist on
  // the server, and branching on it while rendering would mismatch
  // hydration. The first paint shows the neutral wording and settles.
  const [signedOut, setSignedOut] = useState(false);
  // Whether the session ended because the clinician was idle (#848 D6) —
  // either this browser's own onExpire timer or the server's 401
  // `session_idle` refusal, both funnelled through the same teardown in
  // hms-shell.tsx before it lands here. NOT mutually exclusive with
  // `signedOut` in sessionStorage (see this component's doc comment on
  // why both marks can coexist) — kept as its own flag, rather than
  // folded into `signedOut`, so the two messages can never be conflated,
  // and this component's effect below is what enforces which one wins.
  const [idleEnded, setIdleEnded] = useState(false);

  useEffect(() => {
    // An explicit message (the expired-request state) always wins — see
    // this function's doc comment on why the states must never be mixed.
    if (message) return;
    try {
      if (window.sessionStorage.getItem(IDLE_ENDED_MARK)) {
        setIdleEnded(true);
        // Consumed: a reload, or coming back here later in the same tab,
        // is no longer "your session just ended from inactivity".
        window.sessionStorage.removeItem(IDLE_ENDED_MARK);
        // A stale SIGNED_OUT_MARK from an earlier, uncompleted sign-out
        // (see this component's doc comment) must not survive to lie on
        // the NEXT arrival at /login in this tab, once idle-ended has
        // already won this one — cleared here, not merely left unread.
        window.sessionStorage.removeItem(SIGNED_OUT_MARK);
        return;
      }
      if (window.sessionStorage.getItem(SIGNED_OUT_MARK)) {
        setSignedOut(true);
        // Consumed: a reload, or coming back here later in the same tab,
        // is no longer "you just signed out".
        window.sessionStorage.removeItem(SIGNED_OUT_MARK);
      }
    } catch {
      // Blocked storage — fall back to the neutral wording.
    }
  }, [message]);

  async function signIn() {
    setError(null);
    setStarting(true);
    try {
      // prompt=login on this request only — never on the silent renewal in
      // lib/renew.ts, which relies on prompt=none. Applying it there would
      // make every renewal demand a password and turn a background refresh
      // into an interruption mid-consultation.
      //
      // Here it is cheap and worth it: this is an explicit, deliberate
      // sign-in, and forcing re-authentication means a live Zitadel session
      // cannot silently answer for whoever is now at the keyboard.
      await getUserManager().signinRedirect({ prompt: "login" });
    } catch (err: unknown) {
      setStarting(false);
      setError(err instanceof Error ? err.message : "Could not reach the sign-in page.");
    }
  }

  return (
    <AuthLayoutCentered>
      <AuthCardCentered>
        <div className="space-y-2 text-center">
          <h1 className="text-2xl font-semibold tracking-tight">Helivanta</h1>
          <p className="text-sm text-muted-foreground">
            {message ??
              (idleEnded
                ? "Your session ended after a period of inactivity."
                : signedOut
                  ? "You are signed out."
                  : "Sign in to continue.")}
          </p>
        </div>

        <Button className="w-full" onClick={signIn} disabled={starting}>
          {starting ? "Redirecting…" : "Sign in"}
        </Button>

        {error ? (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        ) : null}

        <AuthCardFooter>
          <p className="text-xs text-muted-foreground">
            You will be redirected to sign in securely.
          </p>
        </AuthCardFooter>
      </AuthCardCentered>
    </AuthLayoutCentered>
  );
}
