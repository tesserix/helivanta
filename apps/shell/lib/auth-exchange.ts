// exchangeIdToken posts a Zitadel ID token to POST /v1/auth/login
// (backend/internal/modules/iam/login.go), which verifies it, resolves
// tenant membership from OpenFGA, and — on success — mints the HMS
// session and sets it as this response's Set-Cookie. The API is the only
// thing holding the signing key (design spec D1), so this route, unlike
// the old apps/shell/app/api/session/route.ts it replaces, never touches
// the cookie itself; the browser gets it automatically because the
// request goes through next.config.ts's same-origin /api rewrite, which
// forwards the backend's Set-Cookie header untouched.
//
// Raw fetch, not `apiFetch` from @helivanta/api: this is the one auth route
// outside the `/api/v1` envelope's error-shape contract that runs before
// an HMS session exists — the same sanctioned exception the old
// `/api/session` POST was (docs/standards/frontend.md §3). It is used by
// both the login callback (app/api/auth/callback/page.tsx) and silent
// renewal (lib/renew.ts).
//
// tenantId is omitted on first login (the backend defaults to the
// caller's first tenant binding) and REQUIRED on renewal — D4a is
// explicit that renewal must name the tenant the session is currently
// in, never let the backend re-pick a default, or a renewal could
// silently move a clinician to a different hospital mid-shift.
export async function exchangeIdToken(
  idToken: string,
  tenantId?: string,
): Promise<{ tenant_id: string }> {
  const res = await fetch("/api/v1/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(
      tenantId ? { id_token: idToken, tenant_id: tenantId } : { id_token: idToken },
    ),
    // Spec D3: the backend tells a renewal from a genuine login by whether
    // THIS request carries the HMS session cookie — carrying it forward
    // means "carry idle_deadline forward too"; its absence means "mint a
    // fresh window". fetch's default credentials mode is already
    // "same-origin" (this URL is relative/same-origin), so this is
    // currently a no-op — but stated explicitly and pinned by
    // auth-exchange.test.ts, because a silent default is one refactor (an
    // absolute URL, a different origin, "credentials: omit") away from
    // dropping the cookie, which would make every renewal take the
    // fresh-window branch and let an untouched tab renew itself forever —
    // the exact failure this feature exists to prevent — with every
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
  return (await res.json()) as { tenant_id: string };
}
