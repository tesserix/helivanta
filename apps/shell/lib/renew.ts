import { getUserManager } from "./oidc";
import { exchangeIdToken } from "./auth-exchange";

export class RenewalFailedError extends Error {}

// RENEWAL_INTERVAL_MS must stay comfortably below the API's SessionTTL
// (backend/internal/config/config.go, SESSION_TTL, default 15m) — this is
// a client-side constant, not a value read from the server, because the
// Helivanta session cookie is httpOnly and carries no client-readable expiry.
// One third of the default TTL: frequent enough that a session is never
// close to expiring under normal use, infrequent enough not to spam
// Zitadel's silent-auth endpoint or OpenFGA. If SESSION_TTL is ever
// lowered below ~15 minutes in a given environment, this constant must be
// lowered to match — nothing enforces that coupling today, which is a
// real gap worth flagging rather than hiding.
export const RENEWAL_INTERVAL_MS = 5 * 60 * 1000;

// renewSession is D4a's "renewal is the login exchange, run again with a
// fresh Zitadel token": silently re-authenticate against Zitadel
// (prompt=none, via oidc-client-ts's signinSilent() through the hidden
// iframe at SILENT_RENEW_PATH — a request that succeeds only because the
// browser still carries a live Zitadel SSO session cookie on
// auth.tesserix.app) and re-run the same login exchange the callback page
// uses, explicitly naming the tenant the caller is currently in.
//
// currentTenantId is REQUIRED, not defaulted: the exchange endpoint
// (POST /v1/auth/login) defaults to the caller's first tenant binding
// when tenant_id is omitted, and that default is only correct for a
// first login. Omitting it here would let a routine renewal silently
// move a multi-hospital clinician back to whichever tenant sorts first,
// discarding whichever hospital they actually switched to — the exact
// bug the "keeps the current tenant" mutation check below exists to
// catch.
//
// Any failure — Zitadel refusing the silent re-authentication (a
// deactivated or logged-out user, D4/D4a's whole point) or the exchange
// itself refusing (membership revoked since the last renewal) — surfaces
// as RenewalFailedError. The caller (components/session-renewal.tsx) is
// responsible for the visible-login fallback; this function never
// redirects itself, so it stays unit-testable without a DOM.
export async function renewSession(currentTenantId: string): Promise<void> {
  if (!currentTenantId) {
    throw new RenewalFailedError("renewSession requires the caller's current tenant id");
  }
  let idToken: string | undefined;
  try {
    const user = await getUserManager().signinSilent();
    idToken = user?.id_token;
  } catch (err) {
    throw new RenewalFailedError(
      err instanceof Error ? err.message : "Zitadel silent re-authentication failed",
    );
  }
  if (!idToken) {
    throw new RenewalFailedError("Zitadel silent re-authentication returned no id_token");
  }
  try {
    await exchangeIdToken(idToken, currentTenantId);
  } catch (err) {
    throw new RenewalFailedError(err instanceof Error ? err.message : "session renewal failed");
  }
}
