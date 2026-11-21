import { expect, it } from "vitest";
import type { z } from "zod";
import * as contract from "./pharmacy-contract";

it("accepts an attributable physical quarantine hold without inventing stock", () => {
  const schema = Reflect.get(contract, "pharmacyStockHoldInputSchema") as z.ZodType | undefined;
  const input = {
    batch_id: "0199e000-0000-7000-8000-000000000001",
    from_location_id: "0199e000-0000-7000-8000-000000000002",
    quarantine_location_id: "0199e000-0000-7000-8000-000000000003",
    quantity_units: 4,
    reason: "TEST set aside for supplier return",
    physical_disposition: "moved_to_quarantine",
  };
  expect(schema?.safeParse(input)).toMatchObject({ success: true, data: input });
});

it.each([
  ["zero", 0],
  ["negative", -1],
  ["fractional", 1.5],
  ["over movement bound", 1000000001],
  ["missing", undefined],
  ["null", null],
])("refuses %s quarantine quantities", (_label, quantity_units) => {
  const schema = Reflect.get(contract, "pharmacyStockHoldInputSchema") as z.ZodType | undefined;
  expect(
    schema?.safeParse({
      batch_id: "0199e000-0000-7000-8000-000000000001",
      from_location_id: "0199e000-0000-7000-8000-000000000002",
      quarantine_location_id: "0199e000-0000-7000-8000-000000000003",
      quantity_units,
      reason: "TEST set aside",
      physical_disposition: "moved_to_quarantine",
    }).success,
  ).toBe(false);
});

it("exports account-bound physical hold commands", async () => {
  const sdk = await import("./index");
  expect(Reflect.get(sdk, "recordPharmacyStockHold")).toBeTypeOf("function");
});
