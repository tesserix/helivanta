import { screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { AccountPresentationsProvider, NavigationGuardProvider } from "@helivanta/ui";
import { StocktakeDetail } from "./stocktake-detail";

const scope = {
  subject: "TEST-viewer",
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const id = "0199e000-0000-7000-8000-000000000002";
const actor = "TEST-private-starter-subject";
const header = {
  id,
  outlet_id: scope.outletId,
  number: 1,
  selection: "all",
  product_ids: [],
  quarantine_location_id: id,
  reason: "TEST cycle",
  started_by: actor,
  started_at: "2026-11-20T00:00:00Z",
  snapshot_rows: 3,
  counted_rows: 2,
  difference_rows: 2,
  positive_difference_rows: 0,
  review_relationship: "starter",
  version: 1,
  state: "counting",
};
const line = {
  id,
  stocktake_id: id,
  product_id: id,
  product_version: 1,
  sku: "TEST",
  brand_name: "TEST Product",
  unit_of_issue: "tablet",
  batch_id: id,
  lot_number: "TEST-ZERO",
  expires_on: "2030-01-01",
  location_id: id,
  location_code: "TEST-Q",
  location_name: "TEST quarantine",
  stock_status: "quarantine",
  book_units: 10,
  movement_count: 1,
  regulatory_class: "H1",
  ndps_status: "no",
  schedule_declaration_id: id,
  counted_units: 0,
  counted_by: actor,
  count_reason: "TEST empty",
  counted_at: header.started_at,
  difference_units: -10,
  snapshotted_at: header.started_at,
};
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });
afterEach(() => vi.unstubAllGlobals());
function panel(canConfigure = false) {
  function PermissionFixture() {
    const [allowed, setAllowed] = useState(canConfigure);
    return (
      <>
        <button type="button" onClick={() => setAllowed((value) => !value)}>
          Toggle correction permission
        </button>
        <StocktakeDetail scope={scope} takeId={id} timeZone="Asia/Kolkata" canConfigure={allowed} />
      </>
    );
  }
  return renderWithProviders(
    <NavigationGuardProvider>
      <AccountPresentationsProvider scope={{ tenantId: scope.tenantId, subject: "TEST-viewer" }}>
        <PermissionFixture />
      </AccountPresentationsProvider>
    </NavigationGuardProvider>,
  );
}

it("retains an open physical entry but pauses it when correction permission is withdrawn", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("account-presentations"))
        return json({
          tenant_id: scope.tenantId,
          actor_subject: "TEST-viewer",
          data: [{ subject: actor, available: false, name: null, email: null, roles: [] }],
        });
      if (url.includes("/lines"))
        return json({ data: [line], page: { next_cursor: null, has_more: false } });
      return json(header);
    }),
  );
  const { user } = panel(true);
  await user.click(await screen.findByRole("button", { name: "Record count · TEST-ZERO" }));
  const quantity = screen.getByRole("textbox", { name: "Physical count (tablet)" });
  await user.type(quantity, "8");
  await user.click(screen.getByRole("button", { name: "Toggle correction permission" }));
  expect(quantity).toBeDisabled();
  expect(quantity).toHaveValue("8");
});
it("presents missing counts separately from zero and retains schedule groups and named people", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("account-presentations"))
        return json({
          tenant_id: scope.tenantId,
          actor_subject: "TEST-viewer",
          data: JSON.parse(String(init?.body)).subjects.map((subject: string) => ({
            subject,
            available: true,
            name: "TEST Stock Lead",
            email: "test@example.invalid",
            roles: [{ key: "pharmacist", label: "Pharmacist" }],
          })),
        });
      if (url.includes("/lines"))
        return json({
          data: [
            line,
            {
              ...line,
              id: "0199e000-0000-7000-8000-000000000003",
              lot_number: "TEST-MISSING",
              regulatory_class: "X",
              ndps_status: "yes",
              counted_units: null,
              counted_by: null,
              count_reason: null,
              counted_at: null,
              difference_units: null,
            },
            {
              ...line,
              id: "0199e000-0000-7000-8000-000000000005",
              lot_number: "TEST-NDPS",
              regulatory_class: "X",
              ndps_status: "yes",
            },
          ],
          page: { next_cursor: null, has_more: false },
        });
      return json(header);
    }),
  );
  panel();
  expect(await screen.findByRole("heading", { name: "Stocktake 1" })).toBeVisible();
  expect(await screen.findByText("Not counted")).toBeVisible();
  expect(await screen.findByRole("heading", { name: "Schedule H1 differences" })).toBeVisible();
  expect(await screen.findByRole("heading", { name: "NDPS differences" })).toBeVisible();
  expect((await screen.findAllByText("TEST Stock Lead")).length).toBeGreaterThan(0);
  expect(screen.queryByText(actor)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /Record count/ })).not.toBeInTheDocument();
});
it("shows an unavailable record rather than an empty or editable count", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(json({ error: "unavailable" }, 503)));
  panel(true);
  expect(await screen.findByText("The stocktake could not be loaded.")).toBeVisible();
  expect(screen.queryByRole("button", { name: /Record count/ })).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Download stocktake register" }),
  ).not.toBeInTheDocument();
});
