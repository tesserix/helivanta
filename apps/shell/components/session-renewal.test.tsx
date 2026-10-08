import { waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import {
  RenewalFailedError,
  RenewalUnavailableError,
  FALLBACK_RENEWAL_INTERVAL_MS,
} from "@/lib/renew";
import {
  PERMISSIONS_CACHE_KEY,
  loadExpiresAt,
  loadRenewAt,
  storeExpiresAt,
  storeRenewAt,
} from "@helivanta/api";
import { SessionRenewal } from "./session-renewal";

// setTimeout is spied on GLOBALLY, so other library internals (react-query,
// jsdom timers, etc.) calling it with unrelated delays would otherwise be
// mistaken for this component's own schedule if the test just took
// `mock.calls[0]`. Filtering for a delay this component actually uses is
// what makes these tests target THIS component's schedule specifically.
function findScheduledCall(spy: { mock: { calls: unknown[][] } }, delayMs: number) {
  return spy.mock.calls.find(([, delay]) => delay === delayMs) as [() => void, number] | undefined;
}

const renewSession = vi.hoisted(() => vi.fn());
vi.mock("@/lib/renew", async () => {
  const actual = await vi.importActual<typeof import("@/lib/renew")>("@/lib/renew");
  return { ...actual, renewSession };
});

const usePathname = vi.hoisted(() => vi.fn());
vi.mock("next/navigation", () => ({ usePathname }));

describe("SessionRenewal", () => {
  beforeEach(() => {
    renewSession.mockReset();
    usePathname.mockReturnValue("/");
    // The stored schedule is per-tab state that outlives a render, so it
    // must be cleared between tests or one test's login schedule seeds
    // the next test's first timer (#916 Task 4, F3).
    window.sessionStorage.clear();
    // Same reasoning for the permissions cache, which lives in
    // localStorage (packages/api/src/permissions-cache.ts) and is cleared
    // by the same teardown path asserted below.
    window.localStorage.clear();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("does not schedule a renewal on /login", async () => {
    usePathname.mockReturnValue("/login");
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");

    renderWithProviders(<SessionRenewal />);
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(findScheduledCall(setTimeoutSpy, FALLBACK_RENEWAL_INTERVAL_MS)).toBeUndefined();
    expect(renewSession).not.toHaveBeenCalled();
    setTimeoutSpy.mockRestore();
  });

  it("does not schedule a renewal on the auth callback route", async () => {
    usePathname.mockReturnValue("/api/auth/callback");
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");

    renderWithProviders(<SessionRenewal />);
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(findScheduledCall(setTimeoutSpy, FALLBACK_RENEWAL_INTERVAL_MS)).toBeUndefined();
    expect(renewSession).not.toHaveBeenCalled();
    setTimeoutSpy.mockRestore();
  });

  // Renamed for accuracy by #916 Task 4 (F3): the first renewal is no
  // longer UNCONDITIONALLY at the fallback interval — it uses the
  // schedule POST /v1/auth/login stored when one exists (see the
  // "SessionRenewal's FIRST renewal" block below). This case is the
  // no-stored-schedule one, which beforeEach's sessionStorage.clear()
  // establishes.
  it("schedules the first renewal at the bounded fallback interval when nothing was stored", async () => {
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    renewSession.mockResolvedValue({ renewAt: undefined });

    renderWithProviders(<SessionRenewal />);
    await waitFor(() =>
      expect(findScheduledCall(setTimeoutSpy, FALLBACK_RENEWAL_INTERVAL_MS)).toBeDefined(),
    );

    const [callback] = findScheduledCall(setTimeoutSpy, FALLBACK_RENEWAL_INTERVAL_MS)!;
    callback();
    await waitFor(() => expect(renewSession).toHaveBeenCalledTimes(1));

    setTimeoutSpy.mockRestore();
  });

  // Spec D5: the NEXT renewal is scheduled from the server's renew_at
  // hint, not a client-invented constant.
  it("schedules the next renewal from the server's renew_at hint", async () => {
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    const fiveMinutesMs = 5 * 60 * 1000;
    renewSession.mockResolvedValue({ renewAt: new Date(Date.now() + fiveMinutesMs) });

    renderWithProviders(<SessionRenewal />);
    const [firstCallback] = await waitFor(() => {
      const call = findScheduledCall(setTimeoutSpy, FALLBACK_RENEWAL_INTERVAL_MS);
      expect(call).toBeDefined();
      return call!;
    });

    setTimeoutSpy.mockClear();
    firstCallback();
    await waitFor(() => expect(renewSession).toHaveBeenCalledTimes(1));

    await waitFor(() => {
      const rescheduled = setTimeoutSpy.mock.calls.find(
        ([, delay]) => typeof delay === "number" && Math.abs(delay - fiveMinutesMs) < 1000,
      );
      expect(rescheduled).toBeDefined();
    });

    setTimeoutSpy.mockRestore();
  });

  // The genuine "session is over" case: a RenewalFailedError (401/404)
  // sends the user to a visible re-auth.
  it("redirects to /login when renewal fails with RenewalFailedError", async () => {
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    renewSession.mockRejectedValue(new RenewalFailedError("account is no longer active"));
    const locationSpy = vi.fn();
    vi.stubGlobal("location", {
      set href(v: string) {
        locationSpy(v);
      },
    });

    renderWithProviders(<SessionRenewal />);
    const [callback] = await waitFor(() => {
      const call = findScheduledCall(setTimeoutSpy, FALLBACK_RENEWAL_INTERVAL_MS);
      expect(call).toBeDefined();
      return call!;
    });
    callback();

    await waitFor(() => expect(locationSpy).toHaveBeenCalledWith("/login"));
    setTimeoutSpy.mockRestore();
  });

  // #781's rule — "a finished session must not be reconstructable from
  // anything the browser kept" — applies to THIS teardown path too, not
  // only to HmsShell's sign-out and idle-timeout ones. It did not hold
  // here: the RenewalFailedError branch navigated to /login having
  // cleared nothing, so the previous clinician's cached permission set and
  // renewal schedule survived into whoever signed in next on a shared ward
  // terminal — and packages/api/src/renew-schedule.ts's clearRenewAt
  // comment claimed the property held "with no exceptions". Asserted here
  // rather than trusted to the comment.
  it("clears the browser-held session state before sending the user to /login", async () => {
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    renewSession.mockRejectedValue(new RenewalFailedError("account is no longer active"));
    vi.stubGlobal("location", { set href(_v: string) {} });
    storeRenewAt(new Date(Date.now() + 60_000).toISOString());
    window.localStorage.setItem(PERMISSIONS_CACHE_KEY, JSON.stringify(["lab.order.read"]));
    expect(loadRenewAt(), "precondition: a schedule is stored").toBeDefined();

    renderWithProviders(<SessionRenewal />);
    // The stored schedule above means the first timer is ~60s, not the
    // 5-minute fallback the other failure test matches on.
    const [callback] = await waitFor(() => {
      const call = setTimeoutSpy.mock.calls.find(
        ([, delay]) => typeof delay === "number" && delay > 50_000 && delay <= 60_000,
      ) as [() => void, number] | undefined;
      expect(call).toBeDefined();
      return call!;
    });
    callback();

    await waitFor(() => {
      expect(
        loadRenewAt(),
        "the renewal schedule must not survive a session the backend has ended",
      ).toBeUndefined();
      expect(
        window.localStorage.getItem(PERMISSIONS_CACHE_KEY),
        "the cached permission set must not survive into the next user on this browser",
      ).toBeNull();
    });
    setTimeoutSpy.mockRestore();
  });

  // The judgement call this task turns on: a RenewalUnavailableError
  // (503 fail-closed on a Zitadel/OpenFGA outage, or 429 rate-limited)
  // must NOT log the user out — it must retry instead. Mass-evicting
  // every clinician on an availability blip is the exact harm #916
  // exists to prevent, reproduced from a different cause.
  it("does NOT redirect to /login when renewal fails with RenewalUnavailableError, and retries instead", async () => {
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    renewSession.mockRejectedValue(
      new RenewalUnavailableError("renewal temporarily unavailable (status 503)"),
    );
    const locationSpy = vi.fn();
    vi.stubGlobal("location", {
      set href(v: string) {
        locationSpy(v);
      },
    });

    renderWithProviders(<SessionRenewal />);
    const [callback] = await waitFor(() => {
      const call = findScheduledCall(setTimeoutSpy, FALLBACK_RENEWAL_INTERVAL_MS);
      expect(call).toBeDefined();
      return call!;
    });
    setTimeoutSpy.mockClear();
    callback();

    await waitFor(() => expect(renewSession).toHaveBeenCalledTimes(1));
    // Gave the rejection's microtask a turn to run before asserting the
    // negative — otherwise this would trivially pass before the .catch
    // handler has even executed.
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(locationSpy).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(findScheduledCall(setTimeoutSpy, FALLBACK_RENEWAL_INTERVAL_MS)).toBeDefined(),
    );

    setTimeoutSpy.mockRestore();
  });

  // #941. SESSION_TTL=3m: the session has 120s left when the renewal fails.
  // The fixed five-minute retry would fire after expiry, so one failed
  // renewal signed the clinician out. The retry must land inside the
  // remaining lifetime (a third of it: 40s), from the expiry the login or
  // last renewal stored.
  it("retries a failed renewal within the session's remaining lifetime", async () => {
    storeExpiresAt(new Date(Date.now() + 120_000));
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    renewSession.mockRejectedValue(
      new RenewalUnavailableError("renewal temporarily unavailable (status 503)"),
    );

    renderWithProviders(<SessionRenewal />);
    const [callback] = await waitFor(() => {
      const call = findScheduledCall(setTimeoutSpy, FALLBACK_RENEWAL_INTERVAL_MS);
      expect(call).toBeDefined();
      return call!;
    });
    setTimeoutSpy.mockClear();
    callback();
    await waitFor(() => expect(renewSession).toHaveBeenCalledTimes(1));

    await waitFor(() => {
      const retry = setTimeoutSpy.mock.calls.find(
        ([, delay]) => typeof delay === "number" && delay >= 30_000 && delay <= 40_000,
      );
      expect(retry).toBeDefined();
    });
    expect(findScheduledCall(setTimeoutSpy, FALLBACK_RENEWAL_INTERVAL_MS)).toBeUndefined();
    setTimeoutSpy.mockRestore();
  });

  it("stores the expiry a successful renewal answers with", async () => {
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    renewSession.mockResolvedValue({
      renewAt: new Date(Date.now() + 60_000),
      expiresAt: new Date("2030-01-01T00:03:00.000Z"),
    });

    renderWithProviders(<SessionRenewal />);
    const [callback] = await waitFor(() => {
      const call = findScheduledCall(setTimeoutSpy, FALLBACK_RENEWAL_INTERVAL_MS);
      expect(call).toBeDefined();
      return call!;
    });
    callback();

    await waitFor(() => expect(loadExpiresAt()?.toISOString()).toBe("2030-01-01T00:03:00.000Z"));
    setTimeoutSpy.mockRestore();
  });
});

// --- #916 Task 4, F3: the first renewal obeys the server too -------------

describe("SessionRenewal's FIRST renewal", () => {
  beforeEach(() => {
    renewSession.mockReset();
    usePathname.mockReturnValue("/");
    window.sessionStorage.clear();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // The regression this whole F3 change exists for. Before it, the first
  // timer was always FALLBACK_RENEWAL_INTERVAL_MS (5 minutes) regardless
  // of SESSION_TTL, so any deployment with a shorter TTL lost every
  // session before its first renewal — with nothing anywhere reporting
  // it. The server's boot-time minimum (config.RequireSessionTTL, #921)
  // is 90s, so TTLs under five minutes remain valid configurations.
  //
  // 90s here is not arbitrary: it is one third of a 4m30s SESSION_TTL,
  // i.e. exactly what renewAtFor answers for a TTL well under the old
  // hardcoded fallback. If this line regressed to
  // nextRenewalDelayMs(undefined), the assertion below would find a
  // 300_000ms timer and no 90_000ms one.
  it("is scheduled from the schedule login stored, not from the 5-minute fallback", async () => {
    const ninetySeconds = 90_000;
    storeRenewAt(new Date(Date.now() + ninetySeconds).toISOString());
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");

    renderWithProviders(<SessionRenewal />);
    await new Promise((resolve) => setTimeout(resolve, 0));

    const scheduled = setTimeoutSpy.mock.calls.find(
      ([, delay]) => typeof delay === "number" && delay > 80_000 && delay <= ninetySeconds,
    );
    expect(
      scheduled,
      "the first renewal must be scheduled from the server's renew_at, not the 5-minute fallback",
    ).toBeDefined();
    expect(
      findScheduledCall(setTimeoutSpy, FALLBACK_RENEWAL_INTERVAL_MS),
      "the fallback interval must NOT be used when the server gave a schedule",
    ).toBeUndefined();
    setTimeoutSpy.mockRestore();
  });

  // The other direction, and the reason FALLBACK_RENEWAL_INTERVAL_MS is
  // kept rather than deleted: with nothing stored (a browser that blocks
  // sessionStorage, a tab opened straight onto a deep link with a session
  // cookie already present), behaviour is byte-for-byte what it was
  // before F3 — not an error, and not a zero-delay retry.
  it("falls back to the bounded interval when no schedule was stored", async () => {
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");

    renderWithProviders(<SessionRenewal />);
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(findScheduledCall(setTimeoutSpy, FALLBACK_RENEWAL_INTERVAL_MS)).toBeDefined();
    setTimeoutSpy.mockRestore();
  });

  // A reload mid-session must resume from the LATEST server answer, not
  // from the one login left behind — so every successful renewal
  // rewrites the stored schedule.
  it("rewrites the stored schedule after each successful renewal", async () => {
    storeRenewAt(new Date(Date.now() + 60_000).toISOString());
    const nextRenewAt = new Date(Date.now() + 12 * 60_000);
    renewSession.mockResolvedValue({ renewAt: nextRenewAt });
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");

    renderWithProviders(<SessionRenewal />);
    await new Promise((resolve) => setTimeout(resolve, 0));

    const first = setTimeoutSpy.mock.calls.find(
      ([, delay]) => typeof delay === "number" && delay > 50_000 && delay <= 60_000,
    );
    expect(
      first,
      "precondition: the first renewal was scheduled from the stored value",
    ).toBeDefined();
    first![0]();

    await waitFor(() => {
      expect(loadRenewAt()?.toISOString()).toBe(nextRenewAt.toISOString());
    });
    setTimeoutSpy.mockRestore();
  });
});
