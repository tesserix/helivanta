import { useQueryClient } from "@tanstack/react-query";
import { staffAccessTemplates } from "@helivanta/api";
import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders, releaseSnapshotForTest } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { StocktakeWorkerWorkspace } from "./stocktake-worker-workspace";

const account = {
  tenantId: "018f8f5b-1111-7111-8111-111111111111",
  subject: "TEST-hospital-worker",
};
const assignment = {
  owner_tenant_id: account.tenantId,
  id: "019a55c0-1111-7111-8111-111111111113",
  hospital_id: "22222222-2222-4222-8222-222222222222",
  department_id: "019a55c0-1111-7111-8111-111111111112",
  hospital_name: "TEST Hospital",
  department_name: "TEST Pharmacy department",
  template_key: "pharmacy.department_stocktake_counter.v1",
  template_label: "Hospital pharmacy stocktake counter",
  assigned_permissions: [
    "pharmacy.stocktake.read",
    "pharmacy.stocktake.count",
    "workforce.stocktake_actor.use",
  ],
  snapshot_version: 1,
  authority_epoch: 1,
  expires_at: "2030-01-01T00:00:00Z",
};
const outlet = {
  owner_tenant_id: account.tenantId,
  hospital_id: assignment.hospital_id,
  department_id: assignment.department_id,
  department_revision_id: "019a55c0-1111-7111-8111-111111111114",
  pharmacy_id: "019a55c0-1111-7111-8111-111111111115",
  outlet_id: "019a55c0-1111-7111-8111-111111111116",
  binding_id: "019a55c0-1111-7111-8111-111111111117",
  binding_version: 2,
  pharmacy_name: "TEST Hospital pharmacy",
  outlet_name: "TEST Main outlet",
  outlet_code: "TEST-MAIN",
  country_code: "IN",
  time_zone: "Asia/Kolkata",
};
const page = (data: unknown[], next: string | null = null) => ({
  data,
  page: { has_more: next !== null, next_cursor: next },
});
const root = `/api/v1/pharmacy/hospitals/${assignment.hospital_id}/departments/${assignment.department_id}/pharmacies/${outlet.pharmacy_id}/outlets/${outlet.outlet_id}/stocktakes`;
afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
  localStorage.clear();
});
function render() {
  return renderWithProviders(
    <NavigationGuardProvider>
      <StocktakeWorkerWorkspace />
    </NavigationGuardProvider>,
  );
}

it("chooses a named hospital role and approved outlet through filtered pages with isolated retained recovery", async () => {
  let withdrawn = false;
  const fetch = vi.fn(async (url: string) => {
    const path = new URL(url, "https://stocktake.test").pathname;
    if (path === "/api/v1/iam/me/permissions")
      return Response.json({ data: [], tenant_id: account.tenantId, subject: account.subject });
    if (path === "/api/v1/workspace/release-capabilities")
      return Response.json(releaseSnapshotForTest(account, ["team_accounts", "pharmacy_setup"]));
    if (path === "/api/v1/iam/me/pharmacy-stocktake-locations") return Response.json(page([]));
    if (path === "/api/v1/iam/me/hospital-pharmacy-stocktake-scopes")
      return Response.json(
        withdrawn
          ? page([])
          : url.includes("cursor=")
            ? page([assignment])
            : page([], "TEST-scope-next"),
      );
    if (path.endsWith("/stocktake-outlet-choices"))
      return Response.json(url.includes("cursor=") ? page([outlet]) : page([], "TEST-outlet-next"));
    if (path === root) return Response.json(page([]));
    throw new Error("Unexpected TEST hospital worker request");
  });
  vi.stubGlobal("fetch", fetch);
  const { user } = render();
  await user.click(await screen.findByRole("button", { name: "Load more hospital departments" }));
  await user.click(
    await screen.findByRole("button", {
      name: "TEST Hospital · TEST Pharmacy department · Hospital pharmacy stocktake counter",
    }),
  );
  await user.click(
    await screen.findByRole("button", { name: "Load more approved hospital outlets" }),
  );
  await user.click(
    await screen.findByRole("button", {
      name: "TEST Hospital pharmacy · TEST Main outlet · TEST-MAIN",
    }),
  );
  await screen.findByText(
    "New stocktake work is not available. Retained status and recovery remain accessible.",
  );
  expect(screen.queryByRole("button", { name: "Start stocktake" })).not.toBeInTheDocument();
  await waitFor(() =>
    expect(
      fetch.mock.calls.some(([url]) => new URL(url, "https://stocktake.test").pathname === root),
    ).toBe(true),
  );
  for (const value of [
    account.tenantId,
    account.subject,
    assignment.id,
    assignment.hospital_id,
    assignment.department_id,
    outlet.pharmacy_id,
    outlet.outlet_id,
    outlet.binding_id,
  ])
    expect(document.body.textContent).not.toContain(value);
  expect(
    fetch.mock.calls.some(
      ([url]) =>
        url.includes("/operating-locations/") ||
        url.includes("/stock/") ||
        url.includes("/catalogue/"),
    ),
  ).toBe(false);
  withdrawn = true;
  await user.click(screen.getByRole("button", { name: "Refresh hospital departments" }));
  await waitFor(() =>
    expect(
      screen.queryByRole("button", {
        name: "TEST Hospital pharmacy · TEST Main outlet · TEST-MAIN",
      }),
    ).not.toBeInTheDocument(),
  );
  expect(
    screen.queryByText(
      "New stocktake work is not available. Retained status and recovery remain accessible.",
    ),
  ).not.toBeInTheDocument();
});

it("does not show names from an outlet projection belonging to another tenant", async () => {
  const fetch = vi.fn(async (url: string) => {
    const path = new URL(url, "https://stocktake.test").pathname;
    if (path === "/api/v1/iam/me/permissions")
      return Response.json({ data: [], tenant_id: account.tenantId, subject: account.subject });
    if (path === "/api/v1/workspace/release-capabilities")
      return Response.json(releaseSnapshotForTest(account, ["team_accounts", "pharmacy_setup"]));
    if (path === "/api/v1/iam/me/pharmacy-stocktake-locations") return Response.json(page([]));
    if (path === "/api/v1/iam/me/hospital-pharmacy-stocktake-scopes")
      return Response.json(page([assignment]));
    if (path.endsWith("/stocktake-outlet-choices"))
      return Response.json(page([{ ...outlet, owner_tenant_id: assignment.hospital_id }]));
    throw new Error("Unexpected TEST hospital worker request");
  });
  vi.stubGlobal("fetch", fetch);
  const { user } = render();
  await user.click(
    await screen.findByRole("button", {
      name: "TEST Hospital · TEST Pharmacy department · Hospital pharmacy stocktake counter",
    }),
  );
  await screen.findByText("Current hospital outlets could not be verified.");
  expect(screen.queryByText("TEST Hospital pharmacy")).not.toBeInTheDocument();
  expect(
    fetch.mock.calls.some(([url]) => new URL(url, "https://stocktake.test").pathname === root),
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

it.each(["reader", "counter", "reviewer"] as const)(
  "uses hospital routes and permits starts only for the released %s role",
  async (role) => {
    const definition = staffAccessTemplates.find(
      (item) => item.key === `pharmacy.department_stocktake_${role}.v1`,
    )!;
    const current = {
      ...assignment,
      template_key: definition.key,
      template_label: definition.name,
      assigned_permissions: definition.permissions,
    };
    let releaseStatus = 200;
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      const path = new URL(url, "https://stocktake.test").pathname;
      if (init?.method === "PUT") {
        expect(path.startsWith(`${root}/`)).toBe(true);
        expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(account.tenantId);
        return new Response(JSON.stringify({ error: "unavailable" }), { status: 503 });
      }
      if (path === "/api/v1/iam/me/permissions")
        return Response.json({ data: [], tenant_id: account.tenantId, subject: account.subject });
      if (path === "/api/v1/workspace/release-capabilities")
        return releaseStatus === 200
          ? Response.json(
              releaseSnapshotForTest(account, [
                "team_accounts",
                "pharmacy_setup",
                "pharmacy_stocktake",
              ]),
            )
          : new Response(JSON.stringify({ error: "unavailable" }), { status: releaseStatus });
      if (path === "/api/v1/iam/me/pharmacy-stocktake-locations") return Response.json(page([]));
      if (path === "/api/v1/iam/me/hospital-pharmacy-stocktake-scopes")
        return Response.json(page([current]));
      if (path.endsWith("/stocktake-outlet-choices")) return Response.json(page([outlet]));
      if (path === root) return Response.json(page([]));
      if (path === `${root}/quarantine-choices`)
        return Response.json(
          page([
            {
              id: "019a55c0-1111-7111-8111-111111111118",
              outlet_id: outlet.outlet_id,
              code: "TEST-Q",
              name: "TEST Quarantine",
              stock_status: "quarantine",
              storage_address: { zone_code: "", aisle_code: "", rack_code: "", shelf_code: "" },
              created_at: "2026-11-20T00:00:00Z",
            },
          ]),
        );
      throw new Error("Unexpected TEST hospital start request");
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
        name: `TEST Hospital · TEST Pharmacy department · ${definition.name}`,
      }),
    );
    await user.click(
      await screen.findByRole("button", {
        name: "TEST Hospital pharmacy · TEST Main outlet · TEST-MAIN",
      }),
    );
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(([url]) => new URL(url, "https://stocktake.test").pathname === root),
      ).toBe(true),
    );
    if (role !== "counter") {
      expect(screen.queryByRole("button", { name: "Start stocktake" })).not.toBeInTheDocument();
      expect(fetch.mock.calls.some(([url]) => url.includes("quarantine-choices"))).toBe(false);
    } else {
      await user.click(await screen.findByRole("button", { name: "Start stocktake" }));
      await user.click(await screen.findByRole("combobox", { name: "Quarantine destination" }));
      await user.click(await screen.findByRole("option", { name: "TEST Quarantine · TEST-Q" }));
      await user.type(
        screen.getByRole("textbox", { name: "Start reason" }),
        "TEST interrupted hospital count",
      );
      await user.click(screen.getByRole("button", { name: "Freeze book and start count" }));
      expect(await screen.findByRole("button", { name: "Check started count" })).toBeVisible();
      expect(fetch.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(1);
      releaseStatus = 503;
      await user.click(screen.getByRole("button", { name: "Refresh release availability" }));
      await screen.findByText(
        "New stocktake work is not available. Retained status and recovery remain accessible.",
      );
      expect(screen.queryByRole("button", { name: "Start stocktake" })).not.toBeInTheDocument();
      expect(
        await screen.findByRole("button", { name: "Check original start result" }),
      ).toBeVisible();
    }
    expect(
      fetch.mock.calls.some(
        ([url]) =>
          url.includes("/operating-locations/") ||
          url.includes("/stock/") ||
          url.includes("/catalogue/"),
      ),
    ).toBe(false);
  },
);
