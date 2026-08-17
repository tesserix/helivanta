import { describe, expect, it, vi, afterEach } from "vitest";

import { exchangeIdToken } from "./auth-exchange";

describe("exchangeIdToken", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // Spec D3's discriminator (a renewal carries idle_deadline forward; a
  // genuine login gets a fresh one) depends on the backend seeing the Helivanta
  // session cookie on THIS request. That only happens today because the
  // URL is relative/same-origin AND fetch's credentials mode is explicitly
  // "same-origin" — nothing enforces either fact, so this pins both.
  it("sends the session cookie, because the backend's renewal-vs-login discriminator depends on it", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(new Response(JSON.stringify({ tenant_id: "t1" }), { status: 200 }));

    await exchangeIdToken("id-token", "11111111-1111-1111-1111-111111111111");

    const [url, init] = fetchSpy.mock.calls[0]!;
    expect(String(url).startsWith("/")).toBe(true); // same-origin, relative
    expect((init as RequestInit).credentials).toBe("same-origin");
  });
});
