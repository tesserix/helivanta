// exchangeIdToken posts a Zitadel ID token to POST /v1/auth/login
// (backend/internal/modules/iam/login.go), which verifies it, resolves
// tenant membership from OpenFGA, and — on success — mints the Helivanta
// session and sets it as this response's Set-Cookie. The API is the only
// thing holding the signing key (design spec D1), so this route, unlike
// the old apps/shell/app/api/session/route.ts it replaces, never touches
// the cookie itself; the browser gets it automatically because the
// request goes through next.config.ts's same-origin /api rewrite, which
// forwards the backend's Set-Cookie header untouched.
//
// Raw fetch, not `apiFetch` from @helivanta/api: this is an auth-lifecycle
// route outside the `/api/v1` envelope's error-shape contract that runs
// before a Helivanta session exists — the sanctioned exception
// docs/standards/frontend.md §3 carves out for exactly this class of
// call (POST /v1/auth/login here; POST /v1/auth/renew, lib/renew.ts's
// renewSession, is the same exception's other member).
//
// Its only caller is the login callback
// (app/api/auth/callback/page.tsx) — #916 (design spec D1) moved
// renewal to a same-origin POST /v1/auth/renew that carries no ID
// token at all, so this function no longer has a renewal caller and no
// longer takes a tenantId. Renewal used to call this with a REQUIRED
// tenantId, precisely to stop a routine renewal from silently moving a
// multi-hospital clinician back to whichever tenant sorts first — see
// renew.go and Task 3's own report for how POST /v1/auth/renew now
// preserves that guarantee without a tenant_id in the request at all
// (it re-mints for the cookie's existing tenant, never a fresh pick).
// tenantId is omitted here, unconditionally: the backend defaults to
// the caller's first tenant binding, which is only ever reached from a
// genuine first login (this function's one remaining caller).
// LoginResult.renew_at is the server's answer to "when should this
// browser first call POST /v1/auth/renew" (#916 Task 4, F3). It is
// OPTIONAL on this type, deliberately: an older API, or any response
// whose body loses the field, must degrade to the documented fallback
// cadence rather than throw out of the sign-in path. See
// packages/api/src/renew-schedule.ts for why the value has to survive the navigation
// that follows this call, and backend/internal/modules/iam/renew.go's
// renewAtFor for the single place both endpoints compute it.
export interface LoginResult {
  tenant_id: string;
  renew_at?: string;
}

export async function exchangeIdToken(idToken: string): Promise<LoginResult> {
  const res = await fetch("/api/v1/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ id_token: idToken }),
    // Spec D3: the backend tells a renewal from a genuine login by whether
    // THIS request carries the Helivanta session cookie — carrying it forward
    // means "carry idle_deadline forward too"; its absence means "mint a
    // fresh window". fetch's default credentials mode is already
    // "same-origin" (this URL is relative/same-origin), so this is
    // currently a no-op — but stated explicitly and pinned by
    // auth-exchange.test.ts, because a silent default is one refactor (an
    // absolute URL, a different origin, "credentials: omit") away from
    // dropping the cookie. A stale, still-unexpired session cookie
    // present on this same login request (a clinician re-authenticating
    // through Zitadel while an old Helivanta session cookie is still
    // live) needs the backend to see it in order to make that
    // login-vs-renewal call correctly — silently dropping it here would
    // remove information the backend is entitled to have, with every
    // backend test still green.
    credentials: "same-origin",
  });
  if (!res.ok) {
    let message = "Sign-in failed. Please try again.";
    try {
      const body: unknown = await res.json();
      if (
        body &&
        typeof body === "object" &&
        "message" in body &&
        typeof (body as { message: unknown }).message === "string"
      ) {
        message = (body as { message: string }).message;
      }
    } catch {
      // non-JSON error body — fall back to the generic message
    }
    throw new Error(message);
  }
  return (await res.json()) as LoginResult;
}
