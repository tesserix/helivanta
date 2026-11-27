"use client";

import { Suspense, useCallback, useEffect, useRef, useState, type Ref } from "react";
import Link from "next/link";
import { Loader2 } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { z } from "zod";
import {
  AuthCardCentered,
  AuthCredentialForm,
  AuthError,
  AuthLinkButton,
  AuthSetPasswordForm,
  Button,
  type AuthCredentialValues,
  type AuthMethodPolicy,
  type PasswordPolicy,
} from "@tesserix/web";
import {
  ApiError,
  storeExpiresAt,
  storeRenewAt,
  useApiMutation,
  useApiQuery,
} from "@helivanta/api";
import { Field, IDLE_ENDED_MARK, SIGNED_OUT_MARK, controlClass, useZodForm } from "@helivanta/ui";
import { QRCodeSVG } from "qrcode.react";

import {
  LoginBrandingProvider,
  TenantLoginLayout,
  TenantLoginHeading,
  retainLoginBrandingTenant,
} from "@/components/login-branding";
import { getUserManager } from "@/lib/oidc";
import { AccountDiscovery } from "@/components/account-discovery";
import { MfaCodeForm } from "@/components/mfa-code-form";
import { workspaceSigninScope } from "@/lib/workspace-signin";
import { RenewalFailedError, renewSession, type RenewalResult } from "@/lib/renew";
import {
  changePassword,
  checkFactor,
  checkPassword,
  checkSecurityKey,
  chooseAccount,
  enrollSecurityKey,
  verifyEnrollment,
  type AccountChoice,
  type AuthPoliciesInfo,
  type AuthRequestInfo,
  type FactorCheckResult,
  type PasswordChangeRequired,
  type PasswordChangeResult,
  type PasswordCheckResult,
  type PasswordPolicyInfo,
  type TotpEnrollment,
} from "@/lib/login-client";
import {
  assertionJSON,
  attestationJSON,
  supportsSecurityKeys,
  toCreationOptions,
  toRequestOptions,
} from "@/lib/webauthn";

// Ordinary sign-in proves mailbox control before showing separate tenant accounts.
// Scoped links keep their exact native organisation. Neither discovery nor a
// routing hint grants membership; native credentials/MFA and session exchange
// remain the enforcement boundaries. Credential labels are also the e2e contract.
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
      <LoginBrandingProvider>
        <LoginPageContent />
      </LoginBrandingProvider>
    </Suspense>
  );
}

function LoginPageContent() {
  const params = useSearchParams();
  const authRequestId = params.get("authRequest");
  if (authRequestId) {
    return <ValidatedCredentialForm authRequestId={authRequestId} />;
  }
  // "Sign in again" asks for a new sign-in, so the live session is not
  // consulted and cannot answer for it (#1820 slice 2 D2).
  if (params.get("again") === "1" && !params.has("workspace")) {
    return <RedirectLanding />;
  }
  if (!params.has("workspace") && !params.has("authRequest")) {
    return <SessionLanding />;
  }
  return <RedirectLanding autoStart={params.get("start") === "1"} />;
}

function SessionLanding() {
  const router = useRouter();
  const [state, setState] = useState<"checking" | "signIn" | "unavailable">("checking");
  const [attempt, setAttempt] = useState(0);
  const request = useRef<Promise<RenewalResult> | null>(null);
  useEffect(() => {
    let active = true;
    let ended = false;
    try {
      ended = Boolean(
        window.sessionStorage.getItem(SIGNED_OUT_MARK) ||
        window.sessionStorage.getItem(IDLE_ENDED_MARK),
      );
    } catch {
      // The server remains authoritative when browser storage is unavailable.
    }
    if (ended) {
      void Promise.resolve().then(() => {
        if (active) setState("signIn");
      });
    } else {
      request.current ??= renewSession();
      const pending = request.current;
      void pending.then(
        (result) => {
          if (request.current === pending) request.current = null;
          if (!active) return;
          storeRenewAt(result.renewAt);
          storeExpiresAt(result.expiresAt);
          router.replace("/dashboard");
        },
        (error) => {
          if (request.current === pending) request.current = null;
          if (active) setState(error instanceof RenewalFailedError ? "signIn" : "unavailable");
        },
      );
    }
    return () => {
      active = false;
    };
  }, [router, attempt]);
  if (state === "checking") return <AuthChromeLoading />;
  if (state === "signIn") return <RedirectLanding />;
  return (
    <TenantLoginLayout>
      <AuthCardCentered>
        <TenantLoginHeading />
        <p role="alert" className="text-sm text-muted-foreground">
          Your session could not be checked. Try again to continue.
        </p>
        <Button
          className="w-full"
          onClick={() => {
            request.current = null;
            setState("checking");
            setAttempt((value) => value + 1);
          }}
        >
          Retry session check
        </Button>
        <Button className="w-full" variant="ghost" onClick={() => setState("signIn")}>
          Sign in again
        </Button>
      </AuthCardCentered>
    </TenantLoginLayout>
  );
}

// The one card every pending step of sign-in shows: reading search
// parameters, checking the current session, starting the Zitadel redirect
// and validating the auth request it returns with. Same heading and line as
// the landing, and a spinner instead of step-by-step wording, so the steps
// read as one card filling in rather than three pages.
function AuthChromeLoading() {
  return (
    <TenantLoginLayout>
      <AuthCardCentered>
        <div className="space-y-2 text-center">
          <TenantLoginHeading />
          <p className="text-sm text-muted-foreground">Sign in to continue.</p>
        </div>
        <div role="status" className="flex justify-center py-2">
          <Loader2 aria-hidden className="size-5 animate-spin text-muted-foreground" />
          <span className="sr-only">Loading</span>
        </div>
      </AuthCardCentered>
    </TenantLoginLayout>
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
// The scope prefix that pins an auth request to one Zitadel organisation.
const ORGANIZATION_SCOPE_PREFIX = "urn:zitadel:iam:org:id:";

// The emailed-code account finder, kept as the fallback to the single card
// (#1751 spec D7): no remembered account, no password to hand, or recovery.
const FIND_ACCOUNTS_HREF = "/login?find=accounts";

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

  return (
    <LoginFlow
      authRequestId={authRequestId}
      policies={authRequest.data.policies}
      loginHint={authRequest.data.login_hint}
      organization={authRequest.data.scope
        .find((scope) => scope.startsWith(ORGANIZATION_SCOPE_PREFIX))
        ?.slice(ORGANIZATION_SCOPE_PREFIX.length)}
    />
  );
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
  | { kind: "accountChoice"; currentPassword: string; choices: AccountChoice[] }
  | { kind: "securityKey"; url: string }
  | { kind: "otp"; currentPassword: string }
  // #1898: a Helivanta-registered key is enrolled; `totp` when TOTP is too.
  | { kind: "keyFactor"; currentPassword: string; requestOptions: unknown; totp: boolean }
  | {
      kind: "enroll";
      currentPassword: string;
      totp: TotpEnrollment;
      securityKey?: { creationOptions: unknown };
    }
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
  loginHint,
  organization,
}: {
  authRequestId: string;
  policies: AuthPoliciesInfo;
  loginHint?: string;
  // The organisation the auth request is pinned to, if any.
  organization?: string;
}) {
  const [step, setStep] = useState<LoginStep>({ kind: "credential" });
  // Set by either step when the sign-in cannot continue: the attempt
  // expired, or (#947) Helivanta refused a correct password because it
  // cannot complete this sign-in. Both end on the SAME start-again
  // landing, carrying the API's own wording — never a navigation to
  // Zitadel's hosted login.
  const [endedMessage, setEndedMessage] = useState<string | null>(null);

  // Focus follows the step (#1854 D2): the pressed control unmounts with its
  // step, so the new step's heading takes focus. Keyed on what is shown, not
  // on a first-render flag, so Strict Mode's re-run cannot move focus off the
  // credential step's own autofocus on arrival.
  const heading = useRef<HTMLHeadingElement>(null);
  // The enrol step with a key offered opens on a choice, which has no code.
  const shown =
    endedMessage !== null
      ? "ended"
      : step.kind === "enroll" && step.securityKey
        ? "enrollChoice"
        : step.kind;
  const focusedFor = useRef(shown);
  useEffect(() => {
    if (focusedFor.current === shown) return;
    focusedFor.current = shown;
    // A code step's one job is its code: MfaCodeForm already focused the
    // first digit, which is announced with its group label, so typing can
    // start at once. Every other step, the key steps included, lands on its
    // heading.
    if (shown === "otp" || shown === "enroll") return;
    heading.current?.focus();
  }, [shown]);

  // Ending the flow also drops the password and any TOTP secret the later
  // steps were holding.
  function end(message: string) {
    setStep({ kind: "credential" });
    setEndedMessage(message);
  }

  // Every outcome of a verified password, whether it came from the
  // credential card or from picking an account, leads to the same next step.
  // `complete` is the ONLY outcome that navigates — window.location.assign
  // (not router.push), because /api/auth/callback is a full-page OIDC
  // callback. No outcome sends the browser to Zitadel's hosted login.
  function advance(result: PasswordCheckResult, currentPassword: string) {
    switch (result.outcome) {
      case "complete":
        window.location.assign(result.callbackUrl);
        break;
      case "accountChoice":
        setStep({ kind: "accountChoice", currentPassword, choices: result.choices });
        break;
      case "securityKeyRequired":
        setStep({ kind: "securityKey", url: result.url });
        break;
      case "factorRequired":
        setStep(
          result.securityKey
            ? {
                kind: "keyFactor",
                currentPassword,
                requestOptions: result.securityKey.requestOptions,
                totp: result.factors.includes("totp"),
              }
            : { kind: "otp", currentPassword },
        );
        break;
      case "enrollmentRequired":
        // Neither success nor failure, like factorRequired: the password
        // was right and the next step is to set up the authenticator the
        // org requires (#948).
        setStep({
          kind: "enroll",
          currentPassword,
          totp: result.totp,
          securityKey: result.securityKey,
        });
        break;
      case "passwordChangeRequired":
        setStep({ kind: "passwordChange", currentPassword, change: result });
        break;
      case "blocked":
        end(result.message);
        break;
    }
  }

  if (endedMessage) {
    return <RedirectLanding message={endedMessage} headingRef={heading} />;
  }

  if (step.kind === "accountChoice") {
    return (
      <AccountChoiceStep
        authRequestId={authRequestId}
        choices={step.choices}
        headingRef={heading}
        onEnded={end}
        onResult={(result) => advance(result, step.currentPassword)}
      />
    );
  }

  if (step.kind === "securityKey") {
    return (
      <TenantLoginLayout>
        <AuthCardCentered>
          <div className="space-y-2 text-center">
            <TenantLoginHeading ref={heading} />
            <p className="text-sm text-muted-foreground">
              Complete sign-in with your security key.
            </p>
          </div>
          <p className="text-sm text-muted-foreground">
            Continue securely on {new URL(step.url).hostname}, where your key is registered. You may
            be asked to enter your password again and touch your key. You will return to Helivanta
            after verification.
          </p>
          <Button className="w-full" onClick={() => window.location.assign(step.url)}>
            Continue with security key
          </Button>
          <StartAgainButton onEnded={end} />
        </AuthCardCentered>
      </TenantLoginLayout>
    );
  }

  if (step.kind === "otp" || step.kind === "enroll" || step.kind === "keyFactor") {
    const { currentPassword } = step;
    const toPasswordChange = (change: PasswordChangeRequired) =>
      setStep({ kind: "passwordChange", currentPassword, change });
    if (step.kind === "keyFactor") {
      return (
        <SecurityKeyStep
          authRequestId={authRequestId}
          requestOptions={step.requestOptions}
          headingRef={heading}
          onEnded={end}
          onPasswordChangeRequired={toPasswordChange}
          onUseAuthenticator={
            step.totp ? () => setStep({ kind: "otp", currentPassword }) : undefined
          }
        />
      );
    }
    return step.kind === "otp" ? (
      <OtpStep
        authRequestId={authRequestId}
        headingRef={heading}
        onEnded={end}
        onPasswordChangeRequired={toPasswordChange}
      />
    ) : (
      <EnrollStep
        authRequestId={authRequestId}
        totp={step.totp}
        securityKey={step.securityKey}
        headingRef={heading}
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
        headingRef={heading}
        onEnded={end}
      />
    );
  }

  return (
    <CredentialForm
      authRequestId={authRequestId}
      policies={policies}
      loginHint={loginHint}
      organization={organization}
      onResult={advance}
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

const rememberedUsernameKey = "helivanta.login.remembered-username";
const rememberedUsernameSchema = z.object({
  enabled: z.boolean(),
  username: z.string().max(320),
  organization: z.string().max(64).optional(),
});

function readRememberedUsername(): z.infer<typeof rememberedUsernameSchema> {
  try {
    const raw = window.localStorage.getItem(rememberedUsernameKey);
    const saved = raw ? rememberedUsernameSchema.safeParse(JSON.parse(raw)) : null;
    if (saved?.success) return saved.data;
  } catch {
    // Sign-in stays available when browser storage is blocked.
  }
  return { enabled: true, username: "" };
}

function saveRememberedUsername(enabled: boolean, username: string, organization?: string) {
  try {
    window.localStorage.setItem(
      rememberedUsernameKey,
      JSON.stringify({ enabled, username: enabled ? username : "", organization }),
    );
  } catch {
    // Sign-in stays available when browser storage is blocked.
  }
}

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
  loginHint,
  organization,
  onResult,
}: {
  authRequestId: string;
  policies: AuthPoliciesInfo;
  loginHint?: string;
  // The organisation the auth request names (a workspace link or a chosen
  // account), if any. Unscoped, the API resolves the account from
  // the email after the password (#1751 spec D1).
  organization?: string;
  onResult: (result: PasswordCheckResult, currentPassword: string) => void;
}) {
  // Lazy reads are hydration-safe: this form only mounts after a client-side request check.
  const scoped = organization !== undefined;
  const [remembered] = useState(readRememberedUsername);
  // Each workspace has its own login (ADR-0070): a pinned card offers only a
  // name remembered for that same workspace (#1780). A name saved on the
  // unscoped card, or before names carried a workspace, belongs to no known
  // workspace, so a pinned card leaves the field empty rather than guess.
  const rememberedName =
    remembered.enabled && (!scoped || remembered.organization === organization)
      ? remembered.username
      : "";
  // The email discovery just proved beats a name remembered from an earlier visit.
  // Bounded like the remembered name: the hint is whatever the authorize URL carried.
  const [values, setValues] = useState<AuthCredentialValues>(() => ({
    loginName: loginHint?.slice(0, 320) || rememberedName,
    password: "",
  }));
  const [loginNameError, setLoginNameError] = useState<string | undefined>();
  const [passwordError, setPasswordError] = useState<string | undefined>();
  const [rememberUsername, setRememberUsername] = useState(remembered.enabled);
  const formRef = useRef<HTMLFormElement>(null);

  // The result carries the password that was SUBMITTED, not whatever the
  // field holds by the time the answer arrives: the later steps (#856) must
  // prove exactly the password Zitadel just accepted.
  const submit = useApiMutation<
    { result: PasswordCheckResult; password: string; loginName: string },
    AuthCredentialValues
  >(
    (submitted) =>
      checkPassword({
        authRequestId,
        loginName: submitted.loginName,
        password: submitted.password,
      }).then((result) => ({
        result,
        password: submitted.password,
        loginName: submitted.loginName,
      })),
    {
      // LoginFlow's advance decides the next step; this form only records
      // the name it was given.
      onSuccess: ({ result, password, loginName }) => {
        if (result.outcome !== "blocked")
          saveRememberedUsername(rememberUsername, loginName, organization);
        onResult(result, password);
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
    // Password managers may fill inputs without firing React change events.
    const form = formRef.current;
    const parsed = credentialsSchema.safeParse({
      loginName:
        form?.querySelector<HTMLInputElement>('input[autocomplete="username"]')?.value ??
        submitted.loginName,
      password:
        form?.querySelector<HTMLInputElement>('input[autocomplete="current-password"]')?.value ??
        submitted.password,
    });
    if (!parsed.success) {
      const fieldErrors = parsed.error.flatten().fieldErrors;
      setLoginNameError(fieldErrors.loginName?.[0]);
      setPasswordError(fieldErrors.password?.[0]);
      return;
    }
    setLoginNameError(undefined);
    setPasswordError(undefined);
    submit.mutate(parsed.data);
  }

  return (
    <TenantLoginLayout>
      <AuthCardCentered>
        <div className="space-y-2 text-center">
          <TenantLoginHeading />
          <p className="text-sm text-muted-foreground">
            {scoped ? "Enter your password to continue." : "Sign in with your email and password."}
          </p>
        </div>

        <AuthCredentialForm
          ref={formRef}
          autoComplete="on"
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
        <a
          className="mt-4 block text-sm font-medium text-primary underline"
          href={
            scoped
              ? `/recover-password?authRequest=${encodeURIComponent(authRequestId)}`
              : FIND_ACCOUNTS_HREF
          }
        >
          Forgot password?
        </a>
        {/* The whole row is the target, at least 24px (WCAG 2.5.8, #1854 D4). */}
        <label className="mt-4 flex min-h-6 cursor-pointer items-center gap-2 text-sm text-muted-foreground">
          <input
            type="checkbox"
            className="size-4 accent-primary"
            checked={rememberUsername}
            disabled={submit.isPending}
            onChange={(event) => {
              const enabled = event.target.checked;
              setRememberUsername(enabled);
              saveRememberedUsername(enabled, values.loginName, organization);
            }}
          />
          Remember my username
        </label>

        {/* A pinned card can be left (#1820 spec D3): plain /login is unscoped. */}
        <Link
          href={scoped ? "/login" : FIND_ACCOUNTS_HREF}
          className="mt-2 block text-sm text-muted-foreground underline underline-offset-4"
        >
          {scoped ? "Not your workspace? Sign in with your email" : "Find my accounts by email"}
        </Link>

        <p className="mt-6 text-center text-xs text-muted-foreground">
          Need access? Contact your organisation&apos;s administrator.
        </p>
      </AuthCardCentered>
    </TenantLoginLayout>
  );
}

// The account chooser (#1751 spec D3). Reachable only after a password matched
// two or more accounts behind one email, so it discloses names only to
// someone who just proved that password. Picking an account continues that
// account exactly as a single match would.
function AccountChoiceStep({
  authRequestId,
  choices,
  headingRef,
  onEnded,
  onResult,
}: {
  authRequestId: string;
  choices: AccountChoice[];
  headingRef: Ref<HTMLHeadingElement>;
  onEnded: (message: string) => void;
  onResult: (result: PasswordCheckResult) => void;
}) {
  const choose = useApiMutation((tenantId: string) => chooseAccount({ authRequestId, tenantId }), {
    onSuccess: onResult,
    // The error renders beside the list, like the credential card's.
    suppressErrorToast: true,
  });
  return (
    <TenantLoginLayout>
      <AuthCardCentered>
        <div className="space-y-2 text-center">
          <TenantLoginHeading ref={headingRef} />
          <p className="text-sm text-muted-foreground">Choose the account to sign in to.</p>
        </div>
        <ul className="space-y-2" aria-label="Your accounts">
          {choices.map((choice) => (
            <li key={choice.tenant_id}>
              <Button
                variant="outline"
                className="h-auto w-full flex-col items-start py-3 text-left"
                disabled={choose.isPending}
                onClick={() => choose.mutate(choice.tenant_id)}
              >
                <span className="font-medium">{choice.name}</span>
                <span className="text-xs text-muted-foreground">{choice.account}</span>
              </Button>
            </li>
          ))}
        </ul>
        {choose.error ? (
          <p role="alert" className="text-sm text-destructive">
            {choose.error instanceof ApiError
              ? choose.error.message
              : "Sign-in failed. Please try again."}
          </p>
        ) : null}
        <StartAgainButton onEnded={onEnded} disabled={choose.isPending} />
      </AuthCardCentered>
    </TenantLoginLayout>
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
  headingRef,
  onEnded,
  onPasswordChangeRequired,
}: {
  authRequestId: string;
  headingRef: Ref<HTMLHeadingElement>;
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
      // MfaCodeForm's own `error` prop is this mutation's dedicated error
      // surface.
      suppressErrorToast: true,
    },
  );

  return (
    <TenantLoginLayout>
      <AuthCardCentered>
        <div className="space-y-2 text-center">
          <TenantLoginHeading ref={headingRef} />
          <p className="text-sm text-muted-foreground">
            Enter the 6-digit code from your authenticator app.
          </p>
        </div>

        <MfaCodeForm
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
        <AuthenticatorHelp />
        <StartAgainButton onEnded={onEnded} disabled={submit.isPending} />
      </AuthCardCentered>
    </TenantLoginLayout>
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
//
// When the API also offers a security key (#1898), the step opens on a choice
// between the two. The authenticator app leads to the TOTP view below; the key
// is registered and then confirmed in place (SecurityKeyChoice).
function EnrollStep({
  authRequestId,
  totp,
  securityKey,
  headingRef,
  onEnded,
  onPasswordChangeRequired,
}: {
  authRequestId: string;
  totp: TotpEnrollment;
  securityKey?: { creationOptions: unknown };
  headingRef: Ref<HTMLHeadingElement>;
  onEnded: (message: string) => void;
  onPasswordChangeRequired: (change: PasswordChangeRequired) => void;
}) {
  const [method, setMethod] = useState<"choice" | "totp">(securityKey ? "choice" : "totp");
  if (securityKey && method === "choice") {
    return (
      <SecurityKeyChoice
        authRequestId={authRequestId}
        creationOptions={securityKey.creationOptions}
        headingRef={headingRef}
        onEnded={onEnded}
        onPasswordChangeRequired={onPasswordChangeRequired}
        onUseAuthenticator={() => setMethod("totp")}
      />
    );
  }
  return (
    <TotpEnrollStep
      authRequestId={authRequestId}
      totp={totp}
      headingRef={headingRef}
      onEnded={onEnded}
      onPasswordChangeRequired={onPasswordChangeRequired}
    />
  );
}

function TotpEnrollStep({
  authRequestId,
  totp,
  headingRef,
  onEnded,
  onPasswordChangeRequired,
}: {
  authRequestId: string;
  totp: TotpEnrollment;
  headingRef: Ref<HTMLHeadingElement>;
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
    <TenantLoginLayout>
      <AuthCardCentered>
        <div className="space-y-2 text-center">
          <TenantLoginHeading />
          <h2 ref={headingRef} tabIndex={-1} className="text-lg font-semibold tracking-tight">
            Set up two-step verification
          </h2>
          <p className="text-sm text-muted-foreground">
            Scan with your authenticator app, then enter the code.
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
          <details className="text-center text-xs text-muted-foreground">
            <summary className="cursor-pointer">Can&apos;t scan?</summary>
            <p className="mt-2">Enter this key in your authenticator app:</p>
            <code
              className="mt-1 inline-block select-all rounded-md border px-2 py-1 font-mono text-sm tracking-wider text-foreground"
              aria-label="Authenticator setup key"
            >
              {totp.secret}
            </code>
          </details>
        </div>

        <MfaCodeForm
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
        <AuthenticatorHelp />
        <StartAgainButton onEnded={onEnded} disabled={submit.isPending} />
      </AuthCardCentered>
    </TenantLoginLayout>
  );
}

// #1898 copy, approved by the product owner on 2026-11-27 (enrolment).
const KEY_PROMPT = "Insert or touch your security key.";
const KEY_NOT_REGISTERED =
  "The security key wasn't registered. Try again, or use the authenticator app.";
const KEY_UNSUPPORTED = "This browser can't use security keys. Use the authenticator app instead.";
// Sign-in copy (spec D2); not yet product-owner approved, which slice 2 needs.
const KEY_UNSUPPORTED_SIGN_IN =
  "This browser can't use security keys. Use another browser, or your authenticator app if you have one.";
const KEY_NOT_ACCEPTED = "The security key wasn't accepted. Try again.";

// One press of the key: the API's answer to its assertion, or the browser
// refusing the ceremony (cancelled, timed out, no matching key).
type KeyAttempt = FactorCheckResult | { outcome: "keyFailed" };

// assertKey asks the browser for an assertion over the API's request options
// and posts it to /factor. A refused ceremony posts nothing: there is no
// assertion to send, and the challenge stays valid for a retry. Options that
// are not the server's shape throw (fail closed), outside the try, so they
// are never mistaken for a cancelled key.
async function assertKey(authRequestId: string, requestOptions: unknown): Promise<KeyAttempt> {
  const options = toRequestOptions(requestOptions);
  let assertion: Credential | null;
  try {
    assertion = await navigator.credentials.get(options);
  } catch {
    return { outcome: "keyFailed" };
  }
  if (!assertion) return { outcome: "keyFailed" };
  return checkSecurityKey({
    authRequestId,
    assertion: assertionJSON(assertion as PublicKeyCredential),
  });
}

// The factor check's outcomes, handled exactly as OtpStep handles a code's.
function finishFactor(
  result: FactorCheckResult,
  on: {
    ended: (message: string) => void;
    passwordChange: (change: PasswordChangeRequired) => void;
    refused: (message: string) => void;
  },
) {
  switch (result.outcome) {
    case "complete":
      window.location.assign(result.callbackUrl);
      break;
    case "passwordChangeRequired":
      on.passwordChange(result);
      break;
    case "blocked":
    case "expired":
      on.ended(result.message);
      break;
    case "refused":
      on.refused(result.message);
      break;
  }
}

function errorMessage(error: Error | null): string | undefined {
  if (!error) return undefined;
  return error instanceof ApiError ? error.message : "Sign-in failed. Please try again.";
}

// The key name Zitadel stores (#1898): the API trims it and allows 64
// characters, so the form refuses the same before the key is touched.
const keyNameSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, "Enter a name for the key.")
    .max(64, "Use 64 characters or fewer."),
});

// SecurityKeyChoice is the enrol step's choice (#1898 spec D3). Choosing the
// key runs navigator.credentials.create over the API's creation options,
// posts the credential to /enroll, and then confirms the new key with an
// assertion on /factor: the person touches the key twice, and the key prompt
// covers both. Once /enroll accepts, a retry repeats only the confirmation:
// the key is registered, and registering it again would be refused.
function SecurityKeyChoice({
  authRequestId,
  creationOptions,
  headingRef,
  onEnded,
  onPasswordChangeRequired,
  onUseAuthenticator,
}: {
  authRequestId: string;
  creationOptions: unknown;
  headingRef: Ref<HTMLHeadingElement>;
  onEnded: (message: string) => void;
  onPasswordChangeRequired: (change: PasswordChangeRequired) => void;
  onUseAuthenticator: () => void;
}) {
  const supported = supportsSecurityKeys();
  const form = useZodForm(keyNameSchema, { name: "Security key" });
  const [failure, setFailure] = useState<string | undefined>();
  const [requestOptions, setRequestOptions] = useState<unknown>();

  const register = useApiMutation<KeyAttempt, string>(
    async (name) => {
      let options = requestOptions;
      if (options === undefined) {
        const creation = toCreationOptions(creationOptions);
        let credential: Credential | null;
        try {
          credential = await navigator.credentials.create(creation);
        } catch {
          return { outcome: "keyFailed" };
        }
        if (!credential) return { outcome: "keyFailed" };
        const enrolled = await enrollSecurityKey({
          authRequestId,
          credential: attestationJSON(credential as PublicKeyCredential),
          name,
        });
        if (enrolled.outcome !== "challenged") return enrolled;
        options = enrolled.requestOptions;
        setRequestOptions(options);
      }
      return assertKey(authRequestId, options);
    },
    {
      onSuccess: (result) =>
        result.outcome === "keyFailed"
          ? setFailure(KEY_NOT_REGISTERED)
          : finishFactor(result, {
              ended: onEnded,
              passwordChange: onPasswordChangeRequired,
              refused: setFailure,
            }),
      suppressErrorToast: true,
    },
  );
  const busy = register.isPending;
  const nameError = form.formState.errors.name?.message;

  return (
    <TenantLoginLayout>
      <AuthCardCentered>
        <div className="space-y-2 text-center">
          <TenantLoginHeading />
          <h2 ref={headingRef} tabIndex={-1} className="text-lg font-semibold tracking-tight">
            Set up two-step verification
          </h2>
        </div>
        <Button variant="outline" className="w-full" disabled={busy} onClick={onUseAuthenticator}>
          Authenticator app
        </Button>
        <form
          noValidate
          aria-label="Security key"
          className="flex flex-col gap-4"
          onSubmit={form.handleSubmit(({ name }) => {
            setFailure(undefined);
            register.mutate(name);
          })}
        >
          <fieldset disabled={!supported || busy} className="flex flex-col gap-4">
            <Field id="security-key-name" label="Key name" error={nameError}>
              <input
                id="security-key-name"
                autoComplete="off"
                className={controlClass}
                aria-invalid={!!nameError}
                {...form.register("name")}
              />
            </Field>
            <Button type="submit" className="w-full">
              Security key
            </Button>
          </fieldset>
          {busy ? (
            <p role="status" className="text-sm text-muted-foreground">
              {KEY_PROMPT}
            </p>
          ) : null}
          <AuthError message={failure ?? errorMessage(register.error)} />
        </form>
        {supported ? null : <p className="text-sm text-muted-foreground">{KEY_UNSUPPORTED}</p>}
        <StartAgainButton onEnded={onEnded} disabled={busy} />
      </AuthCardCentered>
    </TenantLoginLayout>
  );
}

// SecurityKeyStep is the factor step for a Helivanta-registered key (#1898
// spec D2): it asks the browser for the key as soon as it renders. A refused
// ceremony stays here with a retry, and the authenticator app is offered
// when TOTP is enrolled too.
function SecurityKeyStep({
  authRequestId,
  requestOptions,
  headingRef,
  onEnded,
  onPasswordChangeRequired,
  onUseAuthenticator,
}: {
  authRequestId: string;
  requestOptions: unknown;
  headingRef: Ref<HTMLHeadingElement>;
  onEnded: (message: string) => void;
  onPasswordChangeRequired: (change: PasswordChangeRequired) => void;
  onUseAuthenticator?: () => void;
}) {
  const supported = supportsSecurityKeys();
  const [failure, setFailure] = useState<string | undefined>();
  const assert = useApiMutation<KeyAttempt>(() => assertKey(authRequestId, requestOptions), {
    onSuccess: (result) =>
      result.outcome === "keyFailed"
        ? setFailure(KEY_NOT_ACCEPTED)
        : finishFactor(result, {
            ended: onEnded,
            passwordChange: onPasswordChangeRequired,
            refused: setFailure,
          }),
    suppressErrorToast: true,
  });
  // Once per step, Strict Mode's second effect run included: two concurrent
  // ceremonies would cancel each other.
  const started = useRef(false);
  useEffect(() => {
    if (!supported || started.current) return;
    started.current = true;
    assert.mutate();
  }, [supported, assert]);
  const error = failure ?? errorMessage(assert.error);

  return (
    <TenantLoginLayout>
      <AuthCardCentered>
        <div className="space-y-2 text-center">
          <TenantLoginHeading ref={headingRef} />
          <p className="text-sm text-muted-foreground">
            {supported ? KEY_PROMPT : KEY_UNSUPPORTED_SIGN_IN}
          </p>
        </div>
        <AuthError message={error} />
        {supported && error && !assert.isPending ? (
          <Button
            className="w-full"
            onClick={() => {
              setFailure(undefined);
              assert.mutate();
            }}
          >
            Try again
          </Button>
        ) : null}
        {onUseAuthenticator ? (
          <AuthLinkButton disabled={assert.isPending} onClick={onUseAuthenticator}>
            Use your authenticator app instead
          </AuthLinkButton>
        ) : null}
        <StartAgainButton onEnded={onEnded} disabled={assert.isPending} />
      </AuthCardCentered>
    </TenantLoginLayout>
  );
}

// Every step after the password has a way back (#1854 D1): the same end()
// as an attempt ending, which drops the password and any TOTP secret and
// lands on the start-again card, never on Zitadel's hosted login.
function StartAgainButton({
  onEnded,
  disabled = false,
}: {
  onEnded: (message: string) => void;
  disabled?: boolean;
}) {
  return (
    <Button
      variant="outline"
      className="w-full"
      disabled={disabled}
      onClick={() => onEnded("Start a new sign-in attempt to use another account.")}
    >
      Start again
    </Button>
  );
}

// The workforce zone holds the reset ("Reason for authenticator reset").
function AuthenticatorHelp() {
  return (
    <p className="text-sm text-muted-foreground">
      Can&apos;t use your authenticator? Ask your organisation&apos;s administrator to reset it.
    </p>
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
  headingRef,
  onEnded,
}: {
  authRequestId: string;
  currentPassword: string;
  change: PasswordChangeRequired;
  headingRef: Ref<HTMLHeadingElement>;
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
    <TenantLoginLayout>
      <AuthCardCentered>
        <div className="space-y-2 text-center">
          <TenantLoginHeading ref={headingRef} />
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
      </AuthCardCentered>
    </TenantLoginLayout>
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
// **OIDC authorization is triggered by a button, not by mounting** (#847). This
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
//
// The one exception is opt-in per link (#1431 D1): a "Sign in" link the user
// has just clicked carries `?start=1`, and asking them a second time is the
// defect. `autoStart` starts the redirect on mount, but only when no session
// has just ended (neither mark present) and storage could be read. Sign-out
// never carries `start`, so the race above cannot come back through it; any
// doubt falls back to the button.
function RedirectLanding({
  message,
  autoStart = false,
  headingRef,
}: {
  message?: string;
  autoStart?: boolean;
  // Set only by LoginFlow, which focuses it when an attempt ends (#1854 D2).
  headingRef?: Ref<HTMLHeadingElement>;
}) {
  const params = useSearchParams();
  const workspace = params.get("workspace") ?? "";
  // The emailed-code finder, asked for explicitly (FIND_ACCOUNTS_HREF).
  const findAccounts = !workspace && params.get("find") === "accounts";
  // A workspace link starts only when it says so (?start=1, #1431). With no
  // workspace, sign-in starts on its own (#1751 spec D1): prompt=login means
  // nothing is granted until a password is typed. The effect below still
  // shows the button after a sign-out or idle end, #847's race.
  const shouldStart = workspace ? autoStart : !findAccounts && !message;
  const [error, setError] = useState<string | null>(null);
  const [starting, setStarting] = useState(shouldStart);
  // React's development double-mount must not start two redirects.
  const autoStarted = useRef(false);
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

  const signIn = useCallback(async () => {
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
      // Only a workspace link pins the request. Anything else is unscoped and
      // the API finds the account from the email after the password (#1751
      // spec D1): a workspace remembered by this browser sent the next person
      // to the previous person's workspace (#1820 spec D1).
      const tenant = workspace.trim();
      await getUserManager().signinRedirect({
        prompt: "login",
        ...(tenant
          ? {
              scope: workspaceSigninScope(tenant),
              state: { purpose: "workspace-login", tenantId: tenant },
            }
          : { state: { purpose: "email-login" } }),
      });
    } catch {
      setStarting(false);
      // signinRedirect only throws library or network errors, never our wording.
      setError("Check your connection and try again.");
    }
  }, [workspace]);

  useEffect(() => {
    // An explicit message (the expired-request state) always wins — see
    // this function's doc comment on why the states must never be mixed.
    if (message) return;
    // Every branch below that finds a just-ended session, or cannot read
    // storage, shows the button instead — and for good: the marks are
    // consumed here, so a re-run of this effect (Strict Mode, a re-render)
    // must not read their absence as "nothing ended" and start a redirect
    // over the sign-out wording (#847).
    const showLanding = () => {
      autoStarted.current = true;
      setStarting(false);
    };
    try {
      if (window.sessionStorage.getItem(IDLE_ENDED_MARK)) {
        showLanding();
        // Consuming a one-shot storage mark after hydration is the external sync effects exist for.
        // eslint-disable-next-line react-hooks/set-state-in-effect
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
        showLanding();
        setSignedOut(true);
        // Consumed: a reload, or coming back here later in the same tab,
        // is no longer "you just signed out".
        window.sessionStorage.removeItem(SIGNED_OUT_MARK);
        return;
      }
    } catch {
      // Blocked storage — fall back to the neutral wording and the button.
      showLanding();
      return;
    }
    if (!shouldStart || autoStarted.current) {
      showLanding();
      return;
    }
    autoStarted.current = true;
    // Back from the credential form returns to the landing, not to another start.
    const url = new URL(window.location.href);
    url.searchParams.delete("start");
    url.searchParams.delete("again");
    window.history.replaceState(window.history.state, "", url);
    void signIn();
  }, [message, shouldStart, signIn]);

  // While the redirect is starting, stay on the pending card rather than
  // swapping the button's wording. A failed start clears `starting` and
  // shows the landing with its error.
  if (starting && !error) return <AuthChromeLoading />;

  return (
    <TenantLoginLayout entry>
      <AuthCardCentered>
        <div className="space-y-2 text-center">
          {/* One heading for every state of this page (#1431 D3). */}
          <TenantLoginHeading ref={headingRef} />
          <p className="text-sm text-muted-foreground">
            {message ??
              (idleEnded
                ? "Your session ended after a period of inactivity."
                : signedOut
                  ? "You are signed out."
                  : "Sign in to continue.")}
          </p>
        </div>

        {!findAccounts ? (
          <>
            <Button size="lg" className="w-full" onClick={signIn} disabled={starting}>
              Sign in
            </Button>
            {workspace ? null : (
              <Link
                href={FIND_ACCOUNTS_HREF}
                className="block text-center text-sm text-muted-foreground underline underline-offset-4"
              >
                Find my accounts by email
              </Link>
            )}
          </>
        ) : (
          <AccountDiscovery
            onSelect={async (tenantId, email) => {
              retainLoginBrandingTenant(tenantId);
              await getUserManager().signinRedirect({
                prompt: "login",
                scope: workspaceSigninScope(tenantId),
                state: { purpose: "workspace-login", tenantId },
                // The credential card prefills the email discovery proved.
                ...(email ? { login_hint: email } : {}),
              });
            }}
          />
        )}

        {error ? (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        ) : null}

        <p className="text-center text-xs text-muted-foreground">
          Need access? Contact your organisation&apos;s administrator.
        </p>
      </AuthCardCentered>
    </TenantLoginLayout>
  );
}
