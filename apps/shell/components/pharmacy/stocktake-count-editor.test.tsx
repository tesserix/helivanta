import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import type { PharmacyStocktake, PharmacyStocktakeLine } from "@helivanta/api";
import { StocktakeCountEditor } from "./stocktake-count-editor";

const scope = {
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const take: PharmacyStocktake = {
  id: "0199e000-0000-7000-8000-000000000002",
  outlet_id: scope.outletId,
  number: 1,
  selection: "all",
  product_ids: [],
  quarantine_location_id: "0199e000-0000-7000-8000-000000000003",
  reason: "TEST count",
  started_by: "TEST starter",
  started_at: "2026-11-20T00:00:00Z",
  snapshot_rows: 1,
  counted_rows: 0,
  difference_rows: 0,
  positive_difference_rows: 0,
  review_relationship: "starter",
  state: "counting",
  version: 0,
};
const line: PharmacyStocktakeLine = {
  id: "0199e000-0000-7000-8000-000000000004",
  stocktake_id: take.id,
  product_id: take.id,
  product_version: 1,
  sku: "TEST-SKU",
  brand_name: "TEST Product",
  unit_of_issue: "tablet",
  batch_id: take.id,
  lot_number: "TEST-LOT",
  expires_on: "2030-01-01",
  location_id: take.quarantine_location_id,
  location_code: "TEST-Q",
  location_name: "TEST quarantine",
  stock_status: "quarantine",
  book_units: 10,
  movement_count: 1,
  regulatory_class: null,
  ndps_status: "unknown",
  schedule_declaration_id: null,
  counted_units: null,
  counted_by: null,
  count_reason: null,
  counted_at: null,
  difference_units: null,
  snapshotted_at: take.started_at,
};
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });
afterEach(() => vi.unstubAllGlobals());
function panel() {
  const saved = vi.fn();
  return {
    ...renderWithProviders(
      <NavigationGuardProvider>
        <StocktakeCountEditor scope={scope} take={take} line={line} onSaved={saved} />
      </NavigationGuardProvider>,
    ),
    saved,
  };
}

it("keeps an untouched count blank and records an explicit physical zero", async () => {
  const fetch = vi.fn(async (_url: string, init?: RequestInit) => {
    const input = JSON.parse(String(init?.body));
    const command = _url.split("/").at(-1);
    return json({
      ...input,
      id: command,
      stocktake_id: take.id,
      outlet_id: scope.outletId,
      version: 1,
      state: "counting",
      recorded_by: "TEST counter",
      recorded_at: take.started_at,
    });
  });
  vi.stubGlobal("fetch", fetch);
  const { user, saved } = panel();
  const quantity = screen.getByRole("textbox", { name: "Physical count (tablet)" });
  expect(quantity).toHaveValue("");
  await user.type(screen.getByRole("textbox", { name: "Count reason" }), "TEST empty shelf");
  await user.click(screen.getByRole("button", { name: "Save physical count" }));
  expect(fetch).not.toHaveBeenCalled();
  await user.type(quantity, "0");
  await user.click(screen.getByRole("button", { name: "Save physical count" }));
  await waitFor(() => expect(saved).toHaveBeenCalledOnce());
  expect(JSON.parse(String(fetch.mock.calls[0]?.[1]?.body))).toEqual({
    expected_version: 0,
    counts: [{ line_id: line.id, counted_units: 0 }],
    reason: "TEST empty shelf",
  });
});

it("freezes an uncertain command, checks recovery and retries its exact original input", async () => {
  let savedBody = "";
  let savedURL = "";
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method !== "PUT") return json({ error: "not_found" }, 404);
    if (!savedBody) {
      savedBody = String(init.body);
      savedURL = url;
      throw new TypeError("TEST disconnected");
    }
    expect(url).toBe(savedURL);
    expect(init.body).toBe(savedBody);
    return json({
      ...JSON.parse(savedBody),
      id: url.split("/").at(-1),
      stocktake_id: take.id,
      outlet_id: scope.outletId,
      version: 1,
      state: "counting",
      recorded_by: "TEST counter",
      recorded_at: take.started_at,
    });
  });
  vi.stubGlobal("fetch", fetch);
  const { user, saved } = panel();
  const quantity = screen.getByRole("textbox", { name: "Physical count (tablet)" });
  await user.type(quantity, "8");
  await user.type(screen.getByRole("textbox", { name: "Count reason" }), "TEST checked");
  await user.click(screen.getByRole("button", { name: "Save physical count" }));
  await screen.findByText(/save could not be confirmed/i);
  expect(quantity).toBeDisabled();
  expect(screen.queryByRole("button", { name: "Retry original count" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Check saved count" }));
  await user.click(await screen.findByRole("button", { name: "Retry original count" }));
  await waitFor(() => expect(saved).toHaveBeenCalledOnce());
  expect(fetch).toHaveBeenCalledTimes(3);
});
