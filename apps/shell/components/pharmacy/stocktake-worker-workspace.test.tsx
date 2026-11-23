import { screen, waitFor } from "@testing-library/react";
import { useQueryClient } from "@tanstack/react-query";
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
      return Response.json(releaseSnapshotForTest(account, ["team_accounts", "pharmacy_setup"]));
    if (url.startsWith("/api/v1/iam/me/hospital-pharmacy-stocktake-scopes?"))
      return Response.json(page([]));
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

function RefreshRelease() {
  const client = useQueryClient();
  return (
    <button
      type="button"
      onClick={() => void client.invalidateQueries({ queryKey: ["release-capabilities"] })}
    >
      Refresh release availability
    </button>
  );
}

it.each(["open", "unconfirmed", "withdrawn"] as const)(
  "keeps released worker starts safe: %s",
  async (scenario) => {
    let releaseStatus = 200;
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT")
        return new Response(
          JSON.stringify({ error: "unavailable", message: "TEST start outcome unknown" }),
          { status: 503 },
        );
      const path = new URL(url, "https://stocktake.test").pathname;
      if (path === "/api/v1/iam/me/permissions")
        return Response.json({ data: [], tenant_id: account.tenantId, subject: account.subject });
      if (path === "/api/v1/workspace/release-capabilities") {
        if (releaseStatus !== 200)
          return new Response(JSON.stringify({ error: "unavailable" }), { status: releaseStatus });
        return Response.json(
          releaseSnapshotForTest(account, [
            "team_accounts",
            "pharmacy_setup",
            "pharmacy_stocktake",
          ]),
        );
      }
      if (path === "/api/v1/iam/me/hospital-pharmacy-stocktake-scopes")
        return Response.json(page([]));
      if (path === "/api/v1/iam/me/pharmacy-stocktake-locations")
        return Response.json(page([choice]));
      const root = `/api/v1/pharmacy/pharmacies/${choice.employer_id}/operating-locations/${choice.location_id}/stocktakes`;
      if (path === root) return Response.json(page([]));
      if (path === `${root}/quarantine-choices`)
        return Response.json(
          page([
            {
              id: "019a55c0-1111-7111-8111-111111111115",
              outlet_id: choice.location_id,
              code: "TEST-Q",
              name: "TEST Quarantine",
              stock_status: "quarantine",
              storage_address: { zone_code: "", aisle_code: "", rack_code: "", shelf_code: "" },
              created_at: "2026-11-20T00:00:00Z",
            },
          ]),
        );
      throw new Error("Unexpected TEST worker stocktake request");
    });
    vi.stubGlobal("fetch", fetch);
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <RefreshRelease />
        <StocktakeWorkerWorkspace />
      </NavigationGuardProvider>,
    );
    await user.click(
      await screen.findByRole("button", {
        name: "TEST Independent Pharmacy · TEST Main Outlet · TEST-MAIN · Outlet stocktake counter",
      }),
    );
    await user.click(await screen.findByRole("button", { name: "Start stocktake" }));
    expect(await screen.findByRole("textbox", { name: "Start reason" })).toBeEnabled();
    await user.click(screen.getByRole("combobox", { name: "Quarantine destination" }));
    expect(
      await screen.findByRole("option", { name: "TEST Quarantine · TEST-Q" }),
    ).toBeInTheDocument();
    if (scenario === "unconfirmed") {
      await user.click(screen.getByRole("option", { name: "TEST Quarantine · TEST-Q" }));
      await user.type(
        screen.getByRole("textbox", { name: "Start reason" }),
        "TEST interrupted monthly count",
      );
      await user.click(screen.getByRole("button", { name: "Freeze book and start count" }));
      expect(await screen.findByRole("button", { name: "Check started count" })).toBeVisible();
      expect(screen.getByRole("textbox", { name: "Start reason" })).toHaveValue(
        "TEST interrupted monthly count",
      );
    } else if (scenario === "withdrawn") {
      await user.keyboard("{Escape}");
      releaseStatus = 503;
      await user.click(screen.getByRole("button", { name: "Refresh release availability" }));
      await screen.findByText(
        "New stocktake work is not available. Retained status and recovery remain accessible.",
      );
      expect(screen.queryByRole("textbox", { name: "Start reason" })).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "Start stocktake" })).not.toBeInTheDocument();
    }
    expect(
      fetch.mock.calls.some(
        ([url]) =>
          url.includes("/outlets/") || url.includes("/catalogue/") || url.includes("/stock/"),
      ),
    ).toBe(false);
  },
);
