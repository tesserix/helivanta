import { describe, expect, it, afterEach, vi } from "vitest";

import {
  renewSession,
  nextRenewalDelayMs,
  retryDelayMs,
  RenewalFailedError,
  RenewalUnavailableError,
  FALLBACK_RENEWAL_INTERVAL_MS,
  MIN_RENEWAL_DELAY_MS,
  MAX_RENEWAL_DELAY_MS,
} from "./renew";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status });
}

function nonJsonResponse(status: number): Response {
  return new Response("<html>not json</html>", {
    status,
    headers: { "content-type": "text/html" },
  });
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

  // Fix round 1, CRITICAL 1: a bare 500 (gin.Recovery on a panic, or the
  // Next.js rewrite proxy answering on the API's behalf when API_URL is
  // unreachable during a rolling deploy) never reaches renew.go's own
  // deliberate 503 translation — it is a DIFFERENT layer failing. This
  // must classify as "could not answer", not "log out", or a routine
  // API deploy evicts every clinician with a shell tab open.
  it("throws RenewalUnavailableError, not RenewalFailedError, on 500", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(jsonResponse(500, { error: "internal", message: "boom" })),
    );

    await expect(renewSession()).rejects.toBeInstanceOf(RenewalUnavailableError);
    await expect(renewSession()).rejects.not.toBeInstanceOf(RenewalFailedError);
  });

  // 502/504 — an ingress or load balancer mid-rolling-deploy, or a
  // gateway timeout on a slow Zitadel call. Same bucket as 500: this
  // client must classify by "is this a 5xx" rather than an allow-list of
  // the one status renew.go happens to document.
  it.each([502, 504])(
    "throws RenewalUnavailableError, not RenewalFailedError, on %i",
    async (status) => {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(status, {})));

      await expect(renewSession()).rejects.toBeInstanceOf(RenewalUnavailableError);
      await expect(renewSession()).rejects.not.toBeInstanceOf(RenewalFailedError);
    },
  );

  // 408 (a request timeout) is also "try again", not "log out".
  it("throws RenewalUnavailableError, not RenewalFailedError, on 408", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(jsonResponse(408, { error: "timeout", message: "timed out" })),
    );

    await expect(renewSession()).rejects.toBeInstanceOf(RenewalUnavailableError);
    await expect(renewSession()).rejects.not.toBeInstanceOf(RenewalFailedError);
  });

  // The classification boundary the other direction: an unenumerated
  // 4xx (never seen in practice, but not a 429/408) must stay
  // RenewalFailedError, not silently widen into the retry bucket —
  // 4xx conventionally means "the request itself was wrong", which a
  // retry cannot fix.
  it("throws RenewalFailedError, not RenewalUnavailableError, on an unenumerated 4xx (400)", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(jsonResponse(400, { error: "invalid_request", message: "bad" })),
    );

    await expect(renewSession()).rejects.toBeInstanceOf(RenewalFailedError);
    await expect(renewSession()).rejects.not.toBeInstanceOf(RenewalUnavailableError);
  });

  // Also fix (Minor 5): a 200 OK whose BODY is not JSON at all (not just
  // a malformed renew_at field) must not throw — renew.go had already
  // set the Set-Cookie before writing the body, so the renewal itself
  // succeeded. This must resolve, falling back to renewAt undefined, not
  // reject as an uncaught SyntaxError that session-renewal.tsx would
  // otherwise treat as "log out" AFTER the session was already renewed.
  it("resolves with renewAt undefined when a 200 response body is not JSON at all", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(nonJsonResponse(200)));

    const result = await renewSession();

    expect(result.renewAt).toBeUndefined();
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

// #941: a retry after RenewalUnavailableError is bounded by what is left of
// the session, so one failed renewal cannot outlast a short SESSION_TTL.
describe("retryDelayMs", () => {
  const now = () => new Date("2026-10-08T12:00:00Z").getTime();

  it("keeps the fixed fallback when the expiry is unknown (the behaviour before #941)", () => {
    expect(retryDelayMs(undefined, now)).toBe(FALLBACK_RENEWAL_INTERVAL_MS);
  });

  it("keeps the fixed fallback on a long session, so retries are never faster than before", () => {
    const expiresAt = new Date("2026-10-08T12:30:00Z"); // 30 minutes left
    expect(retryDelayMs(expiresAt, now)).toBe(FALLBACK_RENEWAL_INTERVAL_MS);
  });

  // The defect itself: SESSION_TTL=3m, first renewal at t+60s fails with
  // 120s left. The fixed fallback would retry at t+360s — after expiry.
  it("retries within the session's remaining lifetime on a short session", () => {
    const expiresAt = new Date("2026-10-08T12:02:00Z"); // 120s left
    const delay = retryDelayMs(expiresAt, now);
    expect(delay).toBe(40_000);
    expect(delay).toBeLessThan(120_000);
  });

  it("never retries faster than MIN_RENEWAL_DELAY_MS, even with seconds left", () => {
    expect(retryDelayMs(new Date("2026-10-08T12:00:20Z"), now)).toBe(MIN_RENEWAL_DELAY_MS);
    expect(retryDelayMs(new Date("2026-10-08T11:59:00Z"), now)).toBe(MIN_RENEWAL_DELAY_MS);
  });
});

describe("renewSession expires_at", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("parses expires_at alongside renew_at", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse(200, {
          tenant_id: "t",
          renew_at: "2026-10-08T12:01:00Z",
          expires_at: "2026-10-08T12:03:00Z",
        }),
      ),
    );
    const result = await renewSession();
    expect(result.expiresAt?.toISOString()).toBe("2026-10-08T12:03:00.000Z");
  });

  it("resolves with expiresAt undefined when the field is missing or unparseable", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse(200, { tenant_id: "t", expires_at: "not-a-date" })),
    );
    expect((await renewSession()).expiresAt).toBeUndefined();
  });
});
