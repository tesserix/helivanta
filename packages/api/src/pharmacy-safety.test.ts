import { describe, expect, it } from "vitest";
import { pharmacySafetyIncidentInputSchema } from "./pharmacy";

const input = {
  batch_id: "019a1b62-0000-7000-8000-000000000001",
  kind: "temperature_excursion",
  reason: "TEST refrigerator temperature excursion",
  occurred_at: "2026-01-01T10:00:00.123456+05:30",
  measured_temperature_millicelsius: 12000,
};

describe("pharmacy safety contract", () => {
  it("accepts measured excursions and quality concerns without a measurement", () => {
    expect(pharmacySafetyIncidentInputSchema.parse(input)).toEqual(input);
    expect(
      pharmacySafetyIncidentInputSchema.safeParse({
        ...input,
        kind: "quality_concern",
        measured_temperature_millicelsius: undefined,
      }).success,
    ).toBe(true);
  });
  it.each([
    { measured_temperature_millicelsius: undefined },
    { measured_temperature_millicelsius: -273151 },
    { measured_temperature_millicelsius: 1000001 },
    { measured_temperature_millicelsius: 12.5 },
    { kind: "quality_concern" },
    { kind: "release" },
    { reason: "short" },
    { reason: "x".repeat(501) },
    { occurred_at: "0001-01-01T00:00:00+01:00" },
    { occurred_at: "2026-01-01T10:00:00.1234560Z" },
    { occurred_at: "2026-01-01T10:00:00.1234560001Z" },
    { occurred_at: "2026-01-01T10:00:00,123456Z" },
    { occurred_at: "2026-01-01T10:00:00+24:00" },
    { actor_subject: "forged" },
  ])("rejects invalid or contradictory details %j", (change) => {
    expect(pharmacySafetyIncidentInputSchema.safeParse({ ...input, ...change }).success).toBe(
      false,
    );
  });
});
