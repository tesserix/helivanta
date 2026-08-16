import { UserManager, WebStorageStateStore } from "oidc-client-ts";

/**
 * Ends the browser's Zitadel SSO session, for whichever HMS app the
 * caller sits in.
 *
 * **Why this lives in `@hms/ui`, not only in `apps/shell`.** Sign-out was
 * first built shell-only (`apps/shell/lib/sign-out.ts`, wired as
 * `HmsShell`'s `onSignOut` prop) on the same reasoning as `tenantPicker`:
 * only the shell app carried Zitadel client config. That reasoning turned
 * out to be wrong for sign-out specifically, and the gap was found live,
 * not theorised: signing out from a ZONE page (`/medicore/opd`, say) hit
 * `HmsShell`'s built-in default, which revoked the HMS session but never
 * touched Zitadel — so Zitadel's own SSO cookie survived, and the very
 * next login on that browser (even for a DIFFERENT identity) completed
 * silently with no credential prompt, reusing whoever's session was still
 * live. On a shared ward terminal that is precisely the failure D5a's
 * sign-out design exists to prevent, and it was happening on the majority
 * path — most sign-outs happen from a zone page, not the dashboard.
 *
 * **How this can work without a UserManager per app.** Every HMS app is
 * served under the SAME browser origin via `next.config.ts`'s rewrites
 * (`apps/shell` proxies `/medicore`, `/pharmacy`, `/lab` to their own
 * dev servers), and oidc-client-ts keys its stored user in
 * `sessionStorage` — itself origin-scoped, not per-bundle — as
 * `` `user:${authority}:${client_id}` `` (verified against
 * `oidc-client-ts@3.5.0`'s source; no other setting affects the key). A
 * fresh `UserManager` constructed here, in whichever app's bundle
 * `HmsShell` happens to be running in, reads and writes the EXACT SAME
 * entry `apps/shell/lib/oidc.ts`'s `UserManager` does — so `getUser()`
 * here sees the id_token the shell's login flow stored, and
 * `signoutRedirect()` can build a real `id_token_hint`, from any zone.
 *
 * **What this deliberately does NOT do.** No login, callback, or silent
 * renewal — those still only make sense centrally, in the shell, which
 * owns the redirect_uri/silent_redirect_uri Zitadel is configured with.
 * This module only ever calls `signoutRedirect()` and `removeUser()`, the
 * two operations that need no redirect target other than
 * `post_logout_redirect_uri` (which every app already agrees is `/login`).
 */
let manager: UserManager | undefined;

function settings() {
  // Every app that renders HmsShell already has these inlined at build
  // time: the Makefile exports NEXT_PUBLIC_ZITADEL_ISSUER_URL/CLIENT_ID
  // into the SAME `pnpm turbo dev` process tree every `next dev` child
  // inherits from (Makefile's comment on these vars explains the
  // dev-infra ordering), and each app's own build config must set the
  // same pair for production. Read directly rather than through
  // `@hms/api`'s `defineEnv` (which throws on a missing value): a zone
  // app that has not been wired with these yet must still render its
  // dashboard and every other feature — only sign-out's Zitadel-ending
  // step degrades, to the same-origin `/login` fallback below, not the
  // whole app.
  const issuer = process.env.NEXT_PUBLIC_ZITADEL_ISSUER_URL;
  const clientId = process.env.NEXT_PUBLIC_ZITADEL_CLIENT_ID;
  if (!issuer || !clientId) return null;
  return {
    authority: issuer,
    client_id: clientId,
    // Required by UserManagerSettings's type even though this module
    // never redirects here — no login flow is initiated from this file.
    redirect_uri: `${window.location.origin}/api/auth/callback`,
    post_logout_redirect_uri: `${window.location.origin}/login`,
    userStore: new WebStorageStateStore({ store: window.sessionStorage }),
  };
}

function getSharedUserManager(): UserManager | null {
  if (manager) return manager;
  const config = settings();
  if (!config) return null;
  manager = new UserManager(config);
  return manager;
}

/**
 * Ends the Zitadel SSO session and clears whatever local oidc-client-ts
 * state this origin holds, then leaves navigation to the caller — unless
 * it can't, in which case it falls back to a same-origin `/login`
 * redirect itself so a sign-out always ends up somewhere signed-out-
 * looking rather than stuck.
 *
 * Returns `true` if `signoutRedirect()` actually ran (meaning it now
 * owns navigation — the caller must not also navigate, or it will cancel
 * the in-flight cross-origin redirect and undo the whole point, exactly
 * as `apps/shell/lib/sign-out.ts` documents), `false` if it fell back to
 * the same-origin redirect itself.
 */
// SIGNED_OUT_MARK records that the user actually signed out, so the login
// page can say so. Without it /login cannot distinguish a clinician who
// just ended their shift from someone who simply opened the app and was
// redirected there by middleware — and telling the latter "You are signed
// out" is plainly false.
//
// sessionStorage rather than a query parameter: post_logout_redirect_uri
// is matched against what is registered on the Zitadel client, so
// decorating it risks the logout being rejected outright. Per-tab is also
// the right scope — signing out in one tab should not relabel another.
export const SIGNED_OUT_MARK = "hms.signed-out";

// IDLE_ENDED_MARK is SIGNED_OUT_MARK's counterpart for design spec D6: the
// session ended because the client (onExpire) or the server (a 401
// `session_idle` refusal) decided the clinician had been idle too long,
// which is a DIFFERENT event from a deliberate sign-out and must say so.
// #850 exists precisely because claiming someone signed out when they did
// not is untrue, and telling an idle-ended session "you signed out" is the
// same class of lie — on a shared terminal it wrongly implies a deliberate
// act ended the previous person's session, when in fact inactivity did.
//
// Set by the caller BEFORE calling endZitadelSession() (packages/ui/src/
// hms-shell.tsx's teardown), mirroring how SIGNED_OUT_MARK is set inside
// this module immediately before signoutRedirect() navigates away —
// endZitadelSession() itself (or its same-origin `/login` fallback) is
// what actually navigates, so anything set after it runs may never run at
// all.
//
// sessionStorage, per-tab, for the same reasons as SIGNED_OUT_MARK: a
// query parameter on post_logout_redirect_uri risks the logout request
// being rejected by Zitadel's registered-redirect check, and per-tab scope
// is correct — one tab's idle expiry should not relabel another tab.
export const IDLE_ENDED_MARK = "hms.idle-ended";

export async function endZitadelSession(): Promise<boolean> {
  const userManager = getSharedUserManager();
  if (!userManager) {
    // No Zitadel config reachable from this app/bundle — degrade to a
    // same-origin redirect rather than leaving the browser on a signed-
    // out-looking page that never actually navigates anywhere.
    window.location.href = "/login";
    return false;
  }
  try {
    // Set before redirecting: signoutRedirect() navigates away, so
    // anything after it may never run.
    try {
      window.sessionStorage.setItem(SIGNED_OUT_MARK, "1");
    } catch {
      // Private mode or a blocked store — the message is cosmetic, and a
      // sign-out that works but is worded generically beats one that
      // throws.
    }
    // signoutRedirect() BEFORE removeUser() — load-bearing, not
    // stylistic: it reads `id_token_hint` from the still-stored user, and
    // clearing first would send Zitadel's end_session request with no way
    // to identify which SSO session to end (see this module's own doc
    // comment, and apps/shell/lib/sign-out.ts's matching note).
    await userManager.signoutRedirect();
    await userManager.removeUser();
    return true;
  } catch {
    window.location.href = "/login";
    return false;
  }
}
