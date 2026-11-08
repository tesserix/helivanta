import { afterEach, expect, it, vi } from "vitest";
import { ApiError } from "./client";
import {
  PharmacyReceiptContract,
  PharmacyReceiptInputContract,
  PharmacyReceivingProductContract,
  isReceivingAccessChanged,
  recordPharmacyReceipt,
  recordPharmacyReceivingBatch,
  recoverPharmacyReceipt,
} from "./pharmacy-receiving";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-receiver",
  pharmacyId: "019a55c0-1111-7111-8111-111111111112",
  outletId: "019a55c0-1111-7111-8111-111111111113",
};
const stock = `/api/v1/pharmacy/pharmacies/${scope.pharmacyId}/operating-locations/${scope.outletId}/stock`;
const input = {
  location_id: "019a55c0-1111-7111-8111-111111111114",
  batch_id: "019a55c0-1111-7111-8111-111111111115",
  kind: "receipt" as const,
  quantity_units: 40,
  reason: "TEST supplier delivery",
  source_reference: "TEST-GRN:1",
};
const receipt = {
  ...input,
  id: "019a55c0-1111-7111-8111-111111111116",
  outlet_id: scope.outletId,
  actor_subject: scope.subject,
  created_at: "2026-11-08T00:00:00Z",
};
const lot = {
  product_id: "019a55c0-1111-7111-8111-111111111117",
  lot_number: "TEST-LOT",
  expires_on: "2030-12-31",
  mrp_minor: 12500,
};
const batch = {
  ...lot,
  id: input.batch_id,
  outlet_id: scope.outletId,
  manufactured_on: null,
  purchase_cost_minor: null,
  product_version: 1,
  created_at: "2026-11-08T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());

it("accepts only a positive receipt, never a correction", () => {
  expect(PharmacyReceiptInputContract.parse(input)).toEqual(input);
  for (const change of [{ kind: "correction" }, { quantity_units: -1 }, { quantity_units: 0 }])
    expect(PharmacyReceiptInputContract.safeParse({ ...input, ...change }).success).toBe(false);
});

it("refuses a product row that carries editor-only commercial or regulatory fields", () => {
  const product = {
    id: lot.product_id,
    outlet_id: scope.outletId,
    sku: "TEST-SKU",
    brand_name: "TEST Brand",
    generic_name: "TEST generic",
    dosage_form: "tablet",
    ingredients: [{ name: "TEST", strength: "500", unit: "mg" }],
    pack_size: 10,
    unit_of_issue: "strip",
    currency: "INR",
    version: 1,
    created_at: "2026-11-08T00:00:00Z",
  };
  expect(PharmacyReceivingProductContract.parse(product)).toEqual(product);
  for (const extra of [{ mrp_minor: 1 }, { schedule: "H" }, { tax_rate: "12" }])
    expect(PharmacyReceivingProductContract.safeParse({ ...product, ...extra }).success).toBe(
      false,
    );
});

it("records a receipt at the exact outlet with the expected tenant and subject", async () => {
  const request = vi.fn<typeof fetch>().mockResolvedValue(Response.json(receipt));
  vi.stubGlobal("fetch", request);
  expect(await recordPharmacyReceipt(scope, receipt.id, input)).toEqual(receipt);
  const [url, init] = request.mock.calls[0]!;
  expect(url).toBe(`${stock}/receipts/${receipt.id}`);
  expect(init?.method).toBe("PUT");
  expect(JSON.parse(String(init?.body))).toEqual(input);
  const headers = new Headers(init?.headers);
  expect(headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(headers.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

it.each([
  { outlet_id: "019a55c0-1111-7111-8111-111111111199" },
  { id: "019a55c0-1111-7111-8111-111111111198" },
  { actor_subject: "TEST-other" },
  { quantity_units: 41 },
  { location_id: "019a55c0-1111-7111-8111-111111111197" },
  { stock_status: "sellable" },
])("rejects a receipt answer that is not exactly what was sent: %j", async (change) => {
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>().mockResolvedValue(Response.json({ ...receipt, ...change })),
  );
  await expect(recordPharmacyReceipt(scope, receipt.id, input)).rejects.toThrow();
});

it("recovers a lost receipt outcome through GET without writing again", async () => {
  const request = vi.fn<typeof fetch>().mockResolvedValue(Response.json(receipt));
  vi.stubGlobal("fetch", request);
  expect(await recoverPharmacyReceipt(scope, receipt.id, input)).toEqual(receipt);
  expect(request.mock.calls.map(([url, init]) => [url, init?.method ?? "GET"])).toEqual([
    [`${stock}/receipts/${receipt.id}`, "GET"],
  ]);
  expect(PharmacyReceiptContract.parse(receipt)).toEqual(receipt);
});

it("rejects a replayed lot whose label differs from what was entered", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>().mockResolvedValue(Response.json({ ...batch, mrp_minor: 12600 })),
  );
  await expect(recordPharmacyReceivingBatch(scope, batch.id, lot)).rejects.toThrow(
    "does not match the label",
  );
  vi.stubGlobal("fetch", vi.fn<typeof fetch>().mockResolvedValue(Response.json(batch)));
  expect(await recordPharmacyReceivingBatch(scope, batch.id, lot)).toEqual(batch);
});

it("treats an unknown outlet or a changed session as lost access, not a retryable failure", () => {
  expect(isReceivingAccessChanged(new ApiError("not_found", "gone", 404))).toBe(true);
  expect(isReceivingAccessChanged(new ApiError("session_changed", "again", 401))).toBe(true);
  expect(isReceivingAccessChanged(new ApiError("session_changed", "again", 409))).toBe(true);
  expect(isReceivingAccessChanged(new ApiError("audit_unavailable", "later", 503))).toBe(false);
  expect(isReceivingAccessChanged(new ApiError("conflict", "taken", 409))).toBe(false);
});
