import { afterEach, expect, it, vi } from "vitest";
import {
  prepareSupplierOrder,
  recoverSupplierOrderProposal,
  decideSupplierOrder,
  recoverSupplierOrderCommand,
  getSourceSupplierOrder,
  getSourceSupplierReviewAccess,
  decideSourceSupplierOrder,
  recoverSourceSupplierOrderCommand,
} from "./supplier-orders";
const id = "019a55c0-1111-7111-8111-111111111111";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-buyer",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  departmentId: "019a55c0-1111-7111-8111-111111111112",
  outletId: "019a55c0-1111-7111-8111-111111111113",
};
const endpoint = (tenant: string, product: string) => ({
  tenant_id: tenant,
  product,
  organization_id: id,
  organization_version: 1,
  location_id: scope.outletId,
  location_version: 1,
  licence_id: id,
  licence_version: 1,
  jurisdiction: "IN-MH",
});
const terms = {
  seller: endpoint("33333333-3333-4333-8333-333333333333", "supply"),
  buyer: {
    ...endpoint(scope.tenantId, "pharmacy"),
    hospital_id: scope.hospitalId,
    binding_id: id,
    binding_version: 2,
    registration_id: id,
    registration_version: 2,
  },
  category: "prescription",
  currency: "INR",
  terms_reference: "TEST terms",
  valid_from: "2026-11-01T00:00:00Z",
  expires_at: "2026-12-01T00:00:00Z",
};
const input = {
  relationship_id: id,
  terms_sha256: "a".repeat(64),
  purchase_order_reference: "TEST-PO",
  quote_reference: "TEST quote",
  expires_at: "2026-11-30T00:00:00Z",
  reason: "TEST shared procurement instruction",
  lines: [
    {
      id,
      product: { owner: "supply" as const, product_id: id, version_id: id },
      quantity_units: 100,
      unit_code: "capsule",
    },
  ],
};
const order = {
  id,
  owner_tenant_id: scope.tenantId,
  side: "buyer",
  buyer_department_id: scope.departmentId,
  terms,
  input,
  input_sha256: "b".repeat(64),
  status: "pending_review",
  version: 1,
  authority_epoch: 1,
  remote_epoch: 0,
  remote_phase: "",
  prepared_by: scope.subject,
  prepared_staff_id: scope.hospitalId,
  created_at: "2026-11-08T00:00:00Z",
  updated_at: "2026-11-08T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());
it("prepares and recovers an exact actor-bound order without rewriting it", async () => {
  const request = vi.fn<typeof fetch>().mockImplementation(async () => Response.json(order));
  vi.stubGlobal("fetch", request);
  expect(await prepareSupplierOrder(scope, id, input)).toEqual(order);
  expect(await recoverSupplierOrderProposal(scope, id, input)).toEqual(order);
  expect(request.mock.calls.map(([, init]) => init?.method ?? "GET")).toEqual(["PUT", "GET"]);
  expect(request.mock.calls[1]![0]).toContain("/proposal-receipt");
  expect(new Headers(request.mock.calls[0]![1]?.headers).get("X-Helivanta-Expected-Subject")).toBe(
    scope.subject,
  );
});
it.each([
  { owner_tenant_id: terms.seller.tenant_id },
  { buyer_department_id: id },
  { id: scope.outletId },
  { prepared_by: "TEST-another" },
  { input: { ...input, reason: "changed" } },
])("refuses mismatched preparation evidence %j", async (change) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ ...order, ...change })));
  await expect(recoverSupplierOrderProposal(scope, id, input)).rejects.toThrow();
});
it("recovers the original decision receipt and does not post again", async () => {
  const command = "019a55c0-1111-7111-8111-111111111119";
  const decision = {
    expected_version: 1,
    decision: "approve" as const,
    reason: "TEST independently approved",
  };
  const source = { ...scope, subject: "TEST-reviewer" };
  const receipt = {
    command_id: command,
    input: decision,
    order: {
      ...order,
      version: 2,
      authority_epoch: 2,
      status: "offered",
      signed_by: source.subject,
      signed_staff_id: id,
    },
    actor_subject: source.subject,
    actor_staff_id: id,
    recorded_at: "2026-11-08T01:00:00Z",
  };
  const request = vi.fn<typeof fetch>().mockImplementation(async () => Response.json(receipt));
  vi.stubGlobal("fetch", request);
  expect(await decideSupplierOrder(source, id, command, decision)).toEqual(receipt);
  expect(await recoverSupplierOrderCommand(source, id, command, decision)).toEqual(receipt);
  expect(request.mock.calls.map(([, init]) => init?.method ?? "GET")).toEqual(["PUT", "GET"]);
});

it("discovers exact independent and hospital pharmacy source orders through explicit scope paths", async () => {
  const source = {
    tenantId: terms.seller.tenant_id,
    subject: "TEST-source",
    product: "supply" as const,
    organizationId: terms.seller.organization_id,
    locationId: terms.seller.location_id,
  };
  const sourceOrder = {
    ...order,
    side: "seller",
    buyer_department_id: undefined,
    owner_tenant_id: source.tenantId,
  };
  const fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => sourceOrder });
  vi.stubGlobal("fetch", fetch);
  await expect(getSourceSupplierOrder(source, id)).resolves.toMatchObject(sourceOrder);
  expect(fetch).toHaveBeenCalledWith(
    `/api/v1/supply/stock-owners/supply/organizations/${source.organizationId}/locations/${source.locationId}/supplier-orders/${id}`,
    expect.objectContaining({ cache: "no-store" }),
  );
  const hospitalScope = {
    ...source,
    product: "pharmacy" as const,
    hospitalId: scope.hospitalId,
    departmentId: scope.departmentId,
  };
  const hospitalOrder = {
    ...sourceOrder,
    terms: {
      ...terms,
      seller: {
        ...terms.seller,
        product: "pharmacy",
        hospital_id: scope.hospitalId,
        binding_id: id,
        binding_version: 1,
      },
    },
  };
  fetch.mockResolvedValue({ ok: true, json: async () => hospitalOrder });
  await expect(getSourceSupplierOrder(hospitalScope, id)).resolves.toMatchObject(hospitalOrder);
  expect(fetch).toHaveBeenLastCalledWith(
    `/api/v1/supply/hospitals/${scope.hospitalId}/departments/${scope.departmentId}/pharmacies/${source.organizationId}/outlets/${source.locationId}/supplier-orders/${id}`,
    expect.objectContaining({ cache: "no-store" }),
  );
  const decision = {
    expected_version: 1,
    decision: "approve" as const,
    reason: "TEST supplier acceptance",
  };
  const command = "0199e000-0000-7000-8000-000000000025";
  const accepted = {
    command_id: command,
    input: decision,
    order: { ...hospitalOrder, status: "accepted", version: 2 },
    actor_subject: source.subject,
    actor_staff_id: id,
    recorded_at: "2026-11-08T01:00:00Z",
  };
  fetch.mockResolvedValue({ ok: true, json: async () => accepted });
  await expect(
    decideSourceSupplierOrder(hospitalScope, id, command, decision),
  ).resolves.toMatchObject(accepted);
  await expect(
    recoverSourceSupplierOrderCommand(hospitalScope, id, command, decision),
  ).resolves.toMatchObject(accepted);
  expect(fetch).toHaveBeenLastCalledWith(
    `/api/v1/supply/hospitals/${scope.hospitalId}/departments/${scope.departmentId}/pharmacies/${source.organizationId}/outlets/${source.locationId}/supplier-orders/${id}/commands/${command}`,
    expect.objectContaining({ cache: "no-store" }),
  );
  fetch.mockClear();
  await expect(
    getSourceSupplierOrder({ ...hospitalScope, product: "supply" }, id),
  ).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});

it("checks source review admission against the exact current account and owner scope", async () => {
  const source = {
    tenantId: terms.seller.tenant_id,
    subject: "TEST-reviewer",
    product: "supply" as const,
    organizationId: terms.seller.organization_id,
    locationId: terms.seller.location_id,
  };
  const admission = {
    owner_tenant_id: source.tenantId,
    order_id: id,
    organization_id: source.organizationId,
    location_id: source.locationId,
    product: source.product,
    actor_subject: source.subject,
    actor_staff_id: id,
    observed_at: "2026-11-09T00:00:00Z",
  };
  const fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => admission });
  vi.stubGlobal("fetch", fetch);
  await expect(getSourceSupplierReviewAccess(source, id)).resolves.toMatchObject(admission);
  expect(fetch).toHaveBeenCalledWith(
    `/api/v1/supply/stock-owners/${source.product}/organizations/${source.organizationId}/locations/${source.locationId}/supplier-orders/${id}/review-access`,
    expect.objectContaining({ cache: "no-store" }),
  );
  fetch.mockResolvedValue({
    ok: true,
    json: async () => ({ ...admission, actor_subject: "TEST-different" }),
  });
  await expect(getSourceSupplierReviewAccess(source, id)).rejects.toThrow();
  fetch.mockResolvedValue({
    ok: true,
    json: async () => ({ ...admission, location_id: "0199e000-0000-7000-8000-000000000099" }),
  });
  await expect(getSourceSupplierReviewAccess(source, id)).rejects.toThrow();
});
