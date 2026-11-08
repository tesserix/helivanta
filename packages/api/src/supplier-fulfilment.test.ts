import { afterEach, expect, it, vi } from "vitest";
import { readFileSync } from "node:fs";
import {
  releaseHospitalSupplierReceipt,
  recoverHospitalSupplierRelease,
  listHospitalReceivingProductChoicesPage,
  listHospitalReceivingLocationsPage,
  listSupplierAllocationsPage,
  reserveSupplierAllocation,
  readSupplierAllocation,
  recoverSupplierAllocation,
  readHospitalSupplierReceipt,
  recoverHospitalSupplierReceipt,
  decideSupplierAllocation,
  recoverSupplierAllocationCommand,
  recordHospitalSupplierReceipt,
  correctHospitalSupplierReceipt,
  closeHospitalSupplierReceiving,
  recordHospitalSupplierClearance,
  listHospitalSupplierCorrectionsPage,
  listHospitalSupplierShipmentsPage,
  listHospitalSupplierResponsesPage,
  listSourceSupplierResponsesPage,
  recoverSourceSupplierResponse,
  listSupplierStockChoicesPage,
  listSupplierStockLocationsPage,
  supplierStockWorkflowKey,
  hospitalSupplierReceivingKey,
} from "./supplier-fulfilment";

const id = (n: number) => `0199e000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const source = {
  tenantId: id(1),
  subject: "TEST-operator",
  product: "supply" as const,
  organizationId: id(2),
  locationId: id(3),
};
const buyer = {
  tenantId: id(4),
  subject: "TEST-reviewer",
  hospitalId: id(5),
  departmentId: id(6),
  outletId: id(7),
  pharmacyId: id(8),
};
const plan = {
  id: id(9),
  owner_tenant_id: id(1),
  order_id: id(10),
  order_input_sha256: "a".repeat(64),
  line_quantity_units: 5,
  order_authority_epoch: 1,
  order_remote_epoch: 1,
  source_organization_id: id(2),
  source_location_id: id(3),
  buyer_tenant_id: id(4),
  hospital_id: id(5),
  buyer_organization_id: id(8),
  buyer_location_id: id(7),
  product: { owner: "supply" as const, product_id: id(20), version_id: id(21) },
  unit_code: "unit",
  input: {
    expected_order_version: 1,
    line_id: id(15),
    stock_id: id(11),
    quantity_units: 5,
    expires_at: "2026-11-08T12:00:00Z",
    reason: "TEST fulfilment",
  },
  actor_subject: "TEST-original",
  actor_staff_id: id(12),
  created_at: "2026-11-08T11:55:00Z",
};
const allocation = {
  plan,
  version: 1,
  status: "reserved",
  lot_number: "TEST-LOT",
  expires_on: "2027-11-08",
  reserved_units: 5,
  dispatched_units: 0,
  released_units: 0,
  product_description: "TEST sterile supply",
  pack_size: 1,
  updated_at: "2026-11-08T11:55:00Z",
};
const view = {
  allocation,
  effective_reserved_units: 5,
  effective_status: "reserved",
  observed_at: "2026-11-08T11:56:00Z",
};
const receipt = {
  id: id(13),
  owner_tenant_id: buyer.tenantId,
  shipment_id: id(14),
  order_id: id(10),
  hospital_id: buyer.hospitalId,
  pharmacy_id: buyer.pharmacyId,
  outlet_id: buyer.outletId,
  input: {
    counted_units: 5,
    accepted_units: 4,
    rejected_units: 1,
    observed_lot_number: "TEST-BATCH",
    observed_expires_on: "2027-11-08",
    identity_confirmed: true,
    reason: "TEST inspection",
  },
  cumulative_counted_units: 5,
  cumulative_accepted_units: 4,
  cumulative_rejected_units: 1,
  actor_subject: "TEST-original",
  actor_staff_id: id(12),
  created_at: "2026-11-08T12:00:00Z",
};
const registered = JSON.parse(readFileSync("../../docs/api/reference/inventory.json", "utf8")) as {
  modules: { routes: { method: string; path: string }[] }[];
};
afterEach(() => {
  try {
    if (vi.isMockFunction(globalThis.fetch))
      for (const [path, options] of vi.mocked(globalThis.fetch).mock.calls) {
        const route = new URL(String(path), "https://test.invalid").pathname.replace(/^\/api/, "");
        const method = options?.method ?? "GET";
        expect(
          registered.modules
            .flatMap((m) => m.routes)
            .some(
              (r) =>
                r.method === method &&
                new RegExp("^" + r.path.replace(/:[^/]+/g, "[^/]+") + "$").test(route),
            ),
          `${method} ${route} must exist in the registered Global API contract`,
        ).toBe(true);
      }
  } finally {
    vi.unstubAllGlobals();
  }
});
function reply(body: unknown) {
  const fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => body });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
it("routes allocation discovery to its actual stock owner and rejects foreign evidence", async () => {
  const fetch = reply({ data: [view], page: { has_more: false, next_cursor: null } });
  await expect(listSupplierAllocationsPage(source, id(10), { limit: 1 })).resolves.toMatchObject({
    data: [view],
  });
  expect(fetch).toHaveBeenCalledWith(
    expect.stringContaining(
      `/api/v1/supply/parties/${source.organizationId}/operating-locations/${source.locationId}/supplier-orders/${id(10)}/allocations?limit=1`,
    ),
    expect.objectContaining({ cache: "no-store" }),
  );
  reply({
    data: [
      { ...view, allocation: { ...allocation, plan: { ...plan, source_location_id: id(99) } } },
    ],
    page: { has_more: false, next_cursor: null },
  });
  await expect(listSupplierAllocationsPage(source, id(10))).rejects.toThrow();
});
it("allows a colleague to read current stock, but binds command recovery to original actor", async () => {
  reply(view);
  await expect(readSupplierAllocation(source, id(10), id(9))).resolves.toMatchObject(view);
  reply(allocation);
  await expect(
    recoverSupplierAllocation(source, id(10), id(9), allocation.plan.input),
  ).rejects.toThrow();
  reply(allocation);
  await expect(
    recoverSupplierAllocation(
      { ...source, subject: allocation.plan.actor_subject },
      id(10),
      id(9),
      allocation.plan.input,
    ),
  ).resolves.toMatchObject(allocation);
  reply({ ...allocation, plan: { ...plan, input: { ...plan.input, quantity_units: 4 } } });
  await expect(
    recoverSupplierAllocation(
      { ...source, subject: allocation.plan.actor_subject },
      id(10),
      id(9),
      allocation.plan.input,
    ),
  ).rejects.toThrow();
});
it("validates immutable reserve intent and both owners' routes", async () => {
  const scope = { ...source, product: "pharmacy" as const };
  const v = {
    ...allocation,
    plan: {
      ...plan,
      product: { owner: "pharmacy", product_id: id(20), version: 1 },
      actor_subject: source.subject,
    },
  };
  const fetch = reply(v);
  await reserveSupplierAllocation(scope, id(10), id(9), allocation.plan.input);
  expect(fetch).toHaveBeenCalledWith(
    expect.stringContaining(
      `/api/v1/pharmacy/pharmacies/${scope.organizationId}/outlets/${scope.locationId}/supplier-orders/${id(10)}/allocations/${id(9)}`,
    ),
    expect.objectContaining({ method: "PUT", body: JSON.stringify(allocation.plan.input) }),
  );
  reply(v);
  await expect(
    reserveSupplierAllocation(scope, id(10), id(9), {
      ...allocation.plan.input,
      quantity_units: 4,
    }),
  ).rejects.toThrow();
});
it("distinguishes hospital receipt disclosure from actor-bound command recovery", async () => {
  reply(receipt);
  await expect(readHospitalSupplierReceipt(buyer, id(14), id(13))).resolves.toMatchObject(receipt);
  reply(receipt);
  await expect(
    recoverHospitalSupplierReceipt(buyer, id(14), id(13), receipt.input),
  ).rejects.toThrow();
  reply(receipt);
  await expect(
    recoverHospitalSupplierReceipt(
      { ...buyer, subject: receipt.actor_subject },
      id(14),
      id(13),
      receipt.input,
    ),
  ).resolves.toMatchObject(receipt);
  reply({ ...receipt, outlet_id: id(99) });
  await expect(readHospitalSupplierReceipt(buyer, id(14), id(13))).rejects.toThrow();
});
it("rejects inconsistent pages and oversized or foreign cursors before network access", async () => {
  const fetch = reply({ data: [], page: { has_more: true, next_cursor: null } });
  await expect(listSupplierAllocationsPage(source, id(10))).rejects.toThrow();
  fetch.mockClear();
  await expect(listSupplierAllocationsPage(source, id(10), { limit: 51 })).rejects.toThrow();
  await expect(
    listSupplierAllocationsPage(source, id(10), { cursor: "x".repeat(4097) }),
  ).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});

it("uses hospital source governance without mistaking the recipient for source ownership", async () => {
  const scope = {
    ...source,
    product: "pharmacy" as const,
    hospitalId: id(90),
    departmentId: id(91),
  };
  const v = {
    ...view,
    allocation: {
      ...allocation,
      plan: { ...plan, product: { owner: "pharmacy", product_id: id(20), version: 1 } },
    },
  };
  const fetch = reply(v);
  await expect(readSupplierAllocation(scope, id(10), id(9))).resolves.toMatchObject(v);
  expect(fetch).toHaveBeenCalledWith(
    expect.stringContaining(
      `/pharmacy/hospitals/${id(90)}/departments/${id(91)}/pharmacies/${id(2)}/outlets/${id(3)}/`,
    ),
    expect.anything(),
  );
  fetch.mockClear();
  await expect(
    readSupplierAllocation({ ...scope, product: "supply" }, id(10), id(9)),
  ).rejects.toThrow();
  await expect(
    readSupplierAllocation({ ...scope, departmentId: undefined }, id(10), id(9)),
  ).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});
it("rejects a current allocation returned as original reservation recovery", async () => {
  reply({ ...allocation, version: 2, reserved_units: 4, dispatched_units: 1 });
  await expect(
    recoverSupplierAllocation(
      { ...source, subject: plan.actor_subject },
      id(10),
      id(9),
      plan.input,
    ),
  ).rejects.toThrow();
});
it("rejects a false effective expiry or duplicate allocation page", async () => {
  reply({ ...view, observed_at: "2026-11-08T12:01:00Z" });
  await expect(readSupplierAllocation(source, id(10), id(9))).rejects.toThrow();
  reply({ data: [view, view], page: { has_more: false, next_cursor: null } });
  await expect(listSupplierAllocationsPage(source, id(10))).rejects.toThrow();
});

it("matches immutable dispatch and release commands rather than latest allocation state", async () => {
  const input = {
    expected_version: 1,
    quantity_units: 2,
    decision: "dispatch" as const,
    handoff_reference: "TEST handoff",
    reason: "TEST dispatch",
  };
  const command = {
    command_id: id(30),
    input,
    allocation: { ...allocation, version: 2, reserved_units: 3, dispatched_units: 2 },
    actor_subject: source.subject,
    actor_staff_id: id(12),
    recorded_at: "2026-11-08T11:58:00Z",
  };
  const fetch = reply(command);
  await expect(
    decideSupplierAllocation(source, id(10), id(9), id(30), input),
  ).resolves.toMatchObject(command);
  expect(fetch).toHaveBeenCalledWith(
    expect.stringContaining(`/commands/${id(30)}`),
    expect.objectContaining({ method: "PUT" }),
  );
  reply(command);
  await expect(
    recoverSupplierAllocationCommand(source, id(10), id(9), id(30), input),
  ).resolves.toMatchObject(command);
  reply({ ...command, allocation: { ...command.allocation, version: 3 } });
  await expect(
    recoverSupplierAllocationCommand(source, id(10), id(9), id(30), input),
  ).rejects.toThrow();
  reply(command);
  await expect(
    recoverSupplierAllocationCommand(source, id(10), id(9), id(30), {
      ...input,
      decision: "release",
    }),
  ).rejects.toThrow();
  reply({ ...command, actor_subject: "TEST-other" });
  await expect(decideSupplierAllocation(source, id(10), id(9), id(30), input)).rejects.toThrow();
});
it("records only the frozen receiving counts in the selected hospital outlet", async () => {
  const v = { ...receipt, actor_subject: buyer.subject };
  const fetch = reply(v);
  await expect(
    recordHospitalSupplierReceipt(buyer, id(14), id(13), receipt.input),
  ).resolves.toMatchObject(v);
  expect(fetch).toHaveBeenCalledWith(
    expect.stringContaining(
      `/pharmacy/hospitals/${buyer.hospitalId}/departments/${buyer.departmentId}/outlets/${buyer.outletId}/supplier-shipments/${id(14)}/receipts/${id(13)}`,
    ),
    expect.objectContaining({ method: "PUT", cache: "no-store" }),
  );
  fetch.mockClear();
  await expect(
    recordHospitalSupplierReceipt(buyer, id(14), id(13), { ...receipt.input, accepted_units: 6 }),
  ).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
  reply({ ...v, input: { ...receipt.input, accepted_units: 3, rejected_units: 2 } });
  await expect(
    recordHospitalSupplierReceipt(buyer, id(14), id(13), receipt.input),
  ).rejects.toThrow();
});
const correctionInput = {
  expected_summary_version: 1,
  expected_receipt_version: 1,
  counted_units: 5,
  accepted_units: 3,
  rejected_units: 2,
  identity_confirmed: true,
  reason: "TEST damaged pack",
};
const correction = {
  id: id(31),
  owner_tenant_id: buyer.tenantId,
  shipment_id: id(14),
  receipt_id: receipt.id,
  outlet_id: buyer.outletId,
  version: 2,
  input: correctionInput,
  accepted_delta_units: -1,
  cumulative_counted_units: 5,
  cumulative_accepted_units: 3,
  cumulative_rejected_units: 2,
  actor_subject: buyer.subject,
  actor_staff_id: id(32),
  created_at: "2026-11-08T12:05:00Z",
};
it("keeps correction history separate from original receiving and rejects foreign corrections", async () => {
  reply(correction);
  await expect(
    correctHospitalSupplierReceipt(buyer, id(14), receipt.id, id(31), correctionInput),
  ).resolves.toMatchObject(correction);
  reply({
    data: [{ ...correction, actor_subject: "TEST-other-reviewer" }],
    page: { has_more: false, next_cursor: null },
  });
  await expect(
    listHospitalSupplierCorrectionsPage(buyer, id(14), receipt.id),
  ).resolves.toMatchObject({ data: [{ id: id(31) }] });
  reply({
    data: [{ ...correction, receipt_id: id(99) }],
    page: { has_more: false, next_cursor: null },
  });
  await expect(listHospitalSupplierCorrectionsPage(buyer, id(14), receipt.id)).rejects.toThrow();
  reply({ ...correction, input: { ...correctionInput, expected_summary_version: 2 } });
  await expect(
    correctHospitalSupplierReceipt(buyer, id(14), receipt.id, id(31), correctionInput),
  ).rejects.toThrow();
});
it("validates closure and clearance receipts without labelling a review as stock release", async () => {
  const input = {
    expected_counted_units: 5,
    expected_summary_version: 2,
    reason: "TEST delivery checked",
  };
  const closure = {
    id: id(33),
    owner_tenant_id: buyer.tenantId,
    shipment_id: id(14),
    counted_units: 5,
    accepted_units: 3,
    rejected_units: 2,
    short_units: 0,
    reason: input.reason,
    actor_subject: buyer.subject,
    actor_staff_id: id(12),
    created_at: "2026-11-08T12:06:00Z",
  };
  reply(closure);
  await expect(closeHospitalSupplierReceiving(buyer, id(14), id(33), input)).resolves.toMatchObject(
    closure,
  );
  reply({ ...closure, counted_units: 4 });
  await expect(closeHospitalSupplierReceiving(buyer, id(14), id(33), input)).rejects.toThrow();
  const clearance = {
    id: id(34),
    receipt_id: receipt.id,
    summary_version: 2,
    state: "review_recorded",
  };
  const fetch = reply(clearance);
  await expect(
    recordHospitalSupplierClearance(buyer, id(14), receipt.id, id(34), {
      expected_summary_version: 2,
      reason: "TEST review",
    }),
  ).resolves.toMatchObject(clearance);
  expect(fetch).toHaveBeenCalledWith(
    expect.stringContaining(`/receipts/${receipt.id}/clearances/${id(34)}`),
    expect.objectContaining({ method: "PUT" }),
  );
  reply({ ...clearance, summary_version: 3 });
  await expect(
    recordHospitalSupplierClearance(buyer, id(14), receipt.id, id(34), {
      expected_summary_version: 2,
      reason: "TEST review",
    }),
  ).rejects.toThrow();
});
it("does not expose a delivery from another recipient in the hospital worklist", async () => {
  const shipment = {
    id: id(14),
    allocation_id: plan.id,
    order_id: plan.order_id,
    line_id: plan.input.line_id,
    order_input_sha256: plan.order_input_sha256,
    source_tenant_id: source.tenantId,
    source_organization_id: source.organizationId,
    source_location_id: source.locationId,
    buyer_tenant_id: buyer.tenantId,
    hospital_id: buyer.hospitalId,
    buyer_organization_id: buyer.pharmacyId,
    buyer_location_id: buyer.outletId,
    product: plan.product,
    unit_code: plan.unit_code,
    quantity_units: 5,
    lot_number: allocation.lot_number,
    expires_on: allocation.expires_on,
    handoff_reference: "TEST handoff",
    actor_subject: source.subject,
    product_description: allocation.product_description,
    pack_size: 1,
    dispatched_at: "2026-11-08T11:58:00Z",
  };
  reply({ data: [shipment], page: { has_more: false, next_cursor: null } });
  await expect(listHospitalSupplierShipmentsPage(buyer)).resolves.toMatchObject({
    data: [shipment],
  });
  reply({
    data: [{ ...shipment, buyer_location_id: id(99) }],
    page: { has_more: false, next_cursor: null },
  });
  await expect(listHospitalSupplierShipmentsPage(buyer)).rejects.toThrow();
});

it("separates cached workflow data across accounts, departments and physical stock owners", () => {
  for (const change of [
    { tenantId: id(90) },
    { subject: "TEST-other" },
    { organizationId: id(90) },
    { locationId: id(90) },
    { product: "pharmacy" as const },
  ])
    expect(supplierStockWorkflowKey({ ...source, ...change }, "orders")).not.toEqual(
      supplierStockWorkflowKey(source, "orders"),
    );
  for (const change of [
    { tenantId: id(90) },
    { subject: "TEST-other" },
    { hospitalId: id(90) },
    { departmentId: id(90) },
    { pharmacyId: id(90) },
    { outletId: id(90) },
  ])
    expect(hospitalSupplierReceivingKey({ ...buyer, ...change }, "shipments")).not.toEqual(
      hospitalSupplierReceivingKey(buyer, "shipments"),
    );
  expect(
    supplierStockWorkflowKey(
      { ...source, product: "pharmacy", hospitalId: id(90), departmentId: id(91) },
      "orders",
    ),
  ).not.toEqual(
    supplierStockWorkflowKey(
      { ...source, product: "pharmacy", hospitalId: id(90), departmentId: id(92) },
      "orders",
    ),
  );
});

it("normalizes frozen reservation expiry to the stock owner's microsecond precision", async () => {
  const input = { ...plan.input, expires_at: "2026-11-08T12:00:00.123456789Z" };
  const stored = {
    ...allocation,
    plan: {
      ...plan,
      actor_subject: source.subject,
      input: { ...input, expires_at: "2026-11-08T12:00:00.123456Z" },
    },
  };
  const fetch = reply(stored);
  await expect(reserveSupplierAllocation(source, id(10), id(9), input)).resolves.toMatchObject(
    stored,
  );
  expect(
    JSON.parse(String((vi.mocked(fetch).mock.calls[0]?.[1] as RequestInit).body)).expires_at,
  ).toBe(stored.plan.input.expires_at);
  reply(stored);
  await expect(recoverSupplierAllocation(source, id(10), id(9), input)).resolves.toMatchObject(
    stored,
  );
});

it("selects current physical stock for the exact owner order line without inventory admin routes", async () => {
  const line = {
    id: plan.input.line_id,
    product: plan.product,
    quantity_units: plan.line_quantity_units,
    unit_code: plan.unit_code,
  };
  const choice = {
    id: plan.input.stock_id,
    owner_tenant_id: source.tenantId,
    order_id: plan.order_id,
    line_id: line.id,
    organization_id: source.organizationId,
    location_id: source.locationId,
    product: plan.product,
    lot_number: allocation.lot_number,
    expires_on: allocation.expires_on,
    description: allocation.product_description,
    unit_code: line.unit_code,
    pack_size: 1,
    available_units: 100,
    created_at: plan.created_at,
    observed_at: view.observed_at,
  };
  const fetch = reply({ data: [choice], page: { has_more: false, next_cursor: null } });
  await expect(listSupplierStockChoicesPage(source, plan.order_id, line)).resolves.toMatchObject({
    data: [choice],
  });
  expect(fetch).toHaveBeenCalledWith(
    expect.stringContaining(`/supplier-orders/${plan.order_id}/lines/${line.id}/stock-choices?`),
    expect.objectContaining({ cache: "no-store" }),
  );
  reply({ data: [{ ...choice, line_id: id(99) }], page: { has_more: false, next_cursor: null } });
  await expect(listSupplierStockChoicesPage(source, plan.order_id, line)).rejects.toThrow();
  reply({
    data: [{ ...choice, product: { ...plan.product, version_id: id(99) } }],
    page: { has_more: false, next_cursor: null },
  });
  await expect(listSupplierStockChoicesPage(source, plan.order_id, line)).rejects.toThrow();
});
it("uses a separate sellable shelf page only for the pharmacy stock owner", async () => {
  const scope = { ...source, product: "pharmacy" as const },
    line = {
      id: plan.input.line_id,
      product: { owner: "pharmacy" as const, product_id: id(20), version: 1 },
      quantity_units: 100,
      unit_code: "capsule",
    };
  const location = {
    id: id(40),
    owner_tenant_id: scope.tenantId,
    order_id: plan.order_id,
    line_id: line.id,
    organization_id: scope.organizationId,
    location_id: scope.locationId,
    stock_id: plan.input.stock_id,
    code: "TEST-SHELF",
    name: "TEST sellable shelf",
    available_units: 40,
    created_at: plan.created_at,
    observed_at: view.observed_at,
  };
  const fetch = reply({ data: [location], page: { has_more: false, next_cursor: null } });
  await expect(
    listSupplierStockLocationsPage(scope, plan.order_id, line, location.stock_id),
  ).resolves.toMatchObject({ data: [location] });
  expect(fetch).toHaveBeenCalledWith(
    expect.stringContaining(`/stock-choices/${location.stock_id}/locations?`),
    expect.objectContaining({ cache: "no-store" }),
  );
  reply({
    data: [{ ...location, stock_id: id(99) }],
    page: { has_more: false, next_cursor: null },
  });
  await expect(
    listSupplierStockLocationsPage(scope, plan.order_id, line, location.stock_id),
  ).rejects.toThrow();
  fetch.mockClear();
  await expect(
    listSupplierStockLocationsPage(source, plan.order_id, line, location.stock_id),
  ).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});

it("keeps a supplier's response separate from hospital counts and checks both response scopes", async () => {
  const shipment = {
    id: id(14),
    allocation_id: plan.id,
    order_id: plan.order_id,
    line_id: plan.input.line_id,
    order_input_sha256: plan.order_input_sha256,
    source_tenant_id: source.tenantId,
    source_organization_id: source.organizationId,
    source_location_id: source.locationId,
    buyer_tenant_id: buyer.tenantId,
    hospital_id: buyer.hospitalId,
    buyer_organization_id: buyer.pharmacyId,
    buyer_location_id: buyer.outletId,
    product: plan.product,
    unit_code: plan.unit_code,
    quantity_units: 5,
    lot_number: allocation.lot_number,
    expires_on: allocation.expires_on,
    handoff_reference: "TEST handoff",
    actor_subject: source.subject,
    product_description: allocation.product_description,
    pack_size: 1,
    dispatched_at: "2026-11-08T11:58:00Z",
  };
  const response = {
    id: id(80),
    owner_tenant_id: source.tenantId,
    buyer_tenant_id: buyer.tenantId,
    order_id: plan.order_id,
    shipment_id: shipment.id,
    source_product: source.product,
    source_organization_id: source.organizationId,
    source_location_id: source.locationId,
    summary_version: 2,
    response: "disputed",
    reason: "TEST review shortage",
    actor_subject: source.subject,
    actor_staff_id: id(12),
    created_at: "2026-11-09T01:00:00Z",
  };
  const shared = Object.fromEntries(
    Object.entries(response).filter(([key]) => key !== "actor_staff_id"),
  );
  reply({ data: [response], page: { has_more: false, next_cursor: null } });
  await expect(listHospitalSupplierResponsesPage(buyer, shipment)).rejects.toThrow();
  reply({ data: [shared], page: { has_more: false, next_cursor: null } });
  await expect(listHospitalSupplierResponsesPage(buyer, shipment)).resolves.toMatchObject({
    data: [shared],
  });
  reply({ data: [response], page: { has_more: false, next_cursor: null } });
  await expect(listSourceSupplierResponsesPage(source, shipment)).resolves.toMatchObject({
    data: [response],
  });
  reply({
    data: [{ ...response, owner_tenant_id: id(99) }],
    page: { has_more: false, next_cursor: null },
  });
  await expect(listHospitalSupplierResponsesPage(buyer, shipment)).rejects.toThrow();
  reply({
    data: [{ ...response, shipment_id: id(99) }],
    page: { has_more: false, next_cursor: null },
  });
  await expect(listSourceSupplierResponsesPage(source, shipment)).rejects.toThrow();
  reply(response);
  await expect(
    recoverSourceSupplierResponse(source, shipment, response.id, {
      expected_summary_version: 2,
      response: "disputed",
      reason: response.reason,
    }),
  ).resolves.toMatchObject(response);
  reply(response);
  await expect(
    recoverSourceSupplierResponse({ ...source, subject: "TEST-other" }, shipment, response.id, {
      expected_summary_version: 2,
      response: "disputed",
      reason: response.reason,
    }),
  ).rejects.toThrow();
});

it("binds receiving choices to the exact shipment and fixed stock status", async () => {
  const common = {
    id: id(30),
    owner_tenant_id: buyer.tenantId,
    shipment_id: id(14),
    pharmacy_id: buyer.pharmacyId,
    outlet_id: buyer.outletId,
    created_at: "2026-11-08T12:00:00Z",
  };
  const product = {
    ...common,
    version: 1,
    brand_name: "TEST brand",
    generic_name: "TEST generic",
    manufacturer: "TEST maker",
    dosage_form: "capsule",
    unit_code: "capsule",
    pack_size: 10,
  };
  const fetch = reply({ data: [product], page: { has_more: false, next_cursor: null } });
  await expect(
    listHospitalReceivingProductChoicesPage(buyer, id(14), { limit: 1 }),
  ).resolves.toMatchObject({ data: [product] });
  expect(fetch).toHaveBeenCalledWith(
    expect.stringContaining(`/supplier-shipments/${id(14)}/receiving/product-choices?limit=1`),
    expect.anything(),
  );
  for (const field of ["owner_tenant_id", "shipment_id", "pharmacy_id", "outlet_id"]) {
    reply({
      data: [{ ...product, [field]: id(99) }],
      page: { has_more: false, next_cursor: null },
    });
    await expect(listHospitalReceivingProductChoicesPage(buyer, id(14))).rejects.toThrow();
  }
  for (const status of ["quarantine", "sellable"] as const) {
    const location = { ...common, code: "TEST-Q", name: "TEST location", stock_status: status };
    const request = reply({ data: [location], page: { has_more: false, next_cursor: null } });
    await expect(
      listHospitalReceivingLocationsPage(buyer, id(14), status, { limit: 1 }),
    ).resolves.toMatchObject({ data: [location] });
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining(`/receiving/${status}-locations?limit=1`),
      expect.anything(),
    );
    reply({
      data: [{ ...location, stock_status: status === "sellable" ? "quarantine" : "sellable" }],
      page: { has_more: false, next_cursor: null },
    });
    await expect(listHospitalReceivingLocationsPage(buyer, id(14), status)).rejects.toThrow();
  }
});

it("binds physical release and original recovery to its receipt, actor and frozen input", async () => {
  const input = {
    expected_summary_version: 4,
    to_location_id: id(92),
    quantity_units: 3,
    reason: "TEST checked and shelved",
  };
  const result = {
    id: id(90),
    owner_tenant_id: buyer.tenantId,
    shipment_id: receipt.shipment_id,
    receipt_id: receipt.id,
    pharmacy_id: buyer.pharmacyId,
    outlet_id: buyer.outletId,
    batch_id: id(91),
    from_location_id: id(93),
    input,
    actor_subject: buyer.subject,
    actor_staff_id: id(12),
    created_at: "2026-11-09T01:00:00Z",
  };
  const fetch = reply(result);
  await expect(
    releaseHospitalSupplierReceipt(buyer, receipt.shipment_id, receipt.id, result.id, input),
  ).resolves.toEqual(result);
  expect(fetch).toHaveBeenCalledWith(
    expect.stringContaining(`/receipts/${receipt.id}/releases/${result.id}`),
    expect.objectContaining({ method: "PUT", body: JSON.stringify(input) }),
  );
  reply(result);
  await expect(
    recoverHospitalSupplierRelease(buyer, receipt.shipment_id, receipt.id, result.id, input),
  ).resolves.toEqual(result);
  for (const field of [
    "owner_tenant_id",
    "shipment_id",
    "receipt_id",
    "pharmacy_id",
    "outlet_id",
  ]) {
    reply({ ...result, [field]: id(99) });
    await expect(
      releaseHospitalSupplierReceipt(buyer, receipt.shipment_id, receipt.id, result.id, input),
    ).rejects.toThrow();
  }
  reply({ ...result, actor_subject: "TEST-other" });
  await expect(
    recoverHospitalSupplierRelease(buyer, receipt.shipment_id, receipt.id, result.id, input),
  ).rejects.toThrow();
  reply({ ...result, input: { ...input, quantity_units: 2 } });
  await expect(
    recoverHospitalSupplierRelease(buyer, receipt.shipment_id, receipt.id, result.id, input),
  ).rejects.toThrow();
});
