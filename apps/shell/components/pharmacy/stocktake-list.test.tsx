import { screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { AccountPresentationsProvider, NavigationGuardProvider } from "@helivanta/ui";
import { StocktakeList } from "./stocktake-list";

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));
const scope = {
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const actor = "0199e000-0000-7000-8000-0000000000ef";
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
const count = {
  id: "0199e000-0000-7000-8000-000000000002",
  outlet_id: scope.outletId,
  number: 1,
  state: "counting",
  version: 0,
  selection: "all",
  product_ids: [],
  quarantine_location_id: "0199e000-0000-7000-8000-000000000003",
  reason: "TEST count",
  started_by: actor,
  started_at: "2026-11-20T00:00:00Z",
  snapshot_rows: 2,
};
afterEach(() => vi.unstubAllGlobals());
function panel(onStart?: () => void) {
  return renderWithProviders(
    <NavigationGuardProvider>
      <AccountPresentationsProvider scope={{ tenantId: scope.tenantId, subject: "TEST-viewer" }}>
        <StocktakeList scope={scope} timeZone="Asia/Kolkata" onStart={onStart} />
      </AccountPresentationsProvider>
    </NavigationGuardProvider>,
  );
}
it("keeps unavailable stocktakes distinct from an empty result and pauses creation", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(json({ error: "unavailable" }, 503)));
  panel(vi.fn());
  expect(await screen.findByText("Stocktakes could not be loaded.")).toBeVisible();
  expect(screen.queryByText(/No stocktakes yet/)).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Start stocktake" })).toBeDisabled();
});
it("presents human count references and actual account names and roles without exposing account IDs", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("/iam/account-presentations"))
        return json({
          tenant_id: scope.tenantId,
          actor_subject: "TEST-viewer",
          data: JSON.parse(String(init?.body)).subjects.map((subject: string) => ({
            subject,
            available: true,
            name: "TEST Stock Lead",
            email: "test-stocklead@example.invalid",
            roles: [{ key: "pharmacist", label: "Pharmacist" }],
          })),
        });
      return json(page([count]));
    }),
  );
  panel();
  expect(await screen.findByRole("link", { name: "Open stocktake 1" })).toHaveAttribute(
    "href",
    expect.stringContaining(`count=${count.id}`),
  );
  expect(await screen.findByText("TEST Stock Lead")).toBeVisible();
  expect(await screen.findByText("Pharmacist")).toBeVisible();
  expect(screen.queryByText(actor)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Start stocktake" })).not.toBeInTheDocument();
});
it("starts only when the authorized caller has no open count", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(json(page([]))));
  const start = vi.fn();
  const { user } = panel(start);
  await screen.findByText(/No stocktakes yet/);
  await user.click(screen.getByRole("button", { name: "Start stocktake" }));
  expect(start).toHaveBeenCalledTimes(1);
});
