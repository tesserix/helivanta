import { describe, expect, it, afterEach, vi } from "vitest";

import {
  renewSession,
  nextRenewalDelayMs,
  RenewalFailedError,
  RenewalUnavailableError,
  FALLBACK_RENEWAL_INTERVAL_MS,
  MIN_RENEWAL_DELAY_MS,
  MAX_RENEWAL_DELAY_MS,
} from "./renew";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status });
}

describe("renewSession", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // The load-bearing shape of the new endpoint: no body, no token, and
  // the existing session cookie carried same-origin — spec D1/D5. This
  // is the mutation-discriminating test for "renewal still depends on
  // the browser holding an IdP credential": if renewSession ever grew a
  // body again, this assertion on the exact init object would fail.
  it("POSTs to /api/v1/auth/renew with no body, same-origin credentials", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(jsonResponse(200, { tenant_id: "t1", renew_at: "2026-08-20T12:05:00Z" }));

    await renewSession();

    expect(fetchSpy).toHaveBeenCalledWith("/api/v1/auth/renew", {
      method: "POST",
      credentials: "same-origin",
    });
  });

  it("parses renew_at from a successful response", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          jsonResponse(200, { tenant_id: "t1", renew_at: "2026-08-20T12:05:00Z" }),
        ),
    );

    const result = await renewSession();

    expect(result.renewAt).toEqual(new Date("2026-08-20T12:05:00Z"));
  });

  // A 200 with a missing/unparseable renew_at is still a SUCCESSFUL
  // renewal (the cookie was set) — it must not throw. The caller falls
  // back to a default cadence via nextRenewalDelayMs, not to a logout.
  it("resolves with renewAt undefined when the response omits renew_at", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(200, { tenant_id: "t1" })));

    const result = await renewSession();

    expect(result.renewAt).toBeUndefined();
  });

  it("resolves with renewAt undefined when renew_at is not a parseable date", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(jsonResponse(200, { tenant_id: "t1", renew_at: "not-a-date" })),
    );

    const result = await renewSession();

    expect(result.renewAt).toBeUndefined();
  });

  // The judgement call this task turns on: 401 (renew.go's cookie-
  // missing / inactive-subject / expired-idle-deadline refusals) means
  // the session is genuinely over.
  it("throws RenewalFailedError on 401", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          jsonResponse(401, { error: "unauthenticated", message: "account is no longer active" }),
        ),
    );

    await expect(renewSession()).rejects.toBeInstanceOf(RenewalFailedError);
  });

  // 404 is renew.go's respondNoAccessibleTenant — membership revoked
  // since the last renewal. Also genuinely over.
  it("throws RenewalFailedError on 404", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          jsonResponse(404, { error: "not_found", message: "no accessible tenant" }),
        ),
    );

    await expect(renewSession()).rejects.toBeInstanceOf(RenewalFailedError);
  });

  // 503 is renew.go's fail-closed answer to an unreadable Zitadel/
  // OpenFGA/signer dependency (identity_unavailable, authz_unavailable,
  // session_unavailable) — the server could not answer, it did not
  // answer "no". Must NOT be treated the same as a 401/404.
  it("throws RenewalUnavailableError, not RenewalFailedError, on 503", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse(503, {
          error: "identity_unavailable",
          message: "could not verify account status",
        }),
      ),
    );

    await expect(renewSession()).rejects.toBeInstanceOf(RenewalUnavailableError);
    await expect(renewSession()).rejects.not.toBeInstanceOf(RenewalFailedError);
  });

  // 429 is RenewRateLimitRule — also "try again", never "log out".
  it("throws RenewalUnavailableError, not RenewalFailedError, on 429", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          jsonResponse(429, { error: "rate_limited", message: "too many requests" }),
        ),
    );

    await expect(renewSession()).rejects.toBeInstanceOf(RenewalUnavailableError);
    await expect(renewSession()).rejects.not.toBeInstanceOf(RenewalFailedError);
  });

  // A network-level failure to even reach the same-origin endpoint is
  // the same "could not answer" bucket as a 503, not a verdict.
  it("throws RenewalUnavailableError when the request itself fails", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("network error")));

    await expect(renewSession()).rejects.toBeInstanceOf(RenewalUnavailableError);
  });
});

describe("nextRenewalDelayMs", () => {
  const now = () => new Date("2026-08-20T12:00:00Z").getTime();

  it("uses the fallback interval when renewAt is undefined", () => {
    expect(nextRenewalDelayMs(undefined, now)).toBe(FALLBACK_RENEWAL_INTERVAL_MS);
  });

  it("uses the server-provided delay when it falls within the bounds", () => {
    const renewAt = new Date("2026-08-20T12:07:00Z"); // 7 minutes out
    expect(nextRenewalDelayMs(renewAt, now)).toBe(7 * 60 * 1000);
  });

  // Proves the floor actually clamps: a renewAt in the past (clock skew,
  // or a renewal response that arrived late) must not produce a
  // near-zero/negative delay that would retry-storm the endpoint.
  it("clamps a past or near-immediate renewAt to MIN_RENEWAL_DELAY_MS", () => {
    const renewAt = new Date("2026-08-20T11:59:00Z"); // 1 minute in the past
    expect(nextRenewalDelayMs(renewAt, now)).toBe(MIN_RENEWAL_DELAY_MS);
  });

  // Proves the ceiling actually clamps: a misconfigured, very long
  // SESSION_TTL must not produce an unbounded sleep.
  it("clamps a far-future renewAt to MAX_RENEWAL_DELAY_MS", () => {
    const renewAt = new Date("2026-08-21T00:00:00Z"); // 12 hours out
    expect(nextRenewalDelayMs(renewAt, now)).toBe(MAX_RENEWAL_DELAY_MS);
  });
});
