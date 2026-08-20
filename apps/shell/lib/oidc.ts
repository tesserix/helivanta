import { UserManager, WebStorageStateStore, type UserManagerSettings } from "oidc-client-ts";
import { env } from "./env";

declare global {
  interface Window {
    // Dev/test-only escape hatch — the direct successor to the old
    // window.__hmsFirebaseAuth (apps/shell/lib/firebase.ts, deleted
    // #838). e2e/tests/signout.spec.ts reconstructs the shared-workstation
    // attack (#781) from inside `page.evaluate`, which runs as a plain
    // browser script with no bundler and therefore cannot
    // `import("oidc-client-ts")`. Stashing the already-constructed
    // UserManager on `window` gives the test the same object getUserManager()
    // would hand back. Never set in production — see below.
    __hmsUserManager?: UserManager;
  }
}

// AUTH_CALLBACK_PATH must match the redirect URI
// scripts/zitadel-bootstrap.mjs registers on the helivanta-web app
// (scripts/lib/zitadel.mjs's DEV_REDIRECT_URI) — a mismatch is refused by
// Zitadel at /oauth/v2/authorize with "redirect_uri not allowed", not
// silently accepted.
export const AUTH_CALLBACK_PATH = "/api/auth/callback";

let manager: UserManager | undefined;

function absoluteUrl(path: string): string {
  return `${window.location.origin}${path}`;
}

function settings(): UserManagerSettings {
  return {
    authority: env.NEXT_PUBLIC_ZITADEL_ISSUER_URL,
    client_id: env.NEXT_PUBLIC_ZITADEL_CLIENT_ID,
    redirect_uri: absoluteUrl(AUTH_CALLBACK_PATH),
    // Sign-out (packages/ui/src/hms-shell.tsx's default handleSignOut, via
    // zitadel-session.ts's endZitadelSession) always ends in an
    // RP-initiated logout redirect to Zitadel's end_session endpoint;
    // /login is where Zitadel sends the browser back afterward, and it is
    // also where middleware.ts sends any unauthenticated request, so a
    // login this page then re-triggers reads correctly either way.
    post_logout_redirect_uri: absoluteUrl("/login"),
    // Authorization code + PKCE (spec D1/D5a; helivanta-web is provisioned as a
    // public client, authMethodType NONE, in scripts/zitadel-bootstrap.mjs).
    // oidc-client-ts generates a fresh code_verifier per signinRedirect()
    // call, derives its S256 code_challenge, and stores the verifier
    // keyed by `state` in userStore below — the callback page
    // (app/api/auth/callback/page.tsx) never sees or handles either
    // value directly, and can't accidentally skip them.
    response_type: "code",
    scope: "openid profile email",
    // userinfo is never called — spec D1/D2: the Helivanta session carries
    // exactly sub/tenant_id/auth_time/exp/iat/iss, no email or name, so
    // there is nothing here that would need it.
    loadUserInfo: false,
    // D4a, load-bearing: Helivanta stores no IdP refresh token. Leaving
    // useRefreshToken at its default (false) and never requesting the
    // `offline_access` scope means oidc-client-ts never asks Zitadel for
    // one in the first place. This UserManager is no longer involved in
    // renewal at all (spec D1/D5, #916): renewal is a same-origin
    // POST /v1/auth/renew carrying only the existing Helivanta session
    // cookie (lib/renew.ts), never a Zitadel round trip through the
    // browser. automaticSilentRenew stays false because there is no
    // Zitadel-driven silent renewal left to automate — this UserManager
    // now exists solely for the initial signinRedirect() (login/page.tsx)
    // and its signinRedirectCallback() (app/api/auth/callback/page.tsx).
    automaticSilentRenew: false,
    // sessionStorage, not localStorage: the stored value includes the
    // raw id_token, and a tab-scoped lifetime is the more conservative
    // default for a system holding patient records — it does not
    // silently outlive the browser tab the way localStorage would.
    userStore: new WebStorageStateStore({ store: window.sessionStorage }),
  };
}

// getUserManager returns the shell's single oidc-client-ts UserManager,
// constructing it on first call. Client-only: env.NEXT_PUBLIC_ZITADEL_*
// values are safe to read anywhere, but WebStorageStateStore needs
// `window`, so this must never run during server-side rendering.
export function getUserManager(): UserManager {
  if (!manager) {
    manager = new UserManager(settings());
    if (process.env.NODE_ENV !== "production") {
      window.__hmsUserManager = manager;
    }
  }
  return manager;
}
