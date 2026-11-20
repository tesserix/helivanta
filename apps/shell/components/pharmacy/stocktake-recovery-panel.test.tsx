import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { AccountPresentationsProvider } from "@helivanta/ui";
import { StocktakeRecoveryPanel } from "./stocktake-recovery-panel";
import { pendingStocktakeCommands, retainStocktakeCommand } from "./stocktake-recovery";
const scope = {
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  subject: "TEST-viewer",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const marker = {
  kind: "count" as const,
  stocktakeId: "0199e000-0000-7000-8000-000000000002",
  commandId: "0199e000-0000-7000-8000-000000000003",
};
const receipt = {
  id: marker.commandId,
  stocktake_id: marker.stocktakeId,
  outlet_id: scope.outletId,
  expected_version: 0,
  version: 1,
  state: "counting",
  counts: [{ line_id: scope.pharmacyId, counted_units: 0 }],
  reason: "TEST private observation",
  recorded_by: scope.subject,
  recorded_at: "2026-11-20T00:00:00Z",
};
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  sessionStorage.clear();
});
function panel(current = scope) {
  return renderWithProviders(
    <AccountPresentationsProvider scope={{ tenantId: current.tenantId, subject: current.subject }}>
      <StocktakeRecoveryPanel scope={current} timeZone="Asia/Kolkata" />
    </AccountPresentationsProvider>,
  );
}

it("recovers an original count after its form is gone without sending another stock command", async () => {
  retainStocktakeCommand(scope, marker);
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.includes("account-presentations"))
      return json({
        tenant_id: scope.tenantId,
        actor_subject: scope.subject,
        data: [
          {
            subject: scope.subject,
            available: true,
            name: "TEST Stock Lead",
            email: "test@example.invalid",
            roles: [],
          },
        ],
      });
    expect(init?.method).toBeUndefined();
    expect(new Headers(init?.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
    expect(url).toContain(`/stocktakes/${marker.stocktakeId}/counts/${marker.commandId}`);
    return json(receipt);
  });
  vi.stubGlobal("fetch", fetch);
  const { user } = panel();
  await user.click(screen.getByRole("button", { name: "Check original count result" }));
  expect(await screen.findByText(/Original physical count was recorded/i)).toBeVisible();
  expect(await screen.findByText("TEST Stock Lead")).toBeVisible();
  expect(screen.queryByText(marker.commandId)).not.toBeInTheDocument();
  expect(screen.queryByText(receipt.reason)).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Acknowledge saved result" }));
  await waitFor(() => expect(pendingStocktakeCommands(scope)).toEqual([]));
  expect(fetch.mock.calls.filter(([url]) => !url.includes("account-presentations"))).toHaveLength(
    1,
  );
});
it("refuses to adopt a saved receipt from another account", async () => {
  retainStocktakeCommand(scope, marker);
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(json({ ...receipt, recorded_by: "TEST other account" })),
  );
  const { user } = panel();
  await user.click(screen.getByRole("button", { name: "Check original count result" }));
  expect(
    await screen.findByText("The original result could not be verified for this account."),
  ).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "Acknowledge saved result" }),
  ).not.toBeInTheDocument();
  expect(pendingStocktakeCommands(scope)).toEqual([marker]);
});
it("keeps not-found outcomes explicitly unknown until the user dismisses the reference", async () => {
  retainStocktakeCommand(scope, marker);
  const fetch = vi.fn().mockResolvedValue(json({ error: "not_found" }, 404));
  vi.stubGlobal("fetch", fetch);
  const { user } = panel();
  await user.click(screen.getByRole("button", { name: "Check original count result" }));
  expect(await screen.findByText(/original request may still finish/i)).toBeVisible();
  expect(pendingStocktakeCommands(scope)).toEqual([marker]);
  await user.click(screen.getByRole("button", { name: "Dismiss missing reference" }));
  await waitFor(() => expect(pendingStocktakeCommands(scope)).toEqual([]));
  expect(fetch).toHaveBeenCalledTimes(1);
});
it("does not read another account's markers or issue recovery traffic", () => {
  retainStocktakeCommand(scope, marker);
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  panel({ ...scope, subject: "TEST different viewer" });
  expect(
    screen.queryByRole("button", { name: "Check original count result" }),
  ).not.toBeInTheDocument();
  expect(fetch).not.toHaveBeenCalled();
});
