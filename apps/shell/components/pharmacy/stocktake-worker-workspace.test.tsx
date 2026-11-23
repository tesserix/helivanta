import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders, releaseSnapshotForTest } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { StocktakeWorkerWorkspace } from "./stocktake-worker-workspace";

const account = { tenantId: "018f8f5b-1111-7111-8111-111111111111", subject: "TEST-worker" };
const choice = {
  owner_tenant_id: account.tenantId,
  duty_id: "019a55c0-1111-7111-8111-111111111112",
  employer_id: "019a55c0-1111-7111-8111-111111111113",
  location_id: "019a55c0-1111-7111-8111-111111111114",
  employer_name: "TEST Independent Pharmacy",
  location_name: "TEST Main Outlet",
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
const page = (data: unknown[], next: string | null = null) => ({
  data,
  page: { has_more: next !== null, next_cursor: next },
});
afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
  localStorage.clear();
});

it("finds named reviewed work through an empty candidate page and retains recovery when new work is disabled", async () => {
  const fetch = vi.fn(async (url: string) => {
    if (url === "/api/v1/iam/me/permissions")
      return Response.json({ data: [], tenant_id: account.tenantId, subject: account.subject });
    if (url === "/api/v1/workspace/release-capabilities")
      return Response.json(releaseSnapshotForTest(account, ["pharmacy_setup"]));
    if (url.startsWith("/api/v1/iam/me/pharmacy-stocktake-locations?")) {
      return Response.json(url.includes("cursor=") ? page([choice]) : page([], "TEST-next"));
    }
    if (
      new URL(url, "https://stocktake.test").pathname ===
      `/api/v1/pharmacy/pharmacies/${choice.employer_id}/operating-locations/${choice.location_id}/stocktakes`
    )
      return Response.json(page([]));
    throw new Error("Unexpected TEST worker stocktake request");
  });
  vi.stubGlobal("fetch", fetch);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <StocktakeWorkerWorkspace />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Load more working locations" }));
  await user.click(
    await screen.findByRole("button", {
      name: "TEST Independent Pharmacy · TEST Main Outlet · TEST-MAIN · Outlet stocktake counter",
    }),
  );
  await screen.findByText(
    "New stocktake work is not available. Retained status and recovery remain accessible.",
  );
  expect(screen.queryByRole("button", { name: "Start stocktake" })).not.toBeInTheDocument();
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  expect(document.body.textContent).not.toContain(choice.employer_id);
  expect(document.body.textContent).not.toContain(choice.location_id);
  await waitFor(() =>
    expect(
      fetch.mock.calls.some(
        ([url]) =>
          new URL(url, "https://stocktake.test").pathname ===
          `/api/v1/pharmacy/pharmacies/${choice.employer_id}/operating-locations/${choice.location_id}/stocktakes`,
      ),
    ).toBe(true),
  );
  expect(
    fetch.mock.calls.some(
      ([url]) =>
        url.includes("/outlets/") || url.includes("/catalogue/") || url.includes("/stock/"),
    ),
  ).toBe(false);
});
