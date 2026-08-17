import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { PERMISSIONS_CACHE_KEY } from "@helivanta/api";
import { TenantPicker } from "./tenant-picker";

// Every network call the picker makes, in the order it made them, so a
// test can assert the *sequence* — the original bug was a switcher that
// stopped after step 1 and reloaded without waiting for the backend to
// actually confirm the switch.
const calls: string[] = [];

function stubFetch(currentTenantId = "t1", switchResult: "ok" | "not-member" = "ok") {
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
      if (switchResult === "not-member") {
        return { ok: false, status: 404, json: async () => ({ error: "not_found", message: "tenant not found" }) };
      }
      const body = init?.body ? (JSON.parse(init.body as string) as { tenant_id: string }) : { tenant_id: "" };
      return { ok: true, status: 200, json: async () => ({ tenant_id: body.tenant_id }) };
    }
    throw new Error(`unexpected fetch: ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

beforeEach(() => {
  calls.length = 0;
  window.localStorage.clear();
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

  it("re-mints the session: switch, then reload only once the backend confirms it", async () => {
    const fetchMock = stubFetch();
    const { user } = renderWithProviders(<TenantPicker />);
    await waitFor(() => expect(screen.getByRole("combobox")).toBeInTheDocument());

    await user.selectOptions(screen.getByRole("combobox"), "t2");

    await waitFor(() => expect(screen.getByText("Switched hospital")).toBeInTheDocument());

    expect(calls).toEqual(["GET /api/v1/iam/me/tenants", "POST /api/v1/iam/me/tenant"]);

    const switchCall = fetchMock.mock.calls.find(([url]) => url === "/api/v1/iam/me/tenant");
    expect(JSON.parse(switchCall![1]!.body as string)).toEqual({ tenant_id: "t2" });

    // Reload only after the backend actually confirmed the switch — a
    // switch that reloaded regardless of the response would leave the old
    // tenant's session cookie in place while showing the new tenant's UI.
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
    stubFetch("t2");
    const { user } = renderWithProviders(<TenantPicker />);

    await waitFor(() => expect(screen.getByRole("combobox")).toBeInTheDocument());

    // Simulates the post-switch, post-reload state: the server now
    // reports t2 as current, even though t1 sorts first in the array.
    expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("t2");

    // Switching back to t1 (the tenant the picker is NOT currently
    // showing) must fire a real change event and the switch mutation.
    await user.selectOptions(screen.getByRole("combobox"), "t1");

    await waitFor(() => expect(calls).toContain("POST /api/v1/iam/me/tenant"));
  });

  // THE mutation-discriminating test for "tenant picker sends a tenant
  // the user isn't in": the backend answers 404 (not 403 — see
  // tenant-picker.tsx's doc comment on why) for exactly this case. The
  // picker must surface it as an ordinary error toast, never crash, never
  // reload, and never leave the caller's actual session touched.
  it("reports failure and never reloads when the backend refuses the target tenant (404)", async () => {
    stubFetch("t1", "not-member");
    const { user } = renderWithProviders(<TenantPicker />);
    await waitFor(() => expect(screen.getByRole("combobox")).toBeInTheDocument());

    await user.selectOptions(screen.getByRole("combobox"), "t2");

    await waitFor(() => expect(screen.getByText("tenant not found")).toBeInTheDocument());
    expect(screen.queryByText("Switched hospital")).not.toBeInTheDocument();
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
    stubFetch();
    const { user } = renderWithProviders(<TenantPicker />);

    await waitFor(() => expect(screen.getByTitle("Hospital")).toBeInTheDocument());
    await user.selectOptions(screen.getByTitle("Hospital"), "t2");

    await waitFor(() => expect(window.localStorage.getItem(PERMISSIONS_CACHE_KEY)).toBeNull());
  });
});
