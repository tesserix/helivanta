import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { newSupplyPartyInput } from "@helivanta/api";
import { renderWithProviders } from "@helivanta/api/testing";
import { PartiesWorkspace } from "./parties-workspace";
const id = "0199e000-0000-7000-8000-000000000020";
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
function fixture(roles: string[] = [], allowed = true) {
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({
          tenant_id: "11111111-1111-1111-1111-111111111111",
          subject: "TEST-owner",
          data: allowed
            ? [
                "supply.directory.read",
                "supply.party.read",
                "supply.party.write",
                "supply.organization.manage",
              ]
            : [],
        }),
      );
    if (url.endsWith("/access")) return new Response(JSON.stringify({ roles }));
    if (init?.method === "PUT")
      return new Response(JSON.stringify({ error: "internal", message: "TEST save unavailable" }), {
        status: 500,
      });
    if (url.endsWith(`/supply/parties/${id}`))
      return new Response(
        JSON.stringify({
          ...newSupplyPartyInput(),
          id,
          code: "TEST-ONE",
          legal_name: "TEST Private Supplier",
          trading_name: "TEST Trade",
          jurisdiction: "TEST-MH",
          registration_number: "TEST-REG-001",
          version: 1,
          onboarding_status: "draft",
          created_at: "2026-10-16T00:00:00Z",
          updated_at: "2026-10-16T00:00:00Z",
        }),
      );
    if (url.endsWith("/revisions")) return new Response(JSON.stringify(page([])));
    if (url.includes("/supply/parties"))
      return new Response(
        JSON.stringify(
          page([
            {
              id,
              code: "TEST-ONE",
              legal_name: "TEST Private Supplier",
              trading_name: "TEST Trade",
              record_kind: "counterparty",
              business_kind: "distributor",
              country_code: "IN",
              onboarding_status: "draft",
              access_roles: roles,
            },
          ]),
        ),
      );
    return new Response(JSON.stringify(page([])));
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
it("does not query the directory without permission", async () => {
  const fetcher = fixture([], false);
  renderWithProviders(<PartiesWorkspace />);
  expect(await screen.findByText("Supply access required")).toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => url.includes("/supply/"))).toBe(false);
});
it("keeps private detail requests behind explicit access", async () => {
  const fetcher = fixture();
  renderWithProviders(<PartiesWorkspace />);
  await userEvent.click(await screen.findByRole("button", { name: /TEST Private Supplier/ }));
  expect(await screen.findByText("Private profile access required")).toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => url.endsWith(`/parties/${id}`))).toBe(false);
});
it("focuses the new profile and preserves the aggregate identity after a failed save", async () => {
  const fetcher = fixture();
  const user = userEvent.setup();
  const scroll = vi.fn();
  HTMLElement.prototype.scrollIntoView = scroll;
  renderWithProviders(<PartiesWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Add organisation" }));
  await waitFor(() => expect(screen.getByLabelText("Organisation code")).toHaveFocus());
  expect(scroll).toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Save draft profile" }));
  expect(await screen.findAllByRole("alert")).not.toHaveLength(0);
  expect(fetcher.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(0);
  await user.type(screen.getByLabelText("Organisation code"), "TEST-NEW");
  await user.type(screen.getByLabelText("Legal name"), "TEST New Distributor");
  await user.type(screen.getByLabelText("Registration jurisdiction"), "TEST-MH");
  await user.type(screen.getByLabelText("Business registration number"), "TEST-REG-001");
  await user.click(screen.getByRole("button", { name: "Save draft profile" }));
  expect(await screen.findByText("TEST save unavailable")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Save draft profile" }));
  await waitFor(() =>
    expect(fetcher.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(2),
  );
  const writes = fetcher.mock.calls.filter(([, init]) => init?.method === "PUT");
  expect(writes[0][0]).toBe(writes[1][0]);
  expect(writes[0][1]?.body).toBe(writes[1][1]?.body);
  expect(JSON.parse(writes[0][1]?.body as string)).toMatchObject({
    expected_version: 0,
    legal_name: "TEST New Distributor",
    contacts: [],
    addresses: [],
    tax_registrations: [],
  });
});

it("keeps an explicitly granted auditor read-only despite global write permission", async () => {
  const fetcher = fixture(["auditor"]);
  renderWithProviders(<PartiesWorkspace />);
  await userEvent.click(await screen.findByRole("button", { name: /TEST Private Supplier/ }));
  expect(await screen.findByText("TEST-REG-001")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Edit profile" })).not.toBeInTheDocument();
  expect(fetcher.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(0);
});

it("adds each structured collection and preserves the remaining contact after removal", async () => {
  const fetcher = fixture();
  const user = userEvent.setup();
  renderWithProviders(<PartiesWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Add organisation" }));
  await user.type(screen.getByLabelText("Organisation code"), "TEST-ARRAYS");
  await user.type(screen.getByLabelText("Legal name"), "TEST Structured Distributor");
  await user.type(screen.getByLabelText("Registration jurisdiction"), "TEST-MH");
  await user.type(screen.getByLabelText("Business registration number"), "TEST-ARRAYS-001");
  await user.click(screen.getByRole("button", { name: "Add contact" }));
  await user.type(screen.getByLabelText("Contact name"), "TEST Removed");
  await user.click(screen.getByRole("button", { name: "Add contact" }));
  await user.type(screen.getAllByLabelText("Contact name")[1], "TEST Retained");
  await user.click(screen.getByRole("button", { name: "Remove contact 1" }));
  expect(screen.getByLabelText("Contact name")).toHaveValue("TEST Retained");
  await user.click(screen.getByRole("button", { name: "Add address" }));
  await user.type(screen.getByLabelText("Address line 1"), "TEST Office");
  await user.type(screen.getByLabelText("City"), "TEST City");
  await user.type(screen.getByLabelText("State or province"), "TEST State");
  await user.type(screen.getByLabelText("Postal code"), "TEST-001");
  await user.click(screen.getByRole("button", { name: "Add tax registration" }));
  await user.type(screen.getByLabelText("Tax identifier"), "TEST-TAX");
  await user.type(screen.getByLabelText("Tax jurisdiction"), "TEST-MH");
  await user.click(screen.getByRole("button", { name: "Save draft profile" }));
  expect(await screen.findByText("TEST save unavailable")).toBeInTheDocument();
  const writes = fetcher.mock.calls.filter(([, init]) => init?.method === "PUT");
  expect(writes).toHaveLength(1);
  const saved = JSON.parse(writes[0][1]?.body as string);
  expect(saved.contacts).toHaveLength(1);
  expect(saved.contacts[0].name).toBe("TEST Retained");
  expect(saved.addresses).toHaveLength(1);
  expect(saved.tax_registrations).toHaveLength(1);
  for (const child of [...saved.contacts, ...saved.addresses, ...saved.tax_registrations]) {
    expect(child.id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7/);
    expect(child).not.toHaveProperty("formKey");
  }
});
