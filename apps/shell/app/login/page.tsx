"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { z } from "zod";
import { AuthCardCentered, AuthCardFooter, AuthLayoutCentered, Button, Input } from "@tesserix/web";
import { ApiError, useApiMutation } from "@hms/api";
import { Field, SIGNED_OUT_MARK, useZodForm } from "@hms/ui";

import { getUserManager } from "@/lib/oidc";
import { checkPassword, type PasswordCheckResult } from "@/lib/login-client";

// HMS's own sign-in page (spec D2/D6, #854 — supersedes D5a of
// 2026-08-15-zitadel-tenancy-topology-design.md). D5a said HMS must
// render no credential surface at all, because a compromised HMS
// frontend would then have a password to harvest; that spec has been
// DELIBERATELY REVERSED. Clinicians now sign in on a page with HMS's own
// theme, not Zitadel's stock hosted login — the Go API still holds the
// only credential-checking secret (the `IAM_LOGIN_CLIENT` PAT,
// backend/internal/modules/iam/loginui.go) and never hands it to the
// browser, so this page collects a credential but never verifies one
// itself. The trade-off D5a priced (a compromised frontend can now
// harvest a submitted password) is accepted deliberately in exchange for
// an HMS-branded sign-in; see the design spec for the full accounting.
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
// The credential form's accessible names — `Email`, `Password`, and a
// button named `Sign in` — are a CONTRACT, not styling (spec D6).
// e2e/tests/support/login.ts drives every one of this repo's eleven spec
// files by these exact names at the login step; renaming a label here
// fails all of them simultaneously, in a way that reads like a broken
// application rather than a renamed field. Do not "improve" the label
// text without updating that contract deliberately.
//
// MOST IMPORTANT: a password check alone is NOT sufficient authentication
// here (spec D4). Zitadel was observed, experimentally, NOT to enforce an
// org's `forceMfa` policy for a login-client session — a password-only
// session finalises with a valid authorization code even when a second
// factor is required. So HMS's own API decides sufficiency itself, and
// answers a submitted password with one of two shapes:
//
//  - `{ callback_url }` — the login is actually complete; navigate there.
//  - `{ handoff_url }` — HMS cannot finish this login itself (MFA
//    required, `forceMfaLocalOnly`, a forced password change, a
//    federated hospital IdP, or a policy the API could not read and so
//    fails closed on). This is a NORMAL OUTCOME, not an error. The
//    caller's password may have been entirely correct — this page must
//    navigate to `handoff_url` exactly as it would `callback_url`, never
//    render it as a failed sign-in. Treating a handoff as an error would
//    strand a clinician who needs MFA at a dead end; treating it as
//    success would silently skip a required factor.
//
// A genuinely refused credential (wrong password or unknown user) is a
// THIRD, distinct outcome: the API answers both cases identically — same
// status, same body, same timing floor (spec D5) — specifically so a
// caller cannot learn which hospital accounts exist by trying login
// names. This page shows that ONE shared message in a single
// `role="alert"` element and never renders a per-field error for it
// (e.g. "no account with that email"), which would rebuild the same
// enumeration oracle the backend just closed.
//
// apps/shell/app/api/auth/callback/page.tsx is unchanged: it is still
// the target both `callback_url` (this page) and Zitadel's hosted login
// (a `handoff_url` this page navigated to) eventually redirect back to.
export default function LoginPage() {
  return (
    // Suspense boundary: useSearchParams() opts a page out of Next's
    // static shell unless something above it can suspend for the value.
    // There is no meaningful loading state to show here — the read is
    // synchronous on the client — so the fallback only matters for the
    // brief window Next's build tooling cares about, not anything a real
    // browser will ever see.
    <Suspense fallback={null}>
      <LoginPageContent />
    </Suspense>
  );
}

function LoginPageContent() {
  const authRequestId = useSearchParams().get("authRequest");
  if (authRequestId) {
    return <CredentialForm authRequestId={authRequestId} />;
  }
  return <RedirectLanding />;
}

const credentialsSchema = z.object({
  email: z.string().min(1, "Enter your email").email("Enter a valid email address"),
  password: z.string().min(1, "Enter your password"),
});

type Credentials = z.infer<typeof credentialsSchema>;

// The credential form itself (spec D2/D6). Renders only when Zitadel
// already handed this page a real auth request id — see LoginPage's
// header comment for the full outcome contract (`callback_url` /
// `handoff_url` / shared refusal) this drives against.
function CredentialForm({ authRequestId }: { authRequestId: string }) {
  const form = useZodForm(credentialsSchema, { email: "", password: "" });

  const submit = useApiMutation<PasswordCheckResult, Credentials>(
    (values) =>
      checkPassword({
        authRequestId,
        loginName: values.email,
        password: values.password,
      }),
    {
      // Both outcomes navigate — see the header comment on why a
      // handoff is not treated as a failure. window.location.assign
      // (not router.push): both URLs leave this Next.js app entirely,
      // either to the same /api/auth/callback route Zitadel's own
      // redirect also lands on, or to Zitadel's hosted login origin.
      onSuccess: (result) => {
        window.location.assign(
          result.outcome === "complete" ? result.callbackUrl : result.handoffUrl,
        );
      },
    },
  );

  return (
    <AuthLayoutCentered>
      <AuthCardCentered>
        <div className="space-y-2 text-center">
          <h1 className="text-2xl font-semibold tracking-tight">HMS</h1>
          <p className="text-sm text-muted-foreground">Sign in to continue.</p>
        </div>

        <form
          noValidate
          className="space-y-4"
          onSubmit={form.handleSubmit((values) => submit.mutate(values))}
        >
          <Field id="email" label="Email" error={form.formState.errors.email?.message}>
            <Input id="email" type="email" autoComplete="username" {...form.register("email")} />
          </Field>
          <Field id="password" label="Password" error={form.formState.errors.password?.message}>
            <Input
              id="password"
              type="password"
              autoComplete="current-password"
              {...form.register("password")}
            />
          </Field>

          {submit.error ? (
            // ONE message, deliberately not attached to either field —
            // see the header comment on spec D5. submit.error is
            // whatever ApiError.message the API answered with: the
            // shared refusal text for a wrong password or unknown user
            // alike, or (for the auth-request-expired / Zitadel-
            // unavailable cases) the API's own distinct, non-enumerating
            // wording for those.
            <p role="alert" className="text-sm text-destructive">
              {submit.error instanceof ApiError
                ? submit.error.message
                : "Sign-in failed. Please try again."}
            </p>
          ) : null}

          <Button type="submit" className="w-full" disabled={submit.isPending}>
            {submit.isPending ? "Signing in…" : "Sign in"}
          </Button>
        </form>

        <AuthCardFooter>
          <p className="text-xs text-muted-foreground">
            Your credential is checked directly by HMS.
          </p>
        </AuthCardFooter>
      </AuthCardCentered>
    </AuthLayoutCentered>
  );
}

// The pre-#854 landing page, UNCHANGED: shown whenever this route is
// reached with no auth request to act on. See LoginPage's header comment
// for when that is — the common case is arriving at /login directly, or
// as the post-logout redirect target, neither of which has an auth
// request to act on yet. Clicking the button below is what obtains one:
// getUserManager().signinRedirect() sends the browser to Zitadel's own
// /oauth/v2/authorize, which (per D1) redirects it right back here, this
// time WITH ?authRequest= set — landing in LoginPageContent's other
// branch, CredentialForm.
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
// who walks away without signing out. Their HMS session cookie is still
// valid, so the next person never reaches this page at all. That is #848
// (idle timeout), and it is the control that actually covers the ward
// terminal.
function RedirectLanding() {
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

  useEffect(() => {
    try {
      if (window.sessionStorage.getItem(SIGNED_OUT_MARK)) {
        setSignedOut(true);
        // Consumed: a reload, or coming back here later in the same tab,
        // is no longer "you just signed out".
        window.sessionStorage.removeItem(SIGNED_OUT_MARK);
      }
    } catch {
      // Blocked storage — fall back to the neutral wording.
    }
  }, []);

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
          <h1 className="text-2xl font-semibold tracking-tight">HMS</h1>
          <p className="text-sm text-muted-foreground">
            {signedOut ? "You are signed out." : "Sign in to continue."}
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
