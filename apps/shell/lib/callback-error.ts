// oidc-client-ts throws this when the browser no longer holds the request it
// started: it expired (staleStateAgeInSeconds) or another tab started it.
// The wording is pinned by callback-state.test.ts against the real library.
export const MISSING_STATE = "No matching state found in storage";

export function redirectFailure(err: unknown): string {
  return err instanceof Error && err.message === MISSING_STATE
    ? "Your sign-in took too long. Sign in again."
    : "Sign-in didn't finish. Sign in again.";
}
