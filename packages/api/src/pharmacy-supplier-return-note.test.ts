import { afterEach, expect, it, vi } from "vitest";
import { fetchPharmacySupplierReturnNote } from "./pharmacy-supplier-return-note";
import { sha256Hex } from "./attachment-file";
import type { PharmacySupplierReturn } from "./pharmacy-supplier-returns";

const scope = {
  subject: "TEST-reader",
  tenantId: "0199e000-0000-7000-8000-000000000001",
  pharmacyId: "0199e000-0000-7000-8000-000000000002",
  outletId: "0199e000-0000-7000-8000-000000000003",
};
const note = {
  id: "0199e000-0000-7000-8000-000000000004",
  number: 1,
  state: "dispatched" as const,
  lines: [
    {
      id: "0199e000-0000-7000-8000-000000000005",
      batch_id: "0199e000-0000-7000-8000-000000000006",
      product_id: "0199e000-0000-7000-8000-000000000007",
      product_version: 1,
      product_name: "TEST Medicine",
      generic_name: "TEST Generic",
      unit_code: "tablet",
      lot_number: "TEST LOT",
      expires_on: "2028-01-01",
      receipt_units: 80,
      source_reference: "TEST delivery",
      source_created_at: "2026-11-21T00:00:00Z",
      receipt_id: "0199e000-0000-7000-8000-000000000008",
      quarantine_location_id: scope.outletId,
      quantity_units: 4,
      expected_credit_minor: 1500,
    },
  ] satisfies PharmacySupplierReturn["lines"],
};
const bytes = new TextEncoder().encode("<!doctype html><title>TEST Return 1</title>");
async function response(overrides: Record<string, string> = {}, body = bytes) {
  return new Response(body, {
    headers: {
      "Content-Type": "text/html; charset=utf-8",
      "Content-Disposition": 'attachment; filename="supplier-return-1.html"',
      "Cache-Control": "private, no-store",
      "Content-Security-Policy": "sandbox; default-src 'none'",
      "X-Supplier-Return-Tenant-Id": scope.tenantId,
      "X-Supplier-Return-Outlet-Id": scope.outletId,
      "X-Supplier-Return-Id": note.id,
      "X-Supplier-Return-Number": "1",
      "X-Export-Row-Count": "1",
      "X-Export-Byte-Count": String(bytes.byteLength),
      "X-Export-Sha256": await sha256Hex(bytes),
      ...overrides,
    },
  });
}
afterEach(() => vi.unstubAllGlobals());

it("downloads only the private byte-verified note for the displayed return", async () => {
  const fetch = vi.fn().mockResolvedValue(await response());
  vi.stubGlobal("fetch", fetch);
  const file = await fetchPharmacySupplierReturnNote(scope, note, new AbortController().signal);
  expect(file.filename).toBe("supplier-return-1.html");
  expect(await file.blob.text()).toContain("TEST Return 1");
  expect(fetch.mock.calls[0]?.[0]).toContain(`/supplier-returns/${note.id}/note.html`);
  const options = fetch.mock.calls[0]?.[1];
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(options.cache).toBe("no-store");
});

it.each([
  ["X-Supplier-Return-Tenant-Id", note.id],
  ["X-Supplier-Return-Outlet-Id", note.id],
  ["X-Supplier-Return-Id", scope.outletId],
  ["X-Supplier-Return-Number", "2"],
  ["X-Export-Row-Count", "2"],
  ["X-Export-Sha256", "0".repeat(64)],
  ["X-Export-Byte-Count", "1048577"],
  ["X-Export-Byte-Count", "-1"],
  ["Content-Type", "application/javascript"],
  ["Content-Disposition", 'attachment; filename="another.html"'],
  ["Cache-Control", "public"],
  ["Content-Security-Policy", "default-src *"],
])("refuses a mismatched %s proof", async (header, value) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(await response({ [header]: value })));
  await expect(
    fetchPharmacySupplierReturnNote(scope, note, new AbortController().signal),
  ).rejects.toThrow();
});

it("rejects short, extra and changed bytes instead of releasing a partial note", async () => {
  for (const body of [
    bytes.slice(0, -1),
    new Uint8Array(bytes.length + 1),
    new Uint8Array(bytes.length),
  ]) {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(await response({}, body)));
    await expect(
      fetchPharmacySupplierReturnNote(scope, note, new AbortController().signal),
    ).rejects.toThrow();
  }
});

it("does not fetch a draft, invalid identity or an aborted download", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(
    fetchPharmacySupplierReturnNote(
      scope,
      { ...note, state: "draft" },
      new AbortController().signal,
    ),
  ).rejects.toThrow();
  await expect(
    fetchPharmacySupplierReturnNote(
      scope,
      { ...note, id: "../other" },
      new AbortController().signal,
    ),
  ).rejects.toThrow();
  const controller = new AbortController();
  controller.abort();
  await expect(fetchPharmacySupplierReturnNote(scope, note, controller.signal)).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});
