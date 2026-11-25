import { afterEach, expect, it, vi } from "vitest";
import {
  readPharmacyPurchaseTax,
  savePharmacyPurchaseTax,
  type PharmacyPurchaseTaxInput,
} from "./pharmacy-purchase-tax";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
  subject: "TEST-owner",
};
const bill = "0199e000-0000-7000-8000-000000000020";
const command = "0199e000-0000-7000-8000-000000000021";
const input: PharmacyPurchaseTaxInput = {
  replaces: null,
  supplier_gstin: "07ABCDE1234F1Z2",
  supplier_state: "07",
  place_of_supply: "07",
  reason: "TEST source",
  lines: [
    {
      position: 1,
      supply_nature: "taxable",
      rate_ppm: 120000,
      assessable_minor: 10000,
      source_variance_reason: null,
      cgst_minor: 600,
      sgst_minor: 600,
      igst_minor: 0,
      utgst_minor: 0,
      cgst_recoverable_minor: 300,
      sgst_recoverable_minor: 300,
      igst_recoverable_minor: 0,
      utgst_recoverable_minor: 0,
    },
  ],
};
const receipt = {
  ...input,
  id: command,
  outlet_id: scope.outletId,
  payable_id: bill,
  recorded_by: scope.subject,
  recorded_at: "2026-11-26T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());
it("pins purchase evidence reads to the tenant and signed-in actor", async () => {
  const fetcher = vi.fn(async () => Response.json({ evidence: receipt }));
  vi.stubGlobal("fetch", fetcher);
  await expect(readPharmacyPurchaseTax(scope, bill, new AbortController().signal)).resolves.toEqual(
    receipt,
  );
  const [url, init] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(url).toContain(`/accounts/payables/${bill}/purchase-tax`);
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});
it.each([
  ["different outlet", { outlet_id: bill }],
  ["different bill", { payable_id: command }],
  ["unordered lines", { lines: [{ ...input.lines[0], position: 2 }] }],
])("refuses a read of %s", async (_, change) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ evidence: { ...receipt, ...change } })),
  );
  await expect(readPharmacyPurchaseTax(scope, bill, new AbortController().signal)).rejects.toThrow(
    "could not be verified",
  );
});
it("verifies all source evidence fields on the save receipt", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(receipt)),
  );
  await expect(
    savePharmacyPurchaseTax(scope, bill, command, input, new AbortController().signal),
  ).resolves.toEqual(receipt);
});
it.each([
  ["actor", { recorded_by: "another-user" }],
  ["command", { id: bill }],
  ["predecessor", { replaces: bill }],
  ["supplier", { supplier_gstin: null }],
  ["reason", { reason: "different source" }],
  ["amount", { lines: [{ ...input.lines[0], cgst_minor: 601 }] }],
])("refuses a changed %s on save", async (_, change) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...receipt, ...change })),
  );
  await expect(
    savePharmacyPurchaseTax(scope, bill, command, input, new AbortController().signal),
  ).rejects.toThrow("could not be verified");
});
