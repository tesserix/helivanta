import { screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { AccountPresentationsProvider, NavigationGuardProvider } from "@helivanta/ui";
import { StockWorkspace } from "./stock-workspace";
import { retainStocktakeCommand } from "./stocktake-recovery";
const navigation = vi.hoisted(() => ({ search: "view=counts", push: vi.fn() }));
vi.mock("next/navigation", () => ({
  usePathname: () => "/pharmacy/stock",
  useSearchParams: () => new URLSearchParams(navigation.search),
  useRouter: () => ({ push: navigation.push, replace: vi.fn() }),
}));
const scope = {
  subject: "TEST-viewer",
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
afterEach(() => {
  vi.unstubAllGlobals();
  navigation.search = "view=counts";
  navigation.push.mockReset();
  sessionStorage.clear();
});
function panel(canCount = false) {
  return renderWithProviders(
    <NavigationGuardProvider>
      <AccountPresentationsProvider scope={{ tenantId: scope.tenantId, subject: "TEST-viewer" }}>
        <StockWorkspace
          subject={scope.subject}
          scope={scope}
          timeZone="Asia/Kolkata"
          canConfigure={true}
          canCount={canCount}
        />
      </AccountPresentationsProvider>
    </NavigationGuardProvider>,
  );
}

it("restores a pending command after reload and pauses new count entry until its result is resolved", async () => {
  retainStocktakeCommand(scope, {
    kind: "count",
    stocktakeId: "0199e000-0000-7000-8000-000000000002",
    commandId: "0199e000-0000-7000-8000-000000000003",
  });
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ data: [], page: { next_cursor: null, has_more: false } })),
      ),
  );
  panel(true);
  expect(await screen.findByRole("button", { name: "Check original count result" })).toBeVisible();
  expect(screen.queryByRole("button", { name: "Start stocktake" })).not.toBeInTheDocument();
});
it("routes the stocktake tab to its real list and keeps catalogue-only editors read-only", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ data: [], page: { next_cursor: null, has_more: false } })),
      ),
  );
  panel();
  expect(await screen.findByText(/No stocktakes yet/)).toBeVisible();
  expect(screen.getByRole("link", { name: "Stocktakes" })).toHaveAttribute("aria-current", "page");
  expect(screen.queryByRole("button", { name: "Start stocktake" })).not.toBeInTheDocument();
});
it("opens the start form only for a current stock-correction editor", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ data: [], page: { next_cursor: null, has_more: false } })),
      ),
  );
  const { user } = panel(true);
  await user.click(await screen.findByRole("button", { name: "Start stocktake" }));
  expect(await screen.findByRole("button", { name: "Freeze book and start count" })).toBeVisible();
});
