import { afterEach, expect, it, vi } from "vitest";
import { WholesaleReceiptContract, WholesaleReceiptInputContract } from "./supply-stock-contract";
import { recordWholesaleReceipt, recoverWholesaleReceipt } from "./supply-stock";

const input = {
  product_version_id: "abcdefab-cdef-4abc-8abc-abcdefabcdef",
  lot_number: "TEST-LOT",
  expires_on: "2030-12-31",
  unit_code: "pack",
  quantity_units: 100,
  source_reference: "TEST-GRN:1",
  reason: "TEST physical count",
};
const receipt = {
  owner_tenant_id: "11111111-1111-4111-8111-111111111111",
  id: "019a55c0-1111-7111-8111-111111111111",
  party_id: "019a55c0-1111-7111-8111-111111111112",
  premise_id: "019a55c0-1111-7111-8111-111111111113",
  input,
  stock_status: "quarantine",
  available_units: 0,
  product_identity_verified: false,
  actor_subject: "TEST-inventory",
  created_at: "2026-11-07T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());
it("accepts only an explicitly unverified quarantined receipt", () => {
  expect(WholesaleReceiptContract.parse(receipt)).toEqual(receipt);
  expect(
    WholesaleReceiptContract.safeParse({ ...receipt, product_identity_verified: true }).success,
  ).toBe(false);
});
it.each([
  { expires_on: "2030-02-30" },
  { lot_number: " TEST-LOT" },
  { unit_code: "arbitrary unit" },
  { quantity_units: 0 },
  { quantity_units: 1.5 },
  { reason: "TEST\nprivate" },
])("rejects invalid physical receipt intent %j", (change) => {
  expect(WholesaleReceiptInputContract.safeParse({ ...input, ...change }).success).toBe(false);
});

it("recovers an exact physical receipt through Global API without another stock write", async () => {
  const scope = {
    tenantId: "11111111-1111-4111-8111-111111111111",
    subject: receipt.actor_subject,
    partyId: receipt.party_id,
    premiseId: receipt.premise_id,
  };
  const request = vi.fn<typeof fetch>().mockImplementation(async () => Response.json(receipt));
  vi.stubGlobal("fetch", request);
  expect(await recordWholesaleReceipt(scope, receipt.id, input)).toEqual(receipt);
  expect(await recoverWholesaleReceipt(scope, receipt.id, input)).toEqual(receipt);
  expect(request.mock.calls.map(([url, init]) => [url, init?.method ?? "GET"])).toEqual([
    [
      `/api/v1/supply/parties/${receipt.party_id}/operating-locations/${receipt.premise_id}/stock/receipts/${receipt.id}`,
      "PUT",
    ],
    [
      `/api/v1/supply/parties/${receipt.party_id}/operating-locations/${receipt.premise_id}/stock/receipts/${receipt.id}`,
      "GET",
    ],
  ]);
  const headers = new Headers(request.mock.calls[0]![1]?.headers);
  expect(headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(headers.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

it("looks up a product through the exact warehouse owner without catalogue-wide authority", async () => {
  const scope = {
    tenantId: receipt.owner_tenant_id,
    subject: receipt.actor_subject,
    partyId: receipt.party_id,
    premiseId: receipt.premise_id,
  };
  const reference = {
    product_id: input.product_version_id,
    version_id: input.product_version_id,
    formulation_id: input.product_version_id,
    formulation_version_id: input.product_version_id,
    brand_name: "TEST Brand",
    manufacturer: "TEST Manufacturer",
    formulation_label: "TEST formulation",
    pack_size: 10,
    unit_of_issue: "capsule",
    regulatory_class: "H",
    ndps: false,
  };
  const request = vi.fn<typeof fetch>().mockResolvedValue(Response.json(reference));
  vi.stubGlobal("fetch", request);
  const { getStockProductReference } = await import("./supply-stock");
  expect(await getStockProductReference(scope, "8901234567890")).toEqual(reference);
  expect(request.mock.calls[0]![0]).toBe(
    `/api/v1/supply/parties/${scope.partyId}/operating-locations/${scope.premiseId}/stock/products/by-gtin/8901234567890`,
  );
});
