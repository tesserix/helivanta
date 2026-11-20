import { describe, expect, it } from "vitest";
import {
  pharmacyStocktakeCountInputSchema,
  pharmacyStocktakeStartInputSchema,
} from "./pharmacy-contract";

const location = "0199e000-0000-7000-8000-000000000001";
const line = "0199e000-0000-7000-8000-000000000002";

describe("stocktake input contracts", () => {
  it("retains an explicit empty all-products scope and trims the recorded reason", () => {
    expect(
      pharmacyStocktakeStartInputSchema.parse({
        selection: "all",
        product_ids: [],
        quarantine_location_id: location,
        reason: " Cycle count ",
      }),
    ).toEqual({
      selection: "all",
      product_ids: [],
      quarantine_location_id: location,
      reason: "Cycle count",
    });
  });

  it.each([undefined, null])("rejects a missing or null product selection: %s", (product_ids) => {
    expect(
      pharmacyStocktakeStartInputSchema.safeParse({
        selection: "all",
        product_ids,
        quarantine_location_id: location,
        reason: "Cycle count",
      }).success,
    ).toBe(false);
  });

  it("retains a real count of zero and a first expected version of zero", () => {
    expect(
      pharmacyStocktakeCountInputSchema.parse({
        expected_version: 0,
        counts: [{ line_id: line, counted_units: 0 }],
        reason: " Shelf count ",
      }),
    ).toEqual({
      expected_version: 0,
      counts: [{ line_id: line, counted_units: 0 }],
      reason: "Shelf count",
    });
  });

  it.each([undefined, null, -1, 1.5, "0", 1_000_000_000_001])(
    "rejects an absent, coerced or invalid counted quantity: %s",
    (counted_units) => {
      expect(
        pharmacyStocktakeCountInputSchema.safeParse({
          expected_version: 0,
          counts: [{ line_id: line, counted_units }],
          reason: "Shelf count",
        }).success,
      ).toBe(false);
    },
  );

  it("refuses client authority and frozen book quantities in a count", () => {
    const input = {
      expected_version: 0,
      counts: [{ line_id: line, counted_units: 1 }],
      reason: "Shelf count",
    };
    expect(
      pharmacyStocktakeCountInputSchema.safeParse({ ...input, actor_subject: "other" }).success,
    ).toBe(false);
    expect(
      pharmacyStocktakeCountInputSchema.safeParse({
        ...input,
        counts: [{ line_id: line, counted_units: 1, book_units: 1 }],
      }).success,
    ).toBe(false);
  });
});
