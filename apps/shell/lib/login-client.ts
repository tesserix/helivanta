import { apiFetch } from "@helivanta/api";

// The wire shape GET /v1/auth/login/request/:id answers with —
// authRequestResponse in loginui.go. Fetched directly via useApiQuery in
// apps/shell/app/login/page.tsx's ValidatedCredentialForm (no wrapper
// function here, unlike checkPassword below — a plain GET with no body
// to build and no discriminated outcome to narrow needs nothing this
// file would add over calling useApiQuery with this type directly, the
// same pattern every other read in this codebase follows, e.g.
// apps/medicore/components/visit-panel.tsx's `useApiQuery<Visit[]>(...)`).
//
// HMS's login form reads this on mount, before rendering the credential
// fields, so that an auth request that is already unknown, expired, or
// already used (a browser left on the login page overnight — spec D3's
// "errors and failure handling" section calls this reachable in normal
// use) is caught and shown as "start again" BEFORE a clinician types a
// credential into a form that can only ever fail. That submit-time
// handling (checkPassword below) stays in place as the backstop for an
// auth request that goes stale in the gap between this read succeeding
// and the credential being submitted.
export interface AuthRequestInfo {
  id: string;
  client_id: string;
  redirect_uri: string;
  scope: string[];
}

// Typed wrapper around POST /v1/auth/login/password
// (backend/internal/modules/iam/loginui.go), the credential check that
// drives Zitadel's login-client API on HMS's behalf (spec D2, #854). This
// is NOT the raw-fetch exception documented in
// docs/standards/frontend.md §3 (that one is exchangeIdToken in
// lib/auth-exchange.ts, which posts to POST /v1/auth/login and installs
// the session cookie): this endpoint never touches a cookie, it only
// answers with a URL to go to next, so it is an ordinary `/api/v1` call
// through `apiFetch` like any other mutation in this codebase.
//
// checkPassword deliberately returns a DISCRIMINATED union rather than an
// object with two optional fields. The backend answers with exactly one
// of `callback_url` (login is complete) or `handoff_url` (HMS cannot
// finish this login itself — see the outcome comment below) and never
// both; the union makes "read the wrong field" a compile error at every
// call site instead of a `undefined` that only surfaces at runtime.
export type PasswordCheckResult =
  { outcome: "complete"; callbackUrl: string } | { outcome: "handoff"; handoffUrl: string };

interface PasswordCheckParams {
  authRequestId: string;
  loginName: string;
  password: string;
}

// The raw wire shape POST /v1/auth/login/password answers with —
// `passwordSuccessResponse` or `passwordHandoffResponse` in loginui.go,
// whichever the backend chose. Never both keys populated; see
// checkPassword's doc comment on why the return type does not mirror
// this directly.
interface PasswordCheckResponse {
  callback_url?: string;
  handoff_url?: string;
}

// checkPassword is the ONLY thing HMS's login form does with a
// credential: hand it to the API and act on the answer. A `handoff`
// outcome is NOT a failure — spec D4 is explicit that Zitadel does not
// enforce MFA for a login client, so the API decides sufficiency itself
// and answers `handoff` whenever a password alone is not enough (MFA
// required by org policy, `forceMfaLocalOnly`, a user's own voluntarily
// enrolled second factor, a federated hospital IdP, or a policy it could
// not read). A forced password change is NOT one of them, though an
// earlier version of this comment listed it: verified live 2026-08-16
// (#854 Task 8, spike §5), Zitadel signals `passwordChangeRequired` to a
// login client nowhere in the flow, so the API COMPLETES those logins
// instead — tracked as #856. The caller MUST navigate to `handoffUrl`
// exactly as it would `callbackUrl` on success — treating a handoff as an
// error would strand a clinician who needs MFA at a dead end, and
// treating it as success would skip a required factor entirely.
//
// A refused credential (wrong password or unknown user, answered
// identically per spec D5) surfaces as an `ApiError` thrown by
// `apiFetch` — its `.message` is the ONE shared refusal text
// ("email or password is incorrect"), never anything that names which
// field was wrong, so the caller can render it verbatim without
// reconstructing the enumeration oracle the backend already closed.
export async function checkPassword(params: PasswordCheckParams): Promise<PasswordCheckResult> {
  const body = await apiFetch<PasswordCheckResponse>("/auth/login/password", {
    method: "POST",
    body: JSON.stringify({
      auth_request_id: params.authRequestId,
      login_name: params.loginName,
      password: params.password,
    }),
  });

  if (body.callback_url) {
    return { outcome: "complete", callbackUrl: body.callback_url };
  }
  if (body.handoff_url) {
    return { outcome: "handoff", handoffUrl: body.handoff_url };
  }
  // Neither key present is a contract violation by the API, not a user
  // error — there is no credential-shaped explanation for it, so it must
  // not be folded into the shared refusal message above.
  throw new Error("Sign-in could not be completed: the server sent an unexpected response.");
}
