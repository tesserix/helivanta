import { expect, it, vi } from "vitest";
import {
  emergencyVehicleSchema,
  emergencyReadinessSchema,
  emergencyDutySchema,
  emergencyLocalTime,
} from "./emergency-fleet";

it("requires an explicit reviewed oxygen threshold and bounded readiness policy", () => {
  const input = {
    version: 0,
    code: "SYN-ALS-1",
    vehicle_class: "als",
    certified_until: "2026-10-20T12:00:00Z",
    policy_reference: "SYN-POLICY",
    oxygen_min_bar: 100,
    readiness_minutes: 480,
    retired: false,
  };
  expect(emergencyVehicleSchema.safeParse(input).success).toBe(true);
  for (const patch of [
    { oxygen_min_bar: 0 },
    { readiness_minutes: 1441 },
    { policy_reference: "" },
    { code: "Patient Name" },
    { version: -1 },
  ]) {
    expect(emergencyVehicleSchema.safeParse({ ...input, ...patch }).success).toBe(false);
  }
});

it("rejects device wall times skipped or repeated by daylight saving", () => {
  vi.stubEnv("TZ", "Australia/Melbourne");
  try {
    const input = {
      version: 0,
      code: "SYN-ALS-1",
      vehicle_class: "als",
      policy_reference: "SYN-POLICY",
      oxygen_min_bar: 100,
      readiness_minutes: 480,
      retired: false,
    };
    for (const certified_until of ["2026-10-04T02:30", "2026-04-05T02:30", "2026-02-30T12:00"]) {
      expect(emergencyVehicleSchema.safeParse({ ...input, certified_until }).success).toBe(false);
    }
    expect(emergencyLocalTime("2026-10-20T12:00:45.123")).toBe("2026-10-20T01:00:45.123Z");
  } finally {
    vi.unstubAllEnvs();
  }
});
it("records explicit failed checks and rejects omitted observations", () => {
  const input = {
    vehicle_id: "0199abc0-0000-7000-8000-000000000001",
    vehicle_version: 1,
    duty_id: "0199abc0-0000-7000-8000-000000000002",
    oxygen_bar: 0,
    defibrillator_passed: false,
    equipment_complete: false,
    roadworthy: false,
  };
  expect(emergencyReadinessSchema.parse(input)).toEqual(input);
  expect(emergencyReadinessSchema.safeParse({ ...input, roadworthy: undefined }).success).toBe(
    false,
  );
  expect(emergencyReadinessSchema.safeParse({ ...input, oxygen_bar: 301 }).success).toBe(false);
});
it("rejects a duty assigning one person to both roles", () => {
  expect(
    emergencyDutySchema.safeParse({
      vehicle_id: "0199abc0-0000-7000-8000-000000000001",
      driver_subject: "same-subject",
      clinician_subject: "same-subject",
      ends_at: "2026-10-20T12:00:00Z",
    }).success,
  ).toBe(false);
});
