import { useState } from "react";
import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider, refusedDefault } from "@helivanta/ui";
import { chooseOption } from "../../test/select";
import { SourceFulfilment } from "./source-fulfilment";
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn(), replace: vi.fn() }) }));
const id = (n: number) => `0199e000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const scope = {
  tenantId: id(1),
  subject: "TEST-source",
  product: "supply" as const,
  organizationId: id(2),
  locationId: id(3),
};
const product = { owner: "supply", product_id: id(4), version_id: id(5) };
const endpoint = (tenant: string, product: string, organization: string, location: string) => ({
  tenant_id: tenant,
  product,
  organization_id: organization,
  location_id: location,
  organization_version: 1,
  location_version: 1,
  licence_id: id(6),
  licence_version: 1,
  jurisdiction: "IN-MH",
});
const terms = {
  seller: endpoint(scope.tenantId, "supply", scope.organizationId, scope.locationId),
  buyer: {
    ...endpoint(id(7), "pharmacy", id(8), id(9)),
    hospital_id: id(10),
    binding_id: id(11),
    binding_version: 1,
    registration_id: id(12),
    registration_version: 1,
  },
  category: "otc",
  currency: "INR",
  valid_from: "2026-01-01T00:00:00Z",
  expires_at: "2030-01-01T00:00:00Z",
  terms_reference: "TEST terms",
};
const line = { id: id(13), product, quantity_units: 100, unit_code: "capsule" };
const order = {
  id: id(14),
  owner_tenant_id: scope.tenantId,
  side: "seller",
  terms,
  input: {
    relationship_id: id(15),
    terms_sha256: "a".repeat(64),
    purchase_order_reference: "TEST-PO",
    quote_reference: "TEST quote",
    expires_at: "2030-01-01T00:00:00Z",
    reason: "TEST procurement",
    lines: [line],
  },
  input_sha256: "b".repeat(64),
  status: "accepted",
  version: 2,
  authority_epoch: 1,
  remote_epoch: 1,
  remote_phase: "approved",
  prepared_by: "TEST-buyer",
  prepared_staff_id: id(16),
  created_at: "2026-11-08T11:00:00Z",
  updated_at: "2026-11-08T11:00:00Z",
};
const choice = {
  id: id(17),
  owner_tenant_id: scope.tenantId,
  order_id: order.id,
  line_id: line.id,
  organization_id: scope.organizationId,
  location_id: scope.locationId,
  product,
  lot_number: "TEST-LOT",
  expires_on: "2030-12-31",
  description: "TEST released capsule",
  unit_code: "capsule",
  pack_size: 10,
  available_units: 100,
  created_at: order.created_at,
  observed_at: order.updated_at,
};
const page = (data: unknown[]) =>
  Response.json({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
it("recovers a lost reservation response with the frozen owner command", async () => {
  let original: unknown = null,
    writes = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes++;
        const input = JSON.parse(String(init.body));
        original = {
          plan: {
            id: url.split("/").at(-1),
            owner_tenant_id: scope.tenantId,
            order_id: order.id,
            order_input_sha256: order.input_sha256,
            line_quantity_units: 100,
            order_authority_epoch: 1,
            order_remote_epoch: 1,
            source_organization_id: scope.organizationId,
            source_location_id: scope.locationId,
            buyer_tenant_id: terms.buyer.tenant_id,
            hospital_id: terms.buyer.hospital_id,
            buyer_organization_id: terms.buyer.organization_id,
            buyer_location_id: terms.buyer.location_id,
            product,
            unit_code: "capsule",
            input,
            actor_subject: scope.subject,
            actor_staff_id: id(18),
            created_at: new Date().toISOString(),
          },
          version: 1,
          status: "reserved",
          lot_number: choice.lot_number,
          expires_on: choice.expires_on,
          reserved_units: input.quantity_units,
          dispatched_units: 0,
          released_units: 0,
          product_description: choice.description,
          pack_size: 10,
          updated_at: new Date().toISOString(),
        };
        throw new TypeError("TEST response lost after commit");
      }
      if (url.endsWith("/creation-receipt")) return Response.json(original);
      if (url.includes("/stock-choices?")) return page([choice]);
      if (url.includes("/allocations?")) return page([]);
      if (url.includes("/supplier-orders?")) return page([order]);
      if (url.endsWith(`/supplier-orders/${order.id}`)) return Response.json(order);
      return Response.json({ error: "not_found", message: "TEST missing route" }, { status: 404 });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <SourceFulfilment scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open TEST-PO" }));
  await chooseOption(
    user,
    await screen.findByRole("combobox", { name: "Physical lot" }),
    /TEST-LOT/,
  );
  await user.clear(screen.getByRole("spinbutton", { name: "Units to reserve" }));
  await user.type(screen.getByRole("spinbutton", { name: "Units to reserve" }), "50");
  await user.type(
    screen.getByRole("textbox", { name: "Reservation reason" }),
    "TEST counted for delivery",
  );
  await user.dblClick(screen.getByRole("button", { name: "Reserve stock" }));
  await screen.findByText(/TEST response lost after commit/);
  expect(screen.getByRole("spinbutton", { name: "Units to reserve" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Check recorded result" }));
  await screen.findByText("Original reservation record verified.");
  expect(writes).toBe(1);
});
it("hides the prior location's work when a new scope cannot be verified", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url.includes(scope.locationId)
        ? page([order])
        : Response.json(
            { error: "forbidden", message: "TEST current access ended" },
            { status: 403 },
          ),
    ),
  );
  function SwitchingWorkspace() {
    const [location, setLocation] = useState(scope.locationId);
    return (
      <>
        <button onClick={() => setLocation(id(99))}>Switch location</button>
        <SourceFulfilment scope={{ ...scope, locationId: location }} />
      </>
    );
  }
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <SwitchingWorkspace />
    </NavigationGuardProvider>,
  );
  await screen.findByRole("button", { name: "Open TEST-PO" });
  await user.click(screen.getByRole("button", { name: "Switch location" }));
  await screen.findByText(refusedDefault);
  await waitFor(() =>
    expect(screen.queryByRole("button", { name: "Open TEST-PO" })).not.toBeInTheDocument(),
  );
});

it("recovers a partial dispatch as its original command rather than sending another stock movement", async () => {
  const allocation = {
    plan: {
      id: id(25),
      owner_tenant_id: scope.tenantId,
      order_id: order.id,
      order_input_sha256: order.input_sha256,
      line_quantity_units: 100,
      order_authority_epoch: 1,
      order_remote_epoch: 1,
      source_organization_id: scope.organizationId,
      source_location_id: scope.locationId,
      buyer_tenant_id: terms.buyer.tenant_id,
      hospital_id: terms.buyer.hospital_id,
      buyer_organization_id: terms.buyer.organization_id,
      buyer_location_id: terms.buyer.location_id,
      product,
      unit_code: "capsule",
      input: {
        expected_order_version: 2,
        line_id: line.id,
        stock_id: choice.id,
        quantity_units: 60,
        expires_at: new Date(Date.now() + 300_000).toISOString(),
        reason: "TEST reserved stock",
      },
      actor_subject: "TEST-original-picker",
      actor_staff_id: id(26),
      created_at: new Date().toISOString(),
    },
    version: 1,
    status: "reserved",
    lot_number: choice.lot_number,
    expires_on: choice.expires_on,
    reserved_units: 60,
    dispatched_units: 0,
    released_units: 0,
    product_description: choice.description,
    pack_size: 10,
    updated_at: new Date().toISOString(),
  };
  let writes = 0,
    receipt: unknown = null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes++;
        const input = JSON.parse(String(init.body));
        receipt = {
          command_id: url.split("/").at(-1),
          input,
          allocation: { ...allocation, version: 2, reserved_units: 30, dispatched_units: 30 },
          actor_subject: scope.subject,
          actor_staff_id: id(18),
          recorded_at: new Date().toISOString(),
        };
        throw new TypeError("TEST dispatch response lost");
      }
      if (/\/commands\/[^/?]+$/.test(url)) return Response.json(receipt);
      if (url.includes("/allocations?"))
        return page([
          {
            allocation,
            effective_status: "reserved",
            effective_reserved_units: 60,
            observed_at: new Date().toISOString(),
          },
        ]);
      if (url.includes("/stock-choices?")) return page([choice]);
      if (url.includes("/supplier-orders?")) return page([order]);
      if (url.endsWith(`/supplier-orders/${order.id}`)) return Response.json(order);
      return Response.json({ error: "not_found", message: "TEST missing route" }, { status: 404 });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <SourceFulfilment scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open TEST-PO" }));
  const quantity = await screen.findByRole("spinbutton", { name: "Units to move" });
  await user.clear(quantity);
  await user.type(quantity, "30");
  await user.type(screen.getByRole("textbox", { name: "Handoff reference" }), "TEST dispatch note");
  await user.type(
    screen.getByRole("textbox", { name: "Allocation reason" }),
    "TEST first delivery",
  );
  await user.click(screen.getByRole("button", { name: "Dispatch reserved stock" }));
  await screen.findByText("TEST dispatch response lost");
  expect(quantity).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Check recorded result" }));
  await screen.findByText("Original allocation decision verified.");
  expect(writes).toBe(1);
});

it("recovers independent source acceptance before enabling physical fulfilment", async () => {
  const incoming = { ...order, status: "incoming", version: 1 };
  let writes = 0,
    receipt: unknown = null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes++;
        receipt = {
          command_id: url.split("/").at(-1),
          input: JSON.parse(String(init.body)),
          order,
          actor_subject: scope.subject,
          actor_staff_id: id(18),
          recorded_at: new Date().toISOString(),
        };
        throw new TypeError("TEST acceptance response lost");
      }
      if (url.endsWith("/review-access"))
        return Response.json({
          owner_tenant_id: scope.tenantId,
          order_id: order.id,
          organization_id: scope.organizationId,
          location_id: scope.locationId,
          product: scope.product,
          actor_subject: scope.subject,
          actor_staff_id: id(18),
          observed_at: new Date().toISOString(),
        });
      if (/\/commands\/[^/?]+$/.test(url)) return Response.json(receipt);
      if (url.includes("/supplier-orders?")) return page([incoming]);
      if (url.endsWith(`/supplier-orders/${order.id}`)) return Response.json(incoming);
      if (url.includes("/allocations?")) return page([]);
      return Response.json({ error: "not_found", message: "TEST missing route" }, { status: 404 });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <SourceFulfilment scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open TEST-PO" }));
  await user.type(
    await screen.findByRole("textbox", { name: "Order review reason" }),
    "TEST agreed delivery",
  );
  await user.click(screen.getByRole("button", { name: "Accept source order" }));
  await screen.findByText("TEST acceptance response lost");
  expect(screen.queryByRole("button", { name: "Reserve stock" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Check recorded result" }));
  await screen.findByText("Original source review verified.");
  expect(writes).toBe(1);
});

it("lets staff edit a reservation rejected before execution without claiming stock was reserved", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT")
        return Response.json(
          { error: "invalid_request", message: "TEST invalid reservation reason" },
          { status: 400 },
        );
      if (url.includes("/stock-choices?")) return page([choice]);
      if (url.includes("/allocations?")) return page([]);
      if (url.includes("/supplier-orders?")) return page([order]);
      return Response.json(order);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <SourceFulfilment scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open TEST-PO" }));
  await chooseOption(
    user,
    await screen.findByRole("combobox", { name: "Physical lot" }),
    /TEST-LOT/,
  );
  await user.type(
    screen.getByRole("textbox", { name: "Reservation reason" }),
    "TEST rejected draft",
  );
  await user.click(screen.getByRole("button", { name: "Reserve stock" }));
  await screen.findByText("TEST invalid reservation reason");
  expect(screen.getByRole("textbox", { name: "Reservation reason" })).toBeEnabled();
  expect(screen.getByRole("button", { name: "Reserve stock" })).toBeEnabled();
  expect(screen.queryByText("Original reservation record verified.")).not.toBeInTheDocument();
});

it("does not offer source acceptance when current review authority is denied", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("/review-access"))
        return Response.json(
          { error: "forbidden", message: "TEST source reader only" },
          { status: 403 },
        );
      if (url.includes("/supplier-orders?"))
        return page([{ ...order, status: "incoming", version: 1 }]);
      if (url.endsWith(`/supplier-orders/${order.id}`))
        return Response.json({ ...order, status: "incoming", version: 1 });
      return page([]);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <SourceFulfilment scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open TEST-PO" }));
  await screen.findByText("Source order review is not available for this account or scope.");
  expect(screen.queryByRole("button", { name: "Accept source order" })).not.toBeInTheDocument();
});

it("keeps the original intent if a retry is rejected after an uncertain first response", async () => {
  const attempts: { url: string; body: string }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        attempts.push({ url, body: String(init.body) });
        if (attempts.length === 1) throw new TypeError("TEST first outcome unknown");
        return Response.json(
          { error: "invalid_request", message: "TEST retry rejected" },
          { status: 400 },
        );
      }
      if (url.includes("/stock-choices?")) return page([choice]);
      if (url.includes("/allocations?")) return page([]);
      if (url.includes("/supplier-orders?")) return page([order]);
      return Response.json(order);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <SourceFulfilment scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open TEST-PO" }));
  await chooseOption(
    user,
    await screen.findByRole("combobox", { name: "Physical lot" }),
    /TEST-LOT/,
  );
  await user.type(
    screen.getByRole("textbox", { name: "Reservation reason" }),
    "TEST frozen intent",
  );
  await user.click(screen.getByRole("button", { name: "Reserve stock" }));
  await screen.findByText("TEST first outcome unknown");
  await user.click(screen.getByRole("button", { name: "Retry unchanged request" }));
  await screen.findByText("TEST retry rejected");
  expect(screen.getByRole("textbox", { name: "Reservation reason" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Check recorded result" })).toBeEnabled();
  expect(attempts).toHaveLength(2);
  expect(attempts[1]).toEqual(attempts[0]);
});
it("refreshes a first owner stock conflict before allowing an edited reservation", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT")
        return Response.json(
          { error: "fulfilment_changed", message: "TEST current stock changed" },
          { status: 409 },
        );
      if (url.includes("/stock-choices?")) return page([choice]);
      if (url.includes("/allocations?")) return page([]);
      if (url.includes("/supplier-orders?")) return page([order]);
      return Response.json(order);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <SourceFulfilment scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open TEST-PO" }));
  await chooseOption(
    user,
    await screen.findByRole("combobox", { name: "Physical lot" }),
    /TEST-LOT/,
  );
  await user.type(
    screen.getByRole("textbox", { name: "Reservation reason" }),
    "TEST current count",
  );
  await user.click(screen.getByRole("button", { name: "Reserve stock" }));
  await screen.findByText("TEST current stock changed");
  await waitFor(() => expect(screen.getByRole("button", { name: "Reserve stock" })).toBeEnabled());
  expect(screen.queryByText("Original reservation record verified.")).not.toBeInTheDocument();
});
