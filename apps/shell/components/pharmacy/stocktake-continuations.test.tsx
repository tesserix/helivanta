import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { AccountPresentationsProvider, NavigationGuardProvider } from "@helivanta/ui";
import { StocktakeContinuations } from "./stocktake-continuations";
const scope = {
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const take = {
  id: "0199e000-0000-7000-8000-000000000002",
  outlet_id: scope.outletId,
  number: 1,
  selection: "all",
  product_ids: [],
  quarantine_location_id: "0199e000-0000-7000-8000-000000000003",
  reason: "TEST cycle",
  started_by: "TEST starter",
  started_at: "2026-11-20T00:00:00Z",
  snapshot_rows: 2,
  state: "submitted",
  version: 2,
};
const json = (body: unknown) => new Response(JSON.stringify(body));
afterEach(() => vi.unstubAllGlobals());
it("inspects and cancels retained work without touching stock entry, inventory lines or approval APIs", async () => {
  const writes: string[] = [];
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.includes("account-presentations"))
      return json({
        tenant_id: scope.tenantId,
        actor_subject: "TEST-viewer",
        data: JSON.parse(String(init?.body)).subjects.map((subject: string) => ({
          subject,
          available: true,
          name: "TEST Stock Lead",
          email: "test@example.invalid",
          roles: [],
        })),
      });
    if (url.includes("/cancellations/") && init?.method === "PUT") {
      writes.push(url);
      return json({
        ...JSON.parse(String(init.body)),
        extra_units_disposition: null,
        id: url.split("/").at(-1),
        stocktake_id: take.id,
        outlet_id: scope.outletId,
        version: 3,
        state: "cancelled",
        adjustment_count: 0,
        recorded_by: "TEST viewer",
        recorded_at: take.started_at,
      });
    }
    if (url.endsWith(`/${take.id}`))
      return json({
        ...take,
        counted_rows: 2,
        difference_rows: 1,
        positive_difference_rows: 0,
        review_relationship: "independent",
      });
    if (url.includes("/stock/stocktakes?"))
      return json({ data: [take], page: { next_cursor: null, has_more: false } });
    throw new Error(`Unexpected TEST endpoint: ${url}`);
  });
  vi.stubGlobal("fetch", fetch);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccountPresentationsProvider scope={{ tenantId: scope.tenantId, subject: "TEST-viewer" }}>
        <StocktakeContinuations scope={scope} timeZone="Asia/Kolkata" canCancel />
      </AccountPresentationsProvider>
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Inspect stocktake 1" }));
  await user.click(await screen.findByRole("button", { name: "Cancel retained stocktake" }));
  await user.click(screen.getByRole("combobox", { name: "Decision" }));
  expect(screen.queryByRole("option", { name: /Approve/ })).not.toBeInTheDocument();
  await user.click(screen.getByRole("option", { name: "Cancel stocktake" }));
  await user.type(
    screen.getByRole("textbox", { name: "Decision reason" }),
    "TEST abandoned safely",
  );
  await user.click(screen.getByRole("button", { name: "Record decision" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(fetch.mock.calls.some(([url]) => /\/lines|register\.csv|\/decisions\//.test(url))).toBe(
    false,
  );
  expect(screen.queryByRole("button", { name: "Start stocktake" })).not.toBeInTheDocument();
});
