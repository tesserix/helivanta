import { screen, within, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { NavigationGuardProvider } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";
import { LowStockPanel } from "./low-stock-panel";

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));
const scope = {
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const product = {
  id: "0199e000-0000-7000-8000-000000000002",
  version: 1,
  brand_name: "TEST Medicine",
  sku: "TEST-MED",
  unit_of_issue: "capsule",
  status: "active",
};
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
function renderPanel(configure = false) {
  return renderWithProviders(
    <NavigationGuardProvider>
      <LowStockPanel scope={scope} canConfigure={configure} />
    </NavigationGuardProvider>,
  );
}
it("keeps unavailable stock distinct from an empty list and never fetches configuration for a reader", async () => {
  const fetch = vi.fn().mockResolvedValue(json({ error: "unavailable" }, 503));
  vi.stubGlobal("fetch", fetch);
  renderPanel();
  expect(await screen.findByText("Low-stock availability could not be loaded.")).toBeVisible();
  expect(screen.queryByText(/No low-stock warnings/)).not.toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Configure reorder level" })).not.toBeInTheDocument();
  expect(fetch.mock.calls.every(([url]) => String(url).includes("/stock/low-products"))).toBe(true);
});
it("distinguishes unconfigured products from a zero-availability warning", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(json(page([]))));
  renderPanel();
  expect(await screen.findByText(/Products without a minimum are not counted/)).toBeVisible();
});
it("recovers an uncertain committed save by the original command, without a second write", async () => {
  let receipt: Record<string, unknown> | null = null;
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes.push(url);
        receipt = {
          ...JSON.parse(String(init.body)),
          id: url.split("/").at(-1),
          product_id: product.id,
          outlet_id: scope.outletId,
          version: 1,
          unit_of_issue: product.unit_of_issue,
          recorded_at: "2026-11-20T00:00:00Z",
        };
        throw new TypeError("TEST connection lost after commit");
      }
      if (url.includes("/reorder-levels/")) return json(receipt);
      if (url.endsWith("/reorder-level")) return json({ level: receipt });
      if (url.includes("/catalogue/products")) return json(page([product]));
      if (url.includes("/stock/low-products"))
        return json(
          page(
            receipt
              ? [
                  {
                    id: product.id,
                    outlet_id: scope.outletId,
                    sku: product.sku,
                    brand_name: product.brand_name,
                    unit_of_issue: product.unit_of_issue,
                    minimum_units: 0,
                    available_units: "0",
                    observed_at: "2026-11-20T00:00:00Z",
                    local_date: "2026-11-20",
                    time_zone: "Asia/Kolkata",
                  },
                ]
              : [],
          ),
        );
      throw new Error(`Unexpected request: ${url}`);
    }),
  );
  const { user } = renderPanel(true);
  await user.click(await screen.findByRole("combobox", { name: "Product" }));
  await user.click(await screen.findByRole("option", { name: "TEST Medicine · TEST-MED" }));
  const enabled = await screen.findByRole("checkbox", { name: "Enable low-stock warning" });
  await user.click(enabled);
  await user.type(screen.getByRole("textbox", { name: "Minimum (capsule)" }), "0");
  await user.click(screen.getByRole("button", { name: "Save reorder level" }));
  expect(await screen.findByText(/save could not be confirmed/)).toBeVisible();
  expect(enabled).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Check saved result" }));
  const list = within(screen.getByRole("region", { name: "Low stock" }));
  expect(await list.findByText("TEST Medicine")).toBeVisible();
  expect(list.getAllByText("0 capsule")).toHaveLength(2);
  await waitFor(() =>
    expect(screen.getByRole("checkbox", { name: "Enable low-stock warning" })).toBeEnabled(),
  );
  expect(writes).toHaveLength(1);
});
