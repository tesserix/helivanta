import { describe, expect, it, vi, beforeEach } from "vitest";

const signinSilent = vi.hoisted(() => vi.fn());
const getUserManager = vi.hoisted(() => vi.fn());
vi.mock("./oidc", () => ({ getUserManager }));

import { renewSession, RenewalFailedError } from "./renew";

describe("renewSession", () => {
  beforeEach(() => {
    signinSilent.mockReset();
    getUserManager.mockReturnValue({ signinSilent });
  });

  // THE mutation-discriminating test for "silent renew reuses a stale
  // tenant": exchangeIdToken must be called with EXACTLY the
  // currentTenantId the caller passed in, never omitted (which would let
  // POST /v1/auth/login fall back to the caller's first tenant binding —
  // design spec D4a's explicit failure mode: a routine renewal silently
  // moving a multi-hospital clinician back to whichever tenant sorts
  // first, discarding the hospital they actually switched to).
  it("re-mints the session for exactly the tenant it was told, never a default", async () => {
    signinSilent.mockResolvedValue({ id_token: "fresh-id-token" });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ tenant_id: "t2" }) }),
    );

    await renewSession("t2");

    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/auth/login",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ id_token: "fresh-id-token", tenant_id: "t2" }),
      }),
    );
    vi.unstubAllGlobals();
  });

  it("refuses to run without a tenant id, rather than silently omitting it", async () => {
    await expect(renewSession("")).rejects.toBeInstanceOf(RenewalFailedError);
    expect(signinSilent).not.toHaveBeenCalled();
  });

  // D4/D4a's whole point: a deactivated account or a revoked membership
  // must fail the silent re-authentication, and that failure must surface
  // as a distinguishable error the caller (components/session-renewal.tsx)
  // can act on — not be swallowed or retried into an infinite loop.
  it("surfaces a Zitadel silent re-authentication failure as RenewalFailedError", async () => {
    signinSilent.mockRejectedValue(new Error("login_required"));

    await expect(renewSession("t1")).rejects.toThrow(RenewalFailedError);
  });

  it("surfaces a refused exchange (e.g. membership revoked since the last renewal) as RenewalFailedError", async () => {
    signinSilent.mockResolvedValue({ id_token: "fresh-id-token" });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
        json: async () => ({ error: "not_found", message: "tenant not found" }),
      }),
    );

    await expect(renewSession("t1")).rejects.toThrow(RenewalFailedError);
    vi.unstubAllGlobals();
  });

  it("treats a silent re-authentication that returns no id_token as a failure", async () => {
    signinSilent.mockResolvedValue({ id_token: undefined });

    await expect(renewSession("t1")).rejects.toThrow(RenewalFailedError);
  });
});
