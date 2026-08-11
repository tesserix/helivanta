import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@hms/api/testing";
import { PERMISSIONS_CACHE_KEY } from "@hms/api";
import { TenantPicker } from "./tenant-picker";

const signInWithCustomToken = vi.hoisted(() => vi.fn());
vi.mock("firebase/auth", () => ({ signInWithCustomToken }));
vi.mock("@/lib/firebase", () => ({ firebaseAuth: () => ({ name: "test-auth" }) }));

// Every network call the picker makes, in the order it made them, so a
// test can assert the *sequence* — the original bug was a switcher that
// stopped after step 1 and reloaded, so "the session POST happened" is
// only meaningful together with "after the custom token came back".
const calls: string[] = [];

function stubFetch(
  switchBody: unknown = { tenant_id: "t2", custom_token: "custom-abc" },
  currentTenantId = "t1",
) {
  const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    calls.push(`${init?.method ?? "GET"} ${url}`);
    if (url.endsWith("/iam/me/tenants")) {
      return {
        ok: true,
        status: 200,
        json: async () => ({
          data: [
            { tenant_id: "t1", roles: ["doctor"], current: currentTenantId === "t1" },
            { tenant_id: "t2", roles: ["nurse"], current: currentTenantId === "t2" },
          ],
        }),
      };
    }
    if (url.endsWith("/iam/me/tenant")) {
      return { ok: true, status: 200, json: async () => switchBody };
    }
    if (url === "/api/session") {
      return { ok: true, status: 200, json: async () => ({ ok: true }) };
    }
    throw new Error(`unexpected fetch: ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

// mockCred builds the signInWithCustomToken resolution: an
// getIdTokenResult() carrying whatever tenant_id claim the test wants to
// simulate, matching the real firebase.User shape closely enough for
// tenant-picker.tsx to read `.claims.tenant_id` and `.token` off it.
function mockCred(claimTenantId: string) {
  return {
    user: {
      getIdToken: async () => "fresh-id-token",
      getIdTokenResult: async () => ({
        token: "fresh-id-token",
        claims: { tenant_id: claimTenantId },
      }),
    },
  };
}

beforeEach(() => {
  calls.length = 0;
  signInWithCustomToken.mockReset();
  signInWithCustomToken.mockImplementation(async () => {
    calls.push("signInWithCustomToken");
    // Default: the minted token carries the tenant the picker asked to
    // switch to. Individual tests override this to simulate GIP handing
    // back a token for the wrong tenant.
    return mockCred("t2");
  });
  vi.stubGlobal("location", { reload: vi.fn() });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("TenantPicker", () => {
  it("renders nothing for a single-tenant user", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ data: [{ tenant_id: "t1", roles: ["doctor"] }] }),
      }),
    );
    renderWithProviders(<TenantPicker />);

    // renderWithProviders always mounts a sonner <Toaster/> alongside the
    // component under test, so the render container is never fully empty —
    // assert on the picker's own markup instead.
    await waitFor(() => expect(screen.queryByText("Hospital")).not.toBeInTheDocument());
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  it("lists every tenant for a multi-hospital user", async () => {
    stubFetch();
    renderWithProviders(<TenantPicker />);

    await waitFor(() => expect(screen.getByRole("combobox")).toBeInTheDocument());
    expect(screen.getAllByRole("option")).toHaveLength(2);
  });

  it("re-mints the session: switch, sign in with the custom token, then replace the cookie", async () => {
    const fetchMock = stubFetch();
    const { user } = renderWithProviders(<TenantPicker />);
    await waitFor(() => expect(screen.getByRole("combobox")).toBeInTheDocument());

    await user.selectOptions(screen.getByRole("combobox"), "t2");

    await waitFor(() => expect(screen.getByText("Switched hospital")).toBeInTheDocument());

    // The whole point: a switch is not done when the backend says yes.
    // Without the middle two steps the user keeps their old tenant_id
    // claim and silently stays in the hospital they started in.
    expect(calls).toEqual([
      "GET /api/v1/iam/me/tenants",
      "POST /api/v1/iam/me/tenant",
      "signInWithCustomToken",
      "POST /api/session",
    ]);

    expect(signInWithCustomToken).toHaveBeenCalledWith(expect.anything(), "custom-abc");

    const sessionCall = fetchMock.mock.calls.find(([url]) => url === "/api/session");
    expect(JSON.parse(sessionCall![1]!.body as string)).toEqual({ idToken: "fresh-id-token" });

    // Reload last, and only after the cookie was replaced — reloading
    // first would just re-render the old tenant's data.
    await waitFor(() => expect(window.location.reload).toHaveBeenCalled());
  });

  // Regresses the one-way-door bug: the picker used to derive "current"
  // from array order (`defaultValue={memberships[0].tenant_id}`), which
  // is computed once at first mount and never changes. After switching
  // from t1 to t2 and reloading, the sorted array order is unchanged, so
  // the old picker kept showing t1 selected even though the caller was
  // now in t2 — and since the DOM already visually matched t1, selecting
  // t1 again fired no change event, so returning to it was impossible
  // without a fresh login. The backend now marks the caller's actual
  // tenant `current: true` (derived from the token claim, not array
  // position), and the picker must be a controlled component driven by
  // that flag.
  it("reflects the caller's actual current tenant after a switch, and allows switching back", async () => {
    stubFetch(undefined, "t2");
    const { user } = renderWithProviders(<TenantPicker />);

    await waitFor(() => expect(screen.getByRole("combobox")).toBeInTheDocument());

    // Simulates the post-switch, post-reload state: the server now
    // reports t2 as current, even though t1 sorts first in the array.
    expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("t2");

    // Switching back to t1 (the tenant the picker is NOT currently
    // showing) must fire a real change event and the switch mutation.
    await user.selectOptions(screen.getByRole("combobox"), "t1");

    await waitFor(() => expect(calls).toContain("POST /api/v1/iam/me/tenant"));
    const switchCall = calls.find((c) => c.includes("/iam/me/tenant"));
    expect(switchCall).toBeDefined();
  });

  // The disclosed production-only risk this control cannot rule out in
  // this environment: real GIP might let a persisted
  // `customAttributes.tenant_id` on the account win over the custom
  // token's developer claim, silently minting a token for the OLD
  // tenant. This test cannot reproduce GIP's actual precedence behavior,
  // but it proves the client-side guard that makes a mismatch loud
  // instead of silent — the exact regression of the bug this control was
  // built to fix, just one layer down.
  it("reports failure and never POSTs the session when the minted token's tenant claim does not match the target", async () => {
    stubFetch();
    // Simulate GIP handing back a token still carrying t1 (the tenant
    // being switched FROM) even though the picker asked to switch to t2.
    signInWithCustomToken.mockImplementation(async () => {
      calls.push("signInWithCustomToken");
      return mockCred("t1");
    });
    const { user } = renderWithProviders(<TenantPicker />);
    await waitFor(() => expect(screen.getByRole("combobox")).toBeInTheDocument());

    await user.selectOptions(screen.getByRole("combobox"), "t2");

    await waitFor(() =>
      expect(screen.getByText(/did not carry the expected hospital/)).toBeInTheDocument(),
    );
    expect(screen.queryByText("Switched hospital")).not.toBeInTheDocument();
    expect(calls).not.toContain("POST /api/session");
    expect(window.location.reload).not.toHaveBeenCalled();
  });

  it("keeps the old session and does not reload when the exchange fails", async () => {
    stubFetch();
    signInWithCustomToken.mockRejectedValue(new Error("custom token rejected"));
    const { user } = renderWithProviders(<TenantPicker />);
    await waitFor(() => expect(screen.getByRole("combobox")).toBeInTheDocument());

    await user.selectOptions(screen.getByRole("combobox"), "t2");

    await waitFor(() => expect(screen.getByText("custom token rejected")).toBeInTheDocument());
    expect(calls).not.toContain("POST /api/session");
    expect(window.location.reload).not.toHaveBeenCalled();
  });

  // A tenant switch changes the whole permission set, so the cached one
  // must not survive the reload that follows.
  it("clears the cached permissions when switching tenants", async () => {
    window.localStorage.setItem(
      PERMISSIONS_CACHE_KEY,
      JSON.stringify({
        subject: "doc",
        tenantId: "t1",
        permissions: ["lab.order.read"],
        storedAt: Date.now(),
      }),
    );
    signInWithCustomToken.mockResolvedValue({
      user: { getIdTokenResult: async () => ({ claims: { tenant_id: "t2" }, token: "id-token" }) },
    });
    stubFetch();
    const { user } = renderWithProviders(<TenantPicker />);

    await waitFor(() => expect(screen.getByTitle("Hospital")).toBeInTheDocument());
    await user.selectOptions(screen.getByTitle("Hospital"), "t2");

    await waitFor(() => expect(window.localStorage.getItem(PERMISSIONS_CACHE_KEY)).toBeNull());
  });
});
