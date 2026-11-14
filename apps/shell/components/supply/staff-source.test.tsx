import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { SupplierStaffSource } from "./staff-source";
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/supply/staff",
  useSearchParams: () => new URLSearchParams(),
}));
const supplier = "019a55c0-1111-7111-8111-111111111111";
const id = "019a55c0-1111-7111-8111-111111111112";
const identity = {
  tenant_id: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-owner",
  data: [
    "supply.directory.read",
    "workforce.independent_login.manage",
    "workforce.independent_login.review",
  ],
};
const row = {
  id,
  supplier_id: supplier,
  employee_code: "TEST-1",
  display_name: "TEST Warehouse Operator",
  job_category: "other",
  employment_status: "active",
  created_at: "2026-11-07T00:00:00Z",
  updated_at: "2026-11-07T00:00:00Z",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
it("opens the supplier's own directory and reviewed account links without hospital routes", async () => {
  const request = vi.fn(async (url: string) => {
    if (url.endsWith("permissions")) return Response.json(identity);
    if (url.includes("/account-links")) return Response.json(page([]));
    if (url.includes(`/staff/${id}?`))
      return Response.json({ ...row, version: 1, directory_context_version: "v1" });
    if (url.includes(`/suppliers/${supplier}/staff?`)) return Response.json(page([row]));
    if (url.includes("/workforce/suppliers?"))
      return Response.json(
        page([
          {
            supplier_id: supplier,
            name: "TEST Distributor",
            access_level: "manager",
            created_at: row.created_at,
          },
        ]),
      );
    return Response.json({ error: "not_found" }, { status: 404 });
  });
  vi.stubGlobal("fetch", request);
  renderWithProviders(
    <NavigationGuardProvider>
      <SupplierStaffSource />
    </NavigationGuardProvider>,
  );
  await screen.findByText("TEST Warehouse Operator");
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "Edit TEST Warehouse Operator" }));
  await screen.findByText("Reviewed accounts");
  expect(screen.getByRole("button", { name: "Propose account link" })).toBeInTheDocument();
  expect(
    request.mock.calls.some(([url]) => url.includes("/hospitals/") || url.includes("/pharmacies/")),
  ).toBe(false);
});
it("does not expose the Staff editor when private source discovery is refused", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url.endsWith("permissions")
        ? Response.json(identity)
        : Response.json({ error: "forbidden" }, { status: 403 }),
    ),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <SupplierStaffSource />
    </NavigationGuardProvider>,
  );
  await screen.findByRole("button", { name: "Reopen supplier Staff source" });
  expect(screen.queryByRole("button", { name: "Add staff" })).not.toBeInTheDocument();
});

it("discovers only authorized supplier Staff without supply catalogue permissions", async () => {
  const request = vi.fn(async (url: string) =>
    Response.json(
      url.endsWith("permissions")
        ? { ...identity, data: ["workforce.independent_login.review"] }
        : page([]),
    ),
  );
  vi.stubGlobal("fetch", request);
  renderWithProviders(
    <NavigationGuardProvider>
      <SupplierStaffSource />
    </NavigationGuardProvider>,
  );
  await screen.findByText("No supplier employer access on this page");
  expect(request.mock.calls.some(([url]) => url.includes("/workforce/suppliers?"))).toBe(true);
  expect(
    request.mock.calls.some(
      ([url]) => url.includes("/staff?") || url.startsWith("/api/v1/supply/"),
    ),
  ).toBe(false);
  expect(screen.queryByRole("button", { name: "Add staff" })).not.toBeInTheDocument();
});

it("opens a scoped supplier reviewer’s account controls without supply directory access", async () => {
  const request = vi.fn(async (url: string) => {
    if (url.endsWith("permissions"))
      return Response.json({ ...identity, data: ["workforce.independent_login.review"] });
    if (url.includes("/account-links")) return Response.json(page([]));
    if (url.includes(`/staff/${id}?`))
      return Response.json({ ...row, version: 1, directory_context_version: "v1" });
    if (url.includes(`/suppliers/${supplier}/staff?`)) return Response.json(page([row]));
    if (url.includes("/workforce/suppliers?"))
      return Response.json(
        page([
          {
            supplier_id: supplier,
            name: "TEST Distributor",
            access_level: "manager",
            created_at: row.created_at,
          },
        ]),
      );
    throw new Error(`Unexpected request ${url}`);
  });
  vi.stubGlobal("fetch", request);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <SupplierStaffSource />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Edit TEST Warehouse Operator" }));
  await screen.findByText("Reviewed accounts");
  expect(screen.queryByRole("button", { name: "Propose account link" })).not.toBeInTheDocument();
  expect(
    request.mock.calls.some(
      ([url]) => url.startsWith("/api/v1/supply/") || url.includes("/hospitals/"),
    ),
  ).toBe(false);
});
