import { workspaceSigninScope } from "./workspace-signin";

// oidc-client-ts throws this when the browser no longer holds the request it
// started: it expired (staleStateAgeInSeconds) or another tab started it.
// The wording is pinned by callback-state.test.ts against the real library.
export const MISSING_STATE = "No matching state found in storage";

export function redirectFailure(err: unknown): string {
  return err instanceof Error && err.message === MISSING_STATE
    ? "Your sign-in took too long. Sign in again."
    : "Sign-in didn't finish. Sign in again.";
}

// "Sign in again" never resumes the session already in this browser (#1820
// slice 2): a workspace named by this flow's own OIDC state reopens that
// workspace, and anything else starts a new unscoped sign-in.
export function signInAgainHref(tenantId?: string): string {
  if (tenantId) {
    try {
      workspaceSigninScope(tenantId);
      return `/login?workspace=${tenantId}&start=1`;
    } catch {
      // An off-contract tenant falls back to the email card, never a guess.
    }
  }
  return "/login?again=1";
}
