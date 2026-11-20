import { describe, expect, it } from "vitest";
import { pharmacyReorderLevelInputSchema } from "./pharmacy-contract";

const input = { minimum_units: 0, expected_version: 0, product_version: 1 };

describe("reorder level contract", () => {
  it("distinguishes a zero-stock warning from disabling the warning", () => {
    expect(pharmacyReorderLevelInputSchema.parse(input).minimum_units).toBe(0);
    expect(
      pharmacyReorderLevelInputSchema.parse({ ...input, minimum_units: null }).minimum_units,
    ).toBeNull();
  });
  it("rejects missing, fractional, negative, excessive and unknown fields", () => {
    for (const value of [undefined, -1, 0.5, 1000000001, "10", NaN, Infinity]) {
      expect(
        pharmacyReorderLevelInputSchema.safeParse({ ...input, minimum_units: value }).success,
      ).toBe(false);
    }
    for (const change of [
      { expected_version: -1 },
      { expected_version: 0.5 },
      { product_version: 0 },
      { product_version: undefined },
      { expected_version: undefined },
      { tenant_id: "supplied-authority" },
    ]) {
      expect(pharmacyReorderLevelInputSchema.safeParse({ ...input, ...change }).success).toBe(
        false,
      );
    }
  });
});
