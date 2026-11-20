import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { StocktakeStart } from "./stocktake-start";
const scope = {
  subject: "TEST-viewer",
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const location = "0199e000-0000-7000-8000-000000000003";
const product = "0199e000-0000-7000-8000-000000000004";
const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200 });
const page = (data: unknown[]) => ({ data, page: { next_cursor: null, has_more: false } });
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  sessionStorage.clear();
});
function panel() {
  const started = vi.fn();
  return {
    ...renderWithProviders(
      <NavigationGuardProvider>
        <StocktakeStart scope={scope} onStarted={started} />
      </NavigationGuardProvider>,
    ),
    started,
  };
}

it("does not start a count if its recovery reference cannot be retained", async () => {
  const fetch = vi.fn().mockResolvedValue(
    json(
      page([
        {
          id: location,
          outlet_id: scope.outletId,
          code: "TEST-Q",
          name: "TEST Quarantine",
          stock_status: "quarantine",
          created_at: "2026-11-20T00:00:00Z",
        },
      ]),
    ),
  );
  vi.stubGlobal("fetch", fetch);
  const { user } = panel();
  await user.click(screen.getByRole("combobox", { name: "Quarantine destination" }));
  await user.click(await screen.findByRole("option", { name: "TEST Quarantine · TEST-Q" }));
  await user.type(screen.getByRole("textbox", { name: "Start reason" }), "TEST monthly count");
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new DOMException("TEST quota", "QuotaExceededError");
  });
  await user.click(screen.getByRole("button", { name: "Freeze book and start count" }));
  await screen.findByText(/No start request was sent/i);
  expect(fetch.mock.calls.some((call) => call[1]?.method === "PUT")).toBe(false);
});
it("starts a selected-product snapshot with a named quarantine target and required reason", async () => {
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.includes("/stock/locations"))
      return json(
        page([
          {
            id: location,
            outlet_id: scope.outletId,
            code: "TEST-Q",
            name: "TEST Quarantine",
            stock_status: "quarantine",
            created_at: "2026-11-20T00:00:00Z",
          },
          { id: product, name: "TEST Shelf", code: "TEST-S", stock_status: "sellable" },
        ]),
      );
    if (url.includes("/catalogue/products"))
      return json(
        page([{ id: product, brand_name: "TEST Product", sku: "TEST-SKU", status: "active" }]),
      );
    const input = JSON.parse(String(init?.body));
    return json({
      ...input,
      id: url.split("/").at(-1),
      outlet_id: scope.outletId,
      number: 1,
      started_by: scope.subject,
      started_at: "2026-11-20T00:00:00Z",
      snapshot_rows: 1,
      counted_rows: 0,
      difference_rows: 0,
      positive_difference_rows: 0,
      review_relationship: "starter",
      version: 0,
      state: "counting",
    });
  });
  vi.stubGlobal("fetch", fetch);
  const { user, started } = panel();
  await user.click(screen.getByRole("combobox", { name: "Product scope" }));
  await user.click(screen.getByRole("option", { name: "Selected products" }));
  await user.click(await screen.findByRole("checkbox", { name: "TEST Product · TEST-SKU" }));
  await user.click(screen.getByRole("combobox", { name: "Quarantine destination" }));
  expect(screen.queryByRole("option", { name: /TEST Shelf/ })).not.toBeInTheDocument();
  await user.click(screen.getByRole("option", { name: "TEST Quarantine · TEST-Q" }));
  await user.type(
    screen.getByRole("textbox", { name: "Start reason" }),
    "TEST monthly shelf count",
  );
  await user.click(screen.getByRole("button", { name: "Freeze book and start count" }));
  await waitFor(() => expect(started).toHaveBeenCalledOnce());
  const save = fetch.mock.calls.find((call) => call[1]?.method === "PUT");
  expect(JSON.parse(String(save?.[1]?.body))).toEqual({
    selection: "selected",
    product_ids: [product],
    quarantine_location_id: location,
    reason: "TEST monthly shelf count",
  });
});
