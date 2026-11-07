import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { SupplierEmployerRegistration } from "./employer-registration";
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/supply/parties",
  useSearchParams: () => new URLSearchParams(),
}));
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-owner",
  partyId: "019a55c0-1111-7111-8111-111111111111",
};
afterEach(() => vi.unstubAllGlobals());
it("recovers supplier employer registration without repeating a lost write", async () => {
  let writes = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return Response.json({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          data: ["workforce.supplier.manage"],
        });
      if (init?.method === "PUT") {
        writes++;
        throw new TypeError("TEST lost committed registration");
      }
      return Response.json({
        supplier_id: scope.partyId,
        name: "TEST Distributor",
        source_version: 1,
        created_at: "2026-11-07T00:00:00Z",
      });
    }),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <SupplierEmployerRegistration scope={scope} />
    </NavigationGuardProvider>,
  );
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Register supplier employer" }));
  expect(
    await screen.findByRole("button", { name: "Recover employer registration" }),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Register supplier employer" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Recover employer registration" }));
  await screen.findByText("Supplier Staff directory registration verified.");
  expect(writes).toBe(1);
});
