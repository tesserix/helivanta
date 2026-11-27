import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { WardIndentRequests } from "./ward-indent-requests";

const ids = {
  tenant: "0199e000-0000-7000-8000-0000000000aa",
  link: "0199e000-0000-7000-8000-000000000030",
  hospital: "0199e000-0000-7000-8000-000000000021",
  department: "0199e000-0000-7000-8000-000000000022",
  outlet: "0199e000-0000-7000-8000-000000000001",
  pharmacy: "0199e000-0000-7000-8000-000000000010",
  occupancy: "0199e000-0000-7000-8000-000000000040",
  product: "0199e000-0000-7000-8000-000000000023",
};
const page = (rows: unknown[]) =>
  new Response(JSON.stringify({ data: rows, page: { has_more: false, next_cursor: null } }));
const scope = (permissions: string[]) => ({
  owner_tenant_id: ids.tenant,
  id: ids.link,
  hospital_id: ids.hospital,
  department_id: ids.department,
  hospital_name: "TEST General",
  department_name: "TEST Ward",
  template_key: "pharmacy.department_ward_indent_requester.v1",
  template_label: "Ward indent requester",
  assigned_permissions: permissions,
  snapshot_version: 1,
  authority_epoch: 1,
  expires_at: "2027-01-01T00:00:00Z",
});
const product = {
  id: ids.product,
  outlet_id: ids.outlet,
  sku: "TEST-1",
  brand_name: "TEST Brand",
  generic_name: "TEST generic",
  dosage_form: "tablet",
  ingredients: [{ name: "TEST ingredient", strength: "500", unit: "mg" }],
  pack_size: 10,
  unit_of_issue: "tablet",
  currency: "INR",
  version: 1,
  created_at: "2026-11-01T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());

it("shows an empty state when the account has no ward department", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => page([])),
  );
  renderWithProviders(<WardIndentRequests />);
  expect(await screen.findByText(/No department is assigned to you/)).toBeVisible();
});

it("raises an indent for the patient found in the bed, once per entry", async () => {
  const writes: { url: string; body: unknown }[] = [];
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "PUT") {
      writes.push({ url, body: JSON.parse(String(init.body)) });
      return new Response("{}", { status: 500 });
    }
    if (url.includes("hospital-ward-indent-scopes"))
      return page([
        scope(["pharmacy.indent.read", "pharmacy.indent.raise", "workforce.ward_indent_actor.use"]),
      ]);
    if (url.includes("ward-indent-outlet-choices"))
      return page([
        {
          id: ids.outlet,
          pharmacy_id: ids.pharmacy,
          pharmacy_name: "TEST Pharmacy",
          name: "Main",
          code: "MAIN",
        },
      ]);
    if (url.includes("ward-indent-occupant"))
      return new Response(
        JSON.stringify({
          occupancy_id: ids.occupancy,
          ward_code: "W1",
          room_code: "R2",
          bed_code: "B3",
        }),
      );
    if (url.includes("ward-indent-products")) return page([product]);
    if (url.includes("/ward-indents?")) return page([]);
    throw new Error(`Unexpected TEST endpoint ${url}`);
  });
  vi.stubGlobal("fetch", fetch);
  const { user } = renderWithProviders(<WardIndentRequests />);
  await user.click(await screen.findByRole("button", { name: "TEST General · TEST Ward" }));
  await user.click(await screen.findByRole("button", { name: "TEST Pharmacy · Main" }));
  await user.type(await screen.findByLabelText("Room"), "r2");
  await user.type(screen.getByLabelText("Bed"), "b3");
  await user.click(screen.getByRole("button", { name: "Find patient" }));
  await user.click(await screen.findByRole("button", { name: "Add TEST Brand" }));
  const units = screen.getByLabelText("Units for TEST Brand");
  await user.clear(units);
  await user.type(units, "4");
  await user.click(screen.getByRole("button", { name: "Raise indent" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]?.body).toEqual({
    occupancy_id: ids.occupancy,
    lines: [{ product_id: ids.product, requested_units: 4 }],
  });
  const first = writes[0]?.url;
  expect(first).toMatch(
    new RegExp(`/departments/${ids.department}/outlets/${ids.outlet}/ward-indents/[0-9a-f-]{36}$`),
  );
  expect(
    fetch.mock.calls.some(
      ([url]) =>
        String(url).includes("room_code=R2&bed_code=B3") ||
        String(url).includes("bed_code=B3&room_code=R2"),
    ),
  ).toBe(true);
});

it("offers no raising to a reader", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("hospital-ward-indent-scopes"))
        return page([scope(["pharmacy.indent.read", "workforce.ward_indent_actor.use"])]);
      if (url.includes("ward-indent-outlet-choices"))
        return page([
          {
            id: ids.outlet,
            pharmacy_id: ids.pharmacy,
            pharmacy_name: "TEST Pharmacy",
            name: "Main",
            code: "MAIN",
          },
        ]);
      return page([]);
    }),
  );
  const { user } = renderWithProviders(<WardIndentRequests />);
  await user.click(await screen.findByRole("button", { name: "TEST General · TEST Ward" }));
  await user.click(await screen.findByRole("button", { name: "TEST Pharmacy · Main" }));
  expect(await screen.findByText("No indents raised at this pharmacy yet.")).toBeVisible();
  expect(screen.queryByText("Raise an indent")).toBeNull();
});
