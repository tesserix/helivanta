"use client";

import { useEffect, useState } from "react";
import {
  AuthCardCentered,
  AuthCardFooter,
  AuthLayoutCentered,
  Button,
} from "@tesserix/web";
import { SIGNED_OUT_MARK } from "@hms/ui";

import { getUserManager } from "@/lib/oidc";

// Uses @tesserix/web's AuthLayout chrome, deliberately WITHOUT its
// credential parts. That family also ships the pieces for a full sign-in
// form, and the temptation is to reach for them because they are right
// there and would make this page look "complete". They must not be used
// here: the moment HMS renders an email and password field it owns the
// credential surface again, which blocks hospital SSO and puts clinicians'
// passwords back through our frontend. This card holds one button, on
// purpose. AuthSocialProviders is unused for the same class of reason —
// HMS has no identity provider configured (spec D5, deferred until a
// hospital asks), so a federated sign-in button would offer a path that
// does not exist.
//
// Login is a redirect, not a form (design spec D5a). HMS renders no
// password field: Zitadel's hosted login at NEXT_PUBLIC_ZITADEL_ISSUER_URL
// owns the credential surface, MFA, password reset and lockout, so a
// compromised HMS frontend never has a password to harvest.
// apps/shell/app/api/auth/callback/page.tsx is the other half — the
// caller Zitadel redirects back to once the user has authenticated there.
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
export default function LoginPage() {
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
