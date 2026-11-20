import { afterEach, expect, it, vi } from "vitest";
import { fetchLowStockPage, saveReorderLevel } from "./pharmacy-reorder";

const scope = {
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const product = "0199e000-0000-7000-8000-000000000002";
const command = "0199e000-0000-7000-8000-000000000003";
const input = { minimum_units: 10, expected_version: 0, product_version: 1 };
const receipt = {
  ...input,
  id: command,
  product_id: product,
  outlet_id: scope.outletId,
  version: 1,
  unit_of_issue: "tablet",
  recorded_at: "2026-11-20T00:00:00Z",
};
const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200 });
afterEach(() => vi.unstubAllGlobals());
it("pins the command and scope and rejects a receipt for another product", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(json(receipt))
    .mockResolvedValueOnce(json({ ...receipt, product_id: command }));
  vi.stubGlobal("fetch", fetch);
  expect(await saveReorderLevel(scope, product, command, input)).toEqual(receipt);
  expect(fetch.mock.calls[0]?.[0]).toBe(
    `/api/v1/pharmacy/pharmacies/${scope.pharmacyId}/outlets/${scope.outletId}/catalogue/products/${product}/reorder-levels/${command}`,
  );
  const request = fetch.mock.calls[0]?.[1] as RequestInit;
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(JSON.parse(request.body as string)).toEqual(input);
  await expect(saveReorderLevel(scope, product, command, input)).rejects.toThrow(/receipt/i);
});
it("keeps exact large quantities and rejects a foreign outlet page", async () => {
  const row = {
    id: product,
    outlet_id: scope.outletId,
    sku: "TEST",
    brand_name: "TEST Product",
    unit_of_issue: "tablet",
    minimum_units: 10,
    available_units: "0",
    observed_at: "2026-11-20T00:00:00Z",
    local_date: "2026-11-20",
    time_zone: "Asia/Kolkata",
  };
  const page = { data: [row], page: { has_more: false, next_cursor: null } };
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(json(page))
    .mockResolvedValueOnce(json({ ...page, data: [{ ...row, outlet_id: command }] }));
  vi.stubGlobal("fetch", fetch);
  expect((await fetchLowStockPage(scope, null)).data[0]?.available_units).toBe("0");
  await expect(fetchLowStockPage(scope, null)).rejects.toThrow(/outlet/i);
});
it("rejects unsafe quantities before displaying a low-stock page", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        json({
          data: [
            {
              id: product,
              outlet_id: scope.outletId,
              sku: "TEST",
              brand_name: "TEST",
              unit_of_issue: "tablet",
              minimum_units: 10,
              available_units: -1,
              observed_at: "2026-11-20T00:00:00Z",
              local_date: "2026-11-20",
              time_zone: "Asia/Kolkata",
            },
          ],
          page: { has_more: false, next_cursor: null },
        }),
      ),
  );
  await expect(fetchLowStockPage(scope, null)).rejects.toThrow();
});
