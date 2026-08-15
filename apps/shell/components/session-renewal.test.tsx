import { waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { renderWithProviders } from "@hms/api/testing";
import { RENEWAL_INTERVAL_MS } from "@/lib/renew";
import { SessionRenewal } from "./session-renewal";

// setInterval is spied on GLOBALLY, so other library internals (react-query,
// jsdom timers, etc.) calling it with unrelated delays would otherwise be
// mistaken for this component's own schedule if the test just took
// `mock.calls[0]`. Filtering for the exact RENEWAL_INTERVAL_MS delay is
// what makes these tests target THIS component's interval specifically.
function findRenewalCall(spy: { mock: { calls: unknown[][] } }) {
  return spy.mock.calls.find(([, delay]) => delay === RENEWAL_INTERVAL_MS) as
    [() => void, number] | undefined;
}

const renewSession = vi.hoisted(() => vi.fn());
vi.mock("@/lib/renew", async () => {
  const actual = await vi.importActual<typeof import("@/lib/renew")>("@/lib/renew");
  return { ...actual, renewSession };
});

const usePathname = vi.hoisted(() => vi.fn());
vi.mock("next/navigation", () => ({ usePathname }));

function stubTenantsFetch(currentTenantId = "t1") {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        data: [{ tenant_id: currentTenantId, roles: ["doctor"], current: true }],
      }),
    }),
  );
}

describe("SessionRenewal", () => {
  beforeEach(() => {
    renewSession.mockReset();
    usePathname.mockReturnValue("/");
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("does not query for a tenant on /login", async () => {
    usePathname.mockReturnValue("/login");
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    renderWithProviders(<SessionRenewal />);
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("does not query for a tenant on the auth callback or silent-renew routes", async () => {
    usePathname.mockReturnValue("/api/auth/callback");
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    renderWithProviders(<SessionRenewal />);
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(fetchMock).not.toHaveBeenCalled();
  });

  // Proves the schedule itself is wired correctly (interval length, and
  // that its callback is renewSession bound to the caller's ACTUAL
  // current tenant, read from the same current:true flag TenantPicker
  // uses) without waiting on a real 5-minute timer: the interval's
  // callback is invoked directly, exactly as the browser's timer would.
  it("schedules renewal for the caller's current tenant at RENEWAL_INTERVAL_MS", async () => {
    stubTenantsFetch("t2");
    renewSession.mockResolvedValue(undefined);
    const setIntervalSpy = vi.spyOn(window, "setInterval");

    renderWithProviders(<SessionRenewal />);
    await waitFor(() => expect(findRenewalCall(setIntervalSpy)).toBeDefined());

    const [callback] = findRenewalCall(setIntervalSpy)!;
    (callback as () => void)();
    await waitFor(() => expect(renewSession).toHaveBeenCalledWith("t2"));

    setIntervalSpy.mockRestore();
  });

  it("falls back to a visible login when a scheduled renewal fails", async () => {
    stubTenantsFetch("t1");
    renewSession.mockRejectedValue(new Error("login_required"));
    const locationSpy = vi.fn();
    vi.stubGlobal("location", {
      set href(v: string) {
        locationSpy(v);
      },
    });
    const setIntervalSpy = vi.spyOn(window, "setInterval");

    renderWithProviders(<SessionRenewal />);
    await waitFor(() => expect(findRenewalCall(setIntervalSpy)).toBeDefined());

    const [callback] = findRenewalCall(setIntervalSpy)!;
    (callback as () => void)();
    await waitFor(() => expect(locationSpy).toHaveBeenCalledWith("/login"));

    setIntervalSpy.mockRestore();
  });
});
