import { act, screen, waitFor } from "@testing-library/react";
import { useQueryClient } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import {
  beginTenantTransition,
  cancelTenantTransition,
  stocktakeWorkerLocationsKey,
} from "@helivanta/api";
import { renderWithProviders, releaseSnapshotForTest } from "@helivanta/api/testing";
import Dashboard from "@/app/(workspace)/dashboard/page";

const scope = { tenantId: "018f8f5b-1111-7111-8111-111111111111", subject: "TEST-worker" };
const choice = {
  owner_tenant_id: scope.tenantId,
  duty_id: "019a55c0-1111-7111-8111-111111111112",
  employer_id: "019a55c0-1111-7111-8111-111111111113",
  location_id: "019a55c0-1111-7111-8111-111111111114",
  employer_name: "TEST Private Pharmacy",
  location_name: "TEST Private Outlet",
  location_code: "TEST-MAIN",
  time_zone: "Asia/Kolkata",
  country_code: "IN",
  template_key: "pharmacy.stocktake_counter.v1",
  template_label: "Outlet stocktake counter",
  assigned_permissions: ["pharmacy.stocktake.read", "pharmacy.stocktake.count"],
  snapshot_version: 2,
  authority_epoch: 1,
  expires_at: "2030-01-01T00:00:00Z",
};
const page = (rows: unknown[], cursor: string | null = null) => ({
  data: rows,
  page: { has_more: cursor !== null, next_cursor: cursor },
});
function RefreshWork() {
  const client = useQueryClient();
  return (
    <button
      type="button"
      onClick={() =>
        void client.invalidateQueries({ queryKey: [...stocktakeWorkerLocationsKey(scope)] })
      }
    >
      Refresh test work
    </button>
  );
}
afterEach(() => {
  act(() => cancelTenantTransition());
  vi.unstubAllGlobals();
  localStorage.clear();
});
function serve(
  rows: unknown[],
  cursor: string | null = null,
  hospitalCursor: string | null = null,
) {
  let unavailable = false;
  const fetch = vi.fn(async (url: string) => {
    if (url === "/api/v1/iam/me/permissions")
      return Response.json({ data: [], tenant_id: scope.tenantId, subject: scope.subject });
    if (url === "/api/v1/workspace/release-capabilities")
      return Response.json(releaseSnapshotForTest(scope, ["team_accounts", "pharmacy_setup"]));
    if (url === "/api/v1/iam/me/hospital-pharmacy-stocktake-scopes?limit=20")
      return Response.json(page([], hospitalCursor));
    if (url === "/api/v1/iam/me/pharmacy-stocktake-locations?limit=20")
      return unavailable
        ? new Response(JSON.stringify({ error: "unavailable" }), { status: 503 })
        : Response.json(page(rows, cursor));
    throw new Error("Unexpected TEST personal work request");
  });
  vi.stubGlobal("fetch", fetch);
  return {
    fetch,
    refuse: () => {
      unavailable = true;
    },
  };
}

it.each([
  { name: "named current duty", rows: [choice], cursor: null },
  { name: "empty filtered page with continuation", rows: [], cursor: "TEST-next" },
])(
  "offers baseline worker navigation for $name without management permissions or new-work release",
  async ({ rows, cursor }) => {
    const { fetch } = serve(rows, cursor);
    renderWithProviders(<Dashboard />);
    expect(await screen.findByRole("link", { name: /^My stocktakes/ })).toHaveAttribute(
      "href",
      "/pharmacy/working-stocktakes",
    );
    const requests = fetch.mock.calls.filter(([url]) =>
      url.includes("pharmacy-stocktake-locations"),
    );
    expect(requests).toHaveLength(1);
    expect(document.body.textContent).not.toContain("TEST Private Pharmacy");
    expect(document.body.textContent).not.toContain(choice.duty_id);
    expect(
      fetch.mock.calls.some(
        ([url]) =>
          url.includes("/outlets/") ||
          url.includes("operating-authority-scopes") ||
          url.includes("/stock/"),
      ),
    ).toBe(false);
  },
);

it("does not advertise stocktake work to an account with no current duty", async () => {
  const { fetch } = serve([]);
  renderWithProviders(<Dashboard />);
  await waitFor(() =>
    expect(fetch.mock.calls.some(([url]) => url.includes("pharmacy-stocktake-locations"))).toBe(
      true,
    ),
  );
  expect(screen.queryByRole("link", { name: /^My stocktakes/ })).not.toBeInTheDocument();
});

it("removes personal work during a tenant transition or failed directory refresh", async () => {
  const server = serve([choice]);
  const { user } = renderWithProviders(
    <>
      <Dashboard />
      <RefreshWork />
    </>,
  );
  await screen.findByRole("link", { name: /^My stocktakes/ });
  act(() => beginTenantTransition());
  expect(screen.queryByRole("link", { name: /^My stocktakes/ })).not.toBeInTheDocument();
  act(() => cancelTenantTransition());
  await screen.findByRole("link", { name: /^My stocktakes/ });
  server.refuse();
  await user.click(screen.getByRole("button", { name: "Refresh test work" }));
  await waitFor(() =>
    expect(screen.queryByRole("link", { name: /^My stocktakes/ })).not.toBeInTheDocument(),
  );
});

it("offers hospital worker navigation when an empty filtered assignment page has continuation", async () => {
  const { fetch } = serve([], null, "TEST-hospital-next");
  renderWithProviders(<Dashboard />);
  expect(await screen.findByRole("link", { name: /^My stocktakes/ })).toHaveAttribute(
    "href",
    "/pharmacy/working-stocktakes",
  );
  expect(fetch.mock.calls.some(([url]) => url.includes("hospital-pharmacy-stocktake-scopes"))).toBe(
    true,
  );
  expect(fetch.mock.calls.some(([url]) => url.includes("stocktake-outlet-choices"))).toBe(false);
});
