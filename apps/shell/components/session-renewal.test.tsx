import { waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import {
  RenewalFailedError,
  RenewalUnavailableError,
  FALLBACK_RENEWAL_INTERVAL_MS,
} from "@/lib/renew";
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

  it("schedules the first renewal at the bounded fallback interval", async () => {
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
});
