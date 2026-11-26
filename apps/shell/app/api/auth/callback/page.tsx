"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import {
  ApiError,
  clearPermissionsCache,
  storeExpiresAt,
  storeRenewAt,
  notifySessionChanged,
  continueCustomerSignup,
} from "@helivanta/api";
import { Button } from "@tesserix/web";
import { TenantLoginCard } from "@/components/login-branding";
import { getUserManager } from "@/lib/oidc";
import { redirectFailure, signInAgainHref } from "@/lib/callback-error";
import { ExchangeError, exchangeIdToken } from "@/lib/auth-exchange";
import { customerSignupSigninStateContract } from "@/lib/signup-signin";

// The redirect target Zitadel sends the browser back to after a
// successful hosted login (registered as helivanta-web's redirect_uri,
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
  const [tenant, setTenant] = useState<string>();
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
      // Library and identity-provider errors are not for the reader; only the
      // exchange below carries Helivanta's own wording (#1767).
      let stage: "redirect" | "exchange" = "redirect";
      try {
        const user = await getUserManager().signinRedirectCallback();
        if (!user.id_token) {
          throw new Error("Zitadel did not return an id_token");
        }
        stage = "exchange";
        const signup = customerSignupSigninStateContract.optional().parse(user.state ?? undefined);
        if (signup && "tenantId" in signup) setTenant(signup.tenantId);
        if (signup?.purpose === "customer-signup") {
          await continueCustomerSignup({ idToken: user.id_token });
          router.replace("/signup");
          return;
        }
        // Whoever signs in now owns this browser's permission cache — a
        // session that expired without an explicit sign-out leaves the
        // previous user's entry behind, and the next person signing in on
        // the same browser must not paint the old user's nav.
        clearPermissionsCache();
        const { renew_at, expires_at } = await exchangeIdToken(
          user.id_token,
          signup && "tenantId" in signup ? signup.tenantId : undefined,
        );
        // The server's own schedule for this session's FIRST renewal
        // (#916 Task 4, F3). Stored rather than passed, because
        // components/session-renewal.tsx does not mount on this page —
        // it is skipped on the auth-callback path — and only comes up
        // once the navigation below lands. Without this the first
        // renewal falls back to a hardcoded five-minute client constant,
        // which silently outlives any SESSION_TTL shorter than that.
        storeRenewAt(renew_at);
        // The session's expiry, for the renewal loop's retry cadence after a
        // failed renewal (#941) — stored for the same reason as renew_at.
        storeExpiresAt(expires_at);
        notifySessionChanged();
        router.replace("/dashboard");
      } catch (err) {
        // Only our own wording is shown: the login exchange's ExchangeError or
        // the signup call's ApiError. A fetch TypeError gets the fixed
        // sentence instead (#1856 D2).
        setError(
          stage !== "exchange"
            ? redirectFailure(err)
            : err instanceof ExchangeError || err instanceof ApiError
              ? err.message
              : "Check your connection and try again.",
        );
      }
    })();
  }, [router]);

  if (error) {
    return (
      <TenantLoginCard>
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
        <Button asChild className="w-full">
          <a href={signInAgainHref(tenant)}>Sign in again</a>
        </Button>
      </TenantLoginCard>
    );
  }

  return (
    <TenantLoginCard>
      <p role="status" className="text-sm text-muted-foreground">
        Signing you in…
      </p>
    </TenantLoginCard>
  );
}
