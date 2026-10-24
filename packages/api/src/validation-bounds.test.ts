import { describe, expect, it } from "vitest";
import { pharmacyProductFieldsSchema } from "./pharmacy-contract";

describe("bounded contract validation", () => {
  it("rejects an oversized ingredient list before allocating per-element errors", () => {
    const result = pharmacyProductFieldsSchema.shape.ingredients.safeParse(Array(1000).fill(null));
    expect(result.success).toBe(false);
    if (result.success) throw new Error("Oversized ingredients were accepted");
    expect(result.error.issues).toHaveLength(1);
    expect(result.error.issues[0]).toMatchObject({ code: "too_big", maximum: 20, path: [] });
  });
});
