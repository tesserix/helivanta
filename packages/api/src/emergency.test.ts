import { describe, expect, it } from "vitest";
import { emergencyIntakeSchema, emergencyBaseSchema, emergencyClosureSchema } from "./emergency";

describe("emergency intake boundaries", () => {
  const report = {
    channel: "phone",
    requested_service: "unknown",
    processing_basis: "medical_emergency",
    contact: { caller_name: "", callback_phone: "", relationship: "unknown" },
    incident: { location_text: "", patient_description: "" },
  };
  it("accepts unknown people without a patient identifier", () => {
    expect(emergencyIntakeSchema.parse(report)).toEqual(report);
  });
  it("refuses elective transport under the medical emergency basis", () => {
    expect(
      emergencyIntakeSchema.safeParse({ ...report, requested_service: "mortuary" }).success,
    ).toBe(false);
    expect(
      emergencyIntakeSchema.safeParse({
        ...report,
        requested_service: "patient_transport",
        processing_basis: "consent",
      }).success,
    ).toBe(true);
  });
  it("rejects invalid phones, controls and oversized narratives", () => {
    expect(
      emergencyIntakeSchema.safeParse({
        ...report,
        contact: { ...report.contact, callback_phone: "112x" },
      }).success,
    ).toBe(false);
    expect(
      emergencyIntakeSchema.safeParse({
        ...report,
        incident: { location_text: "gate\u0000", patient_description: "" },
      }).success,
    ).toBe(false);
    expect(
      emergencyIntakeSchema.safeParse({
        ...report,
        incident: { location_text: "", patient_description: "a".repeat(1501) },
      }).success,
    ).toBe(false);
  });
  it("requires an actual timezone and reviewed policy reference", () => {
    const base = {
      name: "Synthetic Base",
      timezone: "Asia/Kolkata",
      policy_reference: "SYN-01",
      contact_retention_days: 30,
    };
    expect(emergencyBaseSchema.safeParse(base).success).toBe(true);
    expect(emergencyBaseSchema.safeParse({ ...base, timezone: "wrong" }).success).toBe(false);
    expect(emergencyBaseSchema.safeParse({ ...base, contact_retention_days: 0 }).success).toBe(
      false,
    );
  });
  it("requires a supervisor rationale and current version", () => {
    expect(
      emergencyClosureSchema.safeParse({ version: 1, reason: "entered_in_error", explanation: "" })
        .success,
    ).toBe(false);
    expect(
      emergencyClosureSchema.safeParse({
        version: 1,
        reason: "entered_in_error",
        explanation: "Synthetic correction reviewed",
      }).success,
    ).toBe(true);
  });
});
