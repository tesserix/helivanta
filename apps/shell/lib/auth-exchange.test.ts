import { describe, expect, it, vi, afterEach } from "vitest";

import { exchangeIdToken } from "./auth-exchange";

describe("exchangeIdToken", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // Spec D3's login-vs-renewal discriminator depends on the backend
  // seeing whatever Helivanta session cookie the browser is currently
  // holding (a stale one, from before this login) on THIS request. That
  // only happens today because the URL is relative/same-origin AND
  // fetch's credentials mode is explicitly "same-origin" — nothing
  // enforces either fact, so this pins both. This function's only
  // caller is now the login callback (renewal moved to a same-origin
  // POST /v1/auth/renew with no ID token at all — lib/renew.ts, #916),
  // so the cookie this test is really pinning is a POSSIBLY-STALE one
  // the backend still needs to see, not a fresh renewal's live one.
  it("sends the session cookie, because the backend's login-vs-renewal discriminator depends on it", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(new Response(JSON.stringify({ tenant_id: "t1" }), { status: 200 }));

    await exchangeIdToken("id-token");

    const [url, init] = fetchSpy.mock.calls[0]!;
    expect(String(url).startsWith("/")).toBe(true); // same-origin, relative
    expect((init as RequestInit).credentials).toBe("same-origin");
  });
});
