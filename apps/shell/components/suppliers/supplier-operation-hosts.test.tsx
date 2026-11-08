import { screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider, refusedDefault } from "@helivanta/ui";
import { HospitalSupplierOperations } from "./hospital-supplier-operations";
import { SourceSupplierOperations } from "./source-supplier-operations";
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn(), replace: vi.fn() }) }));
const id = (n: number) => `0199e000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const target = { hospitalId: id(1), departmentId: id(2), pharmacyId: id(3), outletId: id(4) };
const identity = { tenant_id: id(5), subject: "TEST-current-staff" };
const empty = { data: [], page: { has_more: false, next_cursor: null } };
afterEach(() => vi.unstubAllGlobals());
it("opens both hospital purposes through exact department routes without catalogue-directory authority", async () => {
  const fetch = vi.fn<(path: string, options?: RequestInit) => Promise<Response>>(async (path) =>
    String(path).endsWith("/iam/me/permissions") ? Response.json(identity) : Response.json(empty),
  );
  vi.stubGlobal("fetch", fetch);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <HospitalSupplierOperations {...target} />
    </NavigationGuardProvider>,
  );
  await screen.findByText("No supplier shipments for this outlet.");
  await user.click(screen.getByRole("button", { name: "Hospital pharmacy fulfilment" }));
  await screen.findByText("No supplier orders at this location.");
  const business = fetch.mock.calls.filter(
    ([path]) => !String(path).endsWith("/iam/me/permissions"),
  );
  expect(business.map(([path]) => String(path))).toEqual(
    expect.arrayContaining([
      expect.stringContaining(
        `/supply/hospitals/${target.hospitalId}/departments/${target.departmentId}/outlets/${target.outletId}/supplier-shipments`,
      ),
      expect.stringContaining(
        `/supply/hospitals/${target.hospitalId}/departments/${target.departmentId}/pharmacies/${target.pharmacyId}/outlets/${target.outletId}/supplier-orders`,
      ),
    ]),
  );
  for (const [, init] of business) {
    const headers = new Headers(init?.headers);
    expect(headers.get("X-Helivanta-Expected-Tenant")).toBe(identity.tenant_id);
    expect(headers.get("X-Helivanta-Expected-Subject")).toBe(identity.subject);
  }
  expect(
    fetch.mock.calls.some(
      ([path]) => String(path).includes("/catalogue") || String(path).includes("/pharmacies?"),
    ),
  ).toBe(false);
});
for (const product of ["pharmacy", "supply"])
  it(`opens the ${product} operating source without an administrative directory`, async () => {
    const fetch = vi.fn<(path: string, options?: RequestInit) => Promise<Response>>(async (path) =>
      String(path).endsWith("/iam/me/permissions") ? Response.json(identity) : Response.json(empty),
    );
    vi.stubGlobal("fetch", fetch);
    renderWithProviders(
      <NavigationGuardProvider>
        <SourceSupplierOperations product={product} organizationId={id(10)} locationId={id(11)} />
      </NavigationGuardProvider>,
    );
    await screen.findByText("No supplier orders at this location.");
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining(
        `/supply/stock-owners/${product}/organizations/${id(10)}/locations/${id(11)}/supplier-orders`,
      ),
      expect.objectContaining({ cache: "no-store" }),
    );
  });
it("shows current-scope denial even when the hospital workspace link is structurally valid", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path) =>
      String(path).endsWith("/iam/me/permissions")
        ? Response.json(identity)
        : Response.json(
            { error: "order_forbidden", message: "TEST no current duty" },
            { status: 403 },
          ),
    ),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <HospitalSupplierOperations {...target} />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByText(refusedDefault)).toBeInTheDocument();
  expect(screen.queryByText("No supplier shipments for this outlet.")).not.toBeInTheDocument();
});
