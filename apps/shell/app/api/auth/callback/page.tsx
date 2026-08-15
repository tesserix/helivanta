"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { clearPermissionsCache } from "@hms/api";
import { getUserManager } from "@/lib/oidc";
import { exchangeIdToken } from "@/lib/auth-exchange";

// The redirect target Zitadel sends the browser back to after a
// successful hosted login (registered as hms-web's redirect_uri,
// scripts/zitadel-bootstrap.mjs / scripts/lib/zitadel.mjs's
// DEV_REDIRECT_URI — the path here MUST match, see lib/oidc.ts's
// AUTH_CALLBACK_PATH).
//
// userManager.signinRedirectCallback() is where the PKCE + state contract
// actually lives, and it is entirely oidc-client-ts's, not hand-rolled
// here:
//  - it reads `state` off the callback URL, looks up the matching
//    SigninRequest oidc-client-ts stashed in sessionStorage at
//    signinRedirect() time (login/page.tsx), and throws if it is missing
//    or does not match — a forged or replayed callback URL with an
//    unknown/absent `state` never reaches exchangeIdToken below.
//  - that same stashed request carries the code_verifier PKCE needs; it is
//    submitted as part of the authorization_code token exchange
//    oidc-client-ts performs against Zitadel's token endpoint internally.
//    There is no separate "post the code" step in this file to get wrong.
//  - it also validates the ID token's `nonce` against what was sent at
//    signinRedirect() time, closing the token-replay gap `state` alone
//    does not cover.
//
// Only past all of that does this file see a `user` at all — exactly the
// gate this route is not allowed to skip.
export default function AuthCallbackPage() {
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);
  // signinRedirectCallback() consumes the authorization code — Zitadel
  // refuses a second exchange of the same code. React 19's Strict Mode
  // double-invokes effects in development, which would otherwise burn the
  // code on the first (discarded) run and fail the second with a
  // confusing "invalid_grant" instead of ever reaching the dashboard.
  const ran = useRef(false);

  useEffect(() => {
    if (ran.current) return;
    ran.current = true;

    (async () => {
      try {
        const user = await getUserManager().signinRedirectCallback();
        if (!user.id_token) {
          throw new Error("Zitadel did not return an id_token");
        }
        // Whoever signs in now owns this browser's permission cache — a
        // session that expired without an explicit sign-out leaves the
        // previous user's entry behind, and the next person signing in on
        // the same browser must not paint the old user's nav.
        clearPermissionsCache();
        // No tenant_id: this is a first login, so the backend defaults to
        // the caller's first tenant binding (login.go). Renewal
        // (lib/renew.ts) is the call site that must always name one.
        await exchangeIdToken(user.id_token);
        router.replace("/");
      } catch (err) {
        setError(err instanceof Error ? err.message : "Sign-in failed.");
      }
    })();
  }, [router]);

  if (error) {
    return (
      <main className="flex min-h-screen items-center justify-center p-4">
        <div className="max-w-sm space-y-3 text-center">
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
          <a href="/login" className="text-sm text-foreground underline underline-offset-4">
            Try signing in again
          </a>
        </div>
      </main>
    );
  }

  return (
    <main className="flex min-h-screen items-center justify-center p-4">
      <p className="text-sm text-muted-foreground">Signing you in…</p>
    </main>
  );
}
