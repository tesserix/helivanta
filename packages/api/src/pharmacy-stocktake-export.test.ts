import { afterEach, expect, it, vi } from "vitest";
import { fetchStocktakeCSV } from "./index";
import { sha256Hex } from "./attachment-file";

const scope = {
  subject: "TEST stocktake actor",
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const take = {
  id: "0199e000-0000-7000-8000-000000000002",
  number: 4,
  version: 3,
  snapshot_rows: 1,
};
const bytes = new TextEncoder().encode("TEST register\r\n");
async function response(overrides: Record<string, string> = {}, body = bytes) {
  return new Response(body, {
    headers: {
      "Content-Type": "text/csv; charset=utf-8",
      "Content-Disposition": 'attachment; filename="stocktake-4-register.csv"',
      "Cache-Control": "private, no-store",
      "X-Stocktake-Tenant-Id": scope.tenantId,
      "X-Stocktake-Outlet-Id": scope.outletId,
      "X-Stocktake-Id": take.id,
      "X-Stocktake-Version": "3",
      "X-Export-Row-Count": "1",
      "X-Export-Sha256": await sha256Hex(bytes),
      ...overrides,
    },
  });
}
afterEach(() => vi.unstubAllGlobals());

it("downloads only a byte-verified register for the displayed outlet and revision", async () => {
  const fetch = vi.fn().mockResolvedValue(await response());
  vi.stubGlobal("fetch", fetch);
  const result = await fetchStocktakeCSV(scope, take, new AbortController().signal);
  expect(result.filename).toBe("stocktake-4-register.csv");
  expect(new Headers(fetch.mock.calls[0]?.[1].headers).get("X-Helivanta-Expected-Subject")).toBe(
    scope.subject,
  );
  expect(await result.blob.text()).toBe("TEST register\r\n");
  expect(fetch.mock.calls[0]?.[0]).toContain(`/stocktakes/${take.id}/register.csv`);
  expect(new Headers(fetch.mock.calls[0]?.[1].headers).get("X-Helivanta-Expected-Tenant")).toBe(
    scope.tenantId,
  );
});

it.each([
  ["X-Stocktake-Tenant-Id", take.id],
  ["X-Stocktake-Outlet-Id", take.id],
  ["X-Stocktake-Id", scope.outletId],
  ["X-Stocktake-Version", "4"],
  ["X-Export-Row-Count", "2"],
  ["X-Export-Sha256", "0".repeat(64)],
  ["Content-Type", "text/html"],
  ["Content-Disposition", 'attachment; filename="stocktake-5-register.csv"'],
  ["Cache-Control", "public, max-age=60"],
])("refuses mismatched or unsafe %s", async (key, value) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(await response({ [key]: value })));
  await expect(fetchStocktakeCSV(scope, take, new AbortController().signal)).rejects.toThrow(
    /register/i,
  );
});

it("stops a chunked file at the byte limit and cancels the response stream", async () => {
  const cancel = vi.fn();
  const source = await response();
  const stream = new ReadableStream({
    start(controller) {
      controller.enqueue(new Uint8Array(8 * 1024 * 1024 + 1));
    },
    cancel,
  });
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(new Response(stream, { headers: source.headers })),
  );
  await expect(fetchStocktakeCSV(scope, take, new AbortController().signal)).rejects.toThrow(
    /register/i,
  );
  expect(cancel).toHaveBeenCalledTimes(1);
});

it("does not request a file after its scope has been cancelled", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  const controller = new AbortController();
  controller.abort();
  await expect(fetchStocktakeCSV(scope, take, controller.signal)).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});
