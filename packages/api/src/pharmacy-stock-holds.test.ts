import { afterEach, expect, it, vi } from "vitest";
import { apiFetch } from "./client";
import { recordPharmacyStockHold, readPharmacyStockHold } from "./pharmacy-stock-holds";
vi.mock("./client", () => ({ apiFetch: vi.fn() }));
afterEach(() => vi.resetAllMocks());
const scope = {
  tenantId: "0199e000-0000-7000-8000-000000000001",
  pharmacyId: "0199e000-0000-7000-8000-000000000002",
  outletId: "0199e000-0000-7000-8000-000000000003",
  subject: "TEST-holder",
};
const id = "0199e000-0000-7000-8000-000000000004";
const input = {
  batch_id: "0199e000-0000-7000-8000-000000000005",
  from_location_id: "0199e000-0000-7000-8000-000000000006",
  quarantine_location_id: "0199e000-0000-7000-8000-000000000007",
  quantity_units: 4,
  reason: "TEST physically moved",
  physical_disposition: "moved_to_quarantine" as const,
};
const receipt = {
  ...input,
  id,
  outlet_id: scope.outletId,
  out_movement_id: "0199e000-0000-7000-8000-000000000008",
  in_movement_id: "0199e000-0000-7000-8000-000000000009",
  held_by: scope.subject,
  held_at: "2026-11-21T00:00:00Z",
};
it("records and recovers one actor-bound physical hold with its two movements", async () => {
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  expect(await recordPharmacyStockHold(scope, id, input)).toEqual(receipt);
  expect(apiFetch).toHaveBeenCalledWith(
    expect.stringContaining(`/stock/holds/${id}`),
    expect.objectContaining({
      method: "PUT",
      cache: "no-store",
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
    }),
  );
  expect(await readPharmacyStockHold(scope, id)).toEqual(receipt);
});
it.each([
  { ...receipt, held_by: "TEST-other" },
  { ...receipt, quantity_units: 5 },
  { ...receipt, in_movement_id: receipt.out_movement_id },
  { ...receipt, outlet_id: input.batch_id },
])("does not acknowledge unrelated or incomplete movement evidence", async (value) => {
  vi.mocked(apiFetch).mockResolvedValue(value);
  await expect(recordPharmacyStockHold(scope, id, input)).rejects.toThrow(/recover|verified/i);
  expect(apiFetch).toHaveBeenCalledTimes(1);
});
it("never sends a same-bucket or unconfirmed physical move", async () => {
  await expect(
    recordPharmacyStockHold(scope, id, {
      ...input,
      quarantine_location_id: input.from_location_id,
    }),
  ).rejects.toThrow();
  await expect(
    recordPharmacyStockHold(scope, id, {
      ...input,
      physical_disposition: "pending" as "moved_to_quarantine",
    }),
  ).rejects.toThrow();
  expect(apiFetch).not.toHaveBeenCalled();
});

it.each([
  {
    ...receipt,
    out_movement_id: "0199e000-0000-7000-8000-00000000000a",
    in_movement_id: "0199E000-0000-7000-8000-00000000000A",
  },
  {
    ...receipt,
    from_location_id: "0199e000-0000-7000-8000-00000000000b",
    quarantine_location_id: "0199E000-0000-7000-8000-00000000000B",
  },
])("rejects recovery evidence that only changes UUID letter case", async (value) => {
  vi.mocked(apiFetch).mockResolvedValue(value);
  await expect(readPharmacyStockHold(scope, id)).rejects.toThrow(/recover|verified/i);
  expect(apiFetch).toHaveBeenCalledTimes(1);
});
