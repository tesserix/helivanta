import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import type { PharmacyStocktake } from "@helivanta/api";
import { StocktakeDecision } from "./stocktake-decision";

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
  snapshot_rows: 2,
  counted_rows: 2,
  difference_rows: 2,
  positive_difference_rows: 1,
  review_relationship: "independent",
  state: "submitted",
  version: 2,
};
const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200 });
afterEach(() => vi.unstubAllGlobals());
function panel(current = take) {
  const saved = vi.fn();
  return {
    ...renderWithProviders(
      <NavigationGuardProvider>
        <StocktakeDecision scope={scope} take={current} onSaved={saved} />
      </NavigationGuardProvider>,
    ),
    saved,
  };
}

it("requires explicit physical quarantine attestation for positive differences before approval", async () => {
  const fetch = vi.fn(async (url: string, init?: RequestInit) =>
    json({
      ...JSON.parse(String(init?.body)),
      id: url.split("/").at(-1),
      stocktake_id: take.id,
      outlet_id: scope.outletId,
      version: 3,
      state: "posted",
      adjustment_count: 2,
      recorded_by: "TEST independent reviewer",
      recorded_at: take.started_at,
    }),
  );
  vi.stubGlobal("fetch", fetch);
  const { user, saved } = panel();
  await user.type(
    screen.getByRole("textbox", { name: "Decision reason" }),
    "TEST physically checked",
  );
  await user.click(screen.getByRole("button", { name: "Record decision" }));
  expect(fetch).not.toHaveBeenCalled();
  await user.click(screen.getByRole("checkbox", { name: /physically moved all extra units/i }));
  await user.click(screen.getByRole("button", { name: "Record decision" }));
  await waitFor(() => expect(saved).toHaveBeenCalledOnce());
  expect(JSON.parse(String(fetch.mock.calls[0]?.[1]?.body))).toEqual({
    decision: "approve",
    reason: "TEST physically checked",
    expected_version: 2,
    extra_units_disposition: "moved_to_quarantine",
  });
});

it.each(["starter", "counter"] as const)(
  "offers no approval or rejection to a %s",
  async (review_relationship) => {
    const { user } = panel({ ...take, review_relationship });
    await user.click(screen.getByRole("combobox", { name: "Decision" }));
    expect(screen.queryByRole("option", { name: /Approve/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("option", { name: /Reject/ })).not.toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Cancel stocktake" })).toBeInTheDocument();
  },
);

it("does not offer submission while any snapshot bucket is uncounted", async () => {
  const { user } = panel({
    ...take,
    state: "counting",
    counted_rows: 1,
    difference_rows: 1,
    positive_difference_rows: 0,
    review_relationship: "starter",
  });
  await user.click(screen.getByRole("combobox", { name: "Decision" }));
  expect(screen.queryByRole("option", { name: /Submit/ })).not.toBeInTheDocument();
  expect(screen.getByRole("option", { name: "Cancel stocktake" })).toBeInTheDocument();
});
