import { describe, expect, it } from "vitest";
import {
  pharmacyStocktakeCountInputSchema,
  pharmacyStocktakeCountReceiptSchema,
  pharmacyStocktakeDecisionInputSchema,
  pharmacyStocktakeStartInputSchema,
  pharmacyStocktakeSchema,
} from "./pharmacy-contract";

const location = "0199e000-0000-7000-8000-000000000001";
const line = "0199e000-0000-7000-8000-000000000002";

describe("stocktake input contracts", () => {
  it("provides exact extra-unit progress and the current actor's review relationship", () => {
    const detail = {
      id: line,
      outlet_id: location,
      number: 1,
      state: "submitted",
      version: 2,
      selection: "all",
      product_ids: [],
      quarantine_location_id: location,
      reason: "TEST snapshot",
      started_by: "TEST starter",
      started_at: "2026-11-20T00:00:00Z",
      snapshot_rows: 1,
      counted_rows: 1,
      difference_rows: 1,
      positive_difference_rows: 1,
      review_relationship: "counter",
    };
    expect(pharmacyStocktakeSchema.parse(detail)).toEqual(detail);
    expect(
      pharmacyStocktakeSchema.safeParse({ ...detail, review_relationship: "owner" }).success,
    ).toBe(false);
  });
  it("retains a reviewed decision and an explicit quarantine attestation", () => {
    const input = {
      decision: "approve",
      expected_version: 2,
      reason: " Independent review ",
      extra_units_disposition: "moved_to_quarantine",
    };
    expect(pharmacyStocktakeDecisionInputSchema.parse(input)).toEqual({
      ...input,
      reason: "Independent review",
    });
    expect(
      pharmacyStocktakeDecisionInputSchema.safeParse({ ...input, actor_subject: "another" })
        .success,
    ).toBe(false);
    expect(
      pharmacyStocktakeDecisionInputSchema.safeParse({
        ...input,
        extra_units_disposition: "sellable",
      }).success,
    ).toBe(false);
  });
  it("retains the original counted observation in its versioned command receipt", () => {
    const receipt = {
      id: line,
      outlet_id: location,
      stocktake_id: "0199e000-0000-7000-8000-000000000003",
      expected_version: 0,
      version: 1,
      state: "counting",
      recorded_by: "TEST-counter",
      recorded_at: "2026-11-20T08:00:00Z",
      counts: [{ line_id: line, counted_units: 0 }],
      reason: "Shelf observation",
    };
    expect(pharmacyStocktakeCountReceiptSchema.parse(receipt)).toEqual(receipt);
    expect(pharmacyStocktakeCountReceiptSchema.safeParse({ ...receipt, version: 0 }).success).toBe(
      false,
    );
    expect(
      pharmacyStocktakeCountReceiptSchema.safeParse({ ...receipt, state: "posted" }).success,
    ).toBe(false);
  });
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
