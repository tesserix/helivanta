import { afterEach, expect, it, vi } from "vitest";
import * as sdk from "./supplier-access";
const id = (value: number) => `0199e000-0000-7000-8000-${String(value).padStart(12, "0")}`;
const scope = {
  tenantId: id(1),
  subject: "TEST-hospital-viewer",
  hospitalId: id(2),
  grantId: id(3),
  relationshipId: id(8),
  termsSha256: "a".repeat(64),
  source: {
    source_tenant_id: id(4),
    recipient_tenant_id: id(1),
    hospital_id: id(2),
    source_product: "pharmacy" as const,
    source_organization_id: id(5),
    source_location_id: id(6),
  },
};
const row = {
  id: id(7),
  scope: scope.source,
  grant_id: scope.grantId,
  relationship_id: id(8),
  terms_sha256: scope.termsSha256,
  purchase_order_reference: "TEST hospital PO",
  created_at: "2026-11-22T00:00:00Z",
  source_organization_name: "TEST Source Organisation",
  source_location_name: "TEST Pune Premise",
};
const page = (rows: unknown[]) => ({ data: rows, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
it("reads named source order references through Global API under the hospital account without switching tenants", async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(page([row]))));
  vi.stubGlobal("fetch", fetcher);
  const result = await sdk.listPartnerShipmentOrders(scope);
  expect(result.data).toEqual([row]);
  expect(fetcher.mock.calls[0][0]).toContain(
    `/supply/hospitals/${scope.hospitalId}/partner-grants/pharmacy/${scope.grantId}/terms/${scope.termsSha256}/shipment-orders?limit=50`,
  );
  const request = fetcher.mock.calls[0][1] as RequestInit;
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(request.cache).toBe("no-store");
  expect(request.signal).toBeInstanceOf(AbortSignal);
});
it("preserves current source organisation and premise names for human choices", async () => {
  const named = {
    ...row,
    source_organization_name: "TEST Independent Pharmacy",
    source_location_name: "TEST Pune Outlet",
  };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(page([named])))));
  expect((await sdk.listPartnerShipmentOrders(scope)).data).toEqual([named]);
});
it.each([
  "source_tenant_id",
  "recipient_tenant_id",
  "hospital_id",
  "source_product",
  "source_organization_id",
  "source_location_id",
] as const)("refuses a %s from another sharing scope", async (field) => {
  const foreign = {
    ...row,
    scope: { ...row.scope, [field]: field === "source_product" ? "supply" : id(90) },
  };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(page([foreign])))));
  await expect(sdk.listPartnerShipmentOrders(scope)).rejects.toThrow();
});
it("refuses a response from another grant", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(new Response(JSON.stringify(page([{ ...row, grant_id: id(90) }])))),
  );
  await expect(sdk.listPartnerShipmentOrders(scope)).rejects.toThrow();
});
it.each([
  { data: [row, row], page: { has_more: false, next_cursor: null } },
  { data: [row], page: { has_more: true, next_cursor: null } },
  { data: [row], page: { has_more: false, next_cursor: "TEST-next" } },
  { data: [row], page: { has_more: true, next_cursor: "TEST-before" } },
  { data: [], page: { has_more: true, next_cursor: "TEST-next" } },
])("refuses inconsistent, duplicated or looping resource continuation %#", async (response) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(response))));
  await expect(sdk.listPartnerShipmentOrders(scope, "TEST-before")).rejects.toThrow();
});

it("refuses a response for another bilateral relationship", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify(page([{ ...row, relationship_id: id(90) }])))),
  );
  await expect(sdk.listPartnerShipmentOrders(scope)).rejects.toThrow();
});
const shipment = {
  id: id(9),
  scope: scope.source,
  grant_id: scope.grantId,
  relationship_id: scope.relationshipId,
  terms_sha256: scope.termsSha256,
  order_id: row.id,
  purchase_order_reference: row.purchase_order_reference,
  product_description: "TEST source product",
  unit_code: "tablet",
  pack_size: 10,
  quantity_units: 5,
  lot_number: "TEST-LOT",
  expires_on: "2027-05-01",
  handoff_reference: "TEST dispatch note",
  dispatched_at: "2026-11-22T01:00:00Z",
  source_organization_name: row.source_organization_name,
  source_location_name: row.source_location_name,
};
it.each(["pharmacy", "supply"] as const)(
  "reads only the declared shipment projection from %s",
  async (owner) => {
    const current = { ...scope, source: { ...scope.source, source_product: owner } };
    const value = { ...shipment, scope: current.source };
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(page([value]))));
    vi.stubGlobal("fetch", fetcher);
    expect((await sdk.listPartnerShipments(current, row.id)).data).toEqual([value]);
    expect(fetcher.mock.calls[0][0]).toContain(
      `/partner-grants/${owner}/${scope.grantId}/terms/${scope.termsSha256}/shipment-orders/${row.id}/shipments?limit=50`,
    );
  },
);
it.each([
  { ...shipment, order_id: id(90) },
  { ...shipment, grant_id: id(90) },
  { ...shipment, relationship_id: id(90) },
  { ...shipment, scope: { ...shipment.scope, recipient_tenant_id: id(90) } },
  { ...shipment, actor_subject: "TEST private source staff" },
  { ...shipment, mrp_minor: 10000 },
  { ...shipment, quantity_units: 0 },
  { ...shipment, quantity_units: 0.5 },
  { ...shipment, pack_size: -1 },
  { ...shipment, expires_on: "2027-02-30" },
  { ...shipment, handoff_reference: "" },
])("refuses unrelated, invalid or over-broad source shipment evidence %#", async (value) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(page([value])))));
  await expect(sdk.listPartnerShipments(scope, row.id)).rejects.toThrow();
});
it("does not retry a refused read or switch to the source tenant", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    new Response(JSON.stringify({ error: { code: "forbidden", message: "TEST refusal" } }), {
      status: 403,
    }),
  );
  vi.stubGlobal("fetch", fetcher);
  await expect(sdk.listPartnerShipments(scope, row.id)).rejects.toMatchObject({ status: 403 });
  expect(fetcher).toHaveBeenCalledTimes(1);
  const request = fetcher.mock.calls[0][1] as RequestInit;
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
});
it("refuses evidence for another approved agreement snapshot", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify(page([{ ...row, terms_sha256: "b".repeat(64) }]))),
      ),
  );
  await expect(sdk.listPartnerShipmentOrders(scope)).rejects.toThrow();
});

it("refuses mixed source names within one guarded projection page", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(
          JSON.stringify(
            page([row, { ...row, id: id(10), source_organization_name: "TEST Other Supplier" }]),
          ),
        ),
      ),
  );
  await expect(sdk.listPartnerShipmentOrders(scope)).rejects.toThrow();
});

it.each([
  { ...row, source_organization_name: undefined },
  { ...row, source_location_name: undefined },
  { ...row, source_organization_name: "" },
  { ...row, source_location_name: " TEST Premise" },
  { ...row, source_organization_name: "TEST\nSupplier" },
  { ...row, source_location_name: "x".repeat(201) },
  { ...row, source_location_name: "😀".repeat(201) },
])(
  "refuses missing or unsafe source labels instead of falling back to identifiers %#",
  async (value) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(page([value])))));
    await expect(sdk.listPartnerShipmentOrders(scope)).rejects.toThrow();
  },
);
