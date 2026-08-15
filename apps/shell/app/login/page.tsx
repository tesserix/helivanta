"use client";

import { useState } from "react";
import { Button } from "@tesserix/web";

import { getUserManager } from "@/lib/oidc";

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
    <main className="flex min-h-screen items-center justify-center p-4">
      <div className="w-full max-w-sm space-y-6 text-center">
        <div className="space-y-2">
          <h1 className="text-2xl font-semibold tracking-tight">HMS</h1>
          <p className="text-sm text-muted-foreground">
            You are signed out. Continue to sign in.
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
      </div>
    </main>
  );
}
