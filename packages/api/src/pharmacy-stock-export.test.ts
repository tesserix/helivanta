import { afterEach, expect, it, vi } from "vitest";
import { fetchPharmacyStockCSV } from "./pharmacy-stock-export";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
afterEach(() => vi.unstubAllGlobals());

it.each([
  ["text/html", 'attachment; filename="pharmacy-stock-report.csv"'],
  ["text/csv", 'attachment; filename="../../report.csv"'],
  ["text/csv", 'inline; filename="pharmacy-stock-report.csv"'],
  ["text/csv", ""],
])(
  "refuses an unexpected file response without accepting its filename",
  async (type, disposition) => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response("TEST", {
          headers: { "Content-Type": type, "Content-Disposition": disposition },
        }),
      ),
    );
    await expect(
      fetchPharmacyStockCSV(scope, "as_of=2026-10-18T00:00:00Z", new AbortController().signal),
    ).rejects.toThrow("valid stock CSV");
  },
);

it("refuses a response if the workspace aborts while the body is being read", async () => {
  const controller = new AbortController();
  const response = new Response("TEST", {
    headers: {
      "Content-Type": "text/csv",
      "Content-Disposition": 'attachment; filename="pharmacy-stock-report.csv"',
    },
  });
  const blob = await response.blob();
  vi.spyOn(response, "blob").mockImplementation(async () => {
    controller.abort();
    return blob;
  });
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
  await expect(
    fetchPharmacyStockCSV(scope, "as_of=2026-10-18T00:00:00Z", controller.signal),
  ).rejects.toMatchObject({ name: "AbortError" });
});
