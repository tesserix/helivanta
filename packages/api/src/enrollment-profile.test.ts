import { describe, expect, it } from "vitest";
import {
  EMPTY_PROFILE,
  enrollmentProfileSchema,
  EMPTY_QUALIFICATIONS,
  enrollmentQualificationSchema,
  EMPTY_EMERGENCY_CONTACTS,
  enrollmentContactSchema,
  ENROLLMENT_CONTACT_RELATIONSHIP_MAX_LENGTH,
} from "./enrollment-fields";
import { hrMasterSchema } from "./hr-masters";

const revision = "11111111-2222-4333-8444-555555555555";

describe("profile master contract", () => {
  it.each([
    [
      "HR master",
      (label: string) =>
        hrMasterSchema.safeParse({
          expected_version: 0,
          kind: "language",
          code: "SYN-UNICODE",
          name: label,
          description: "",
          active: true,
        }).success,
    ],
    [
      "staff language",
      (label: string) =>
        enrollmentProfileSchema.safeParse({
          ...EMPTY_PROFILE,
          preferred_language: label,
          preferred_language_revision_id: revision,
        }).success,
    ],
  ] as const)("uses the owner's 80-code-point label bound for %s", (_, accepts) => {
    expect(accepts("𑀓".repeat(80))).toBe(true);
    expect(accepts("𑀓".repeat(81))).toBe(false);
    expect(accepts("A".repeat(81))).toBe(false);
  });

  it("requires a contract type reference for a supplied contract label", () => {
    const result = enrollmentProfileSchema.safeParse({
      ...EMPTY_PROFILE,
      contract_type: "Synthetic fixed term",
    });
    expect(result.success).toBe(false);
    if (result.success) throw new Error("Unreferenced contract classification accepted");
    expect(result.error.issues).toEqual(
      expect.arrayContaining([expect.objectContaining({ path: ["contract_type_revision_id"] })]),
    );
    expect(
      enrollmentProfileSchema.safeParse({ ...EMPTY_PROFILE, contract_type_revision_id: revision })
        .success,
    ).toBe(true);
  });
  it("accepts an older profile without synthesizing explicit clears for new fields", () => {
    const legacy: Record<string, unknown> = { ...EMPTY_PROFILE };
    for (const field of [
      "preferred_language_revision_id",
      "exit_reason_revision_id",
      "contract_type",
      "contract_type_revision_id",
    ])
      delete legacy[field];
    const parsed = enrollmentProfileSchema.safeParse(legacy);
    expect(parsed.success).toBe(true);
    if (!parsed.success) throw new Error("Older profile input was rejected");
    expect(JSON.parse(JSON.stringify(parsed.data))).toEqual(legacy);
  });
});

describe("qualification master contract", () => {
  const qualification = {
    ...EMPTY_QUALIFICATIONS,
    id: revision,
    qualification: "Synthetic qualification",
    institution: "Synthetic University",
  };
  it("preserves older input omission rather than manufacturing an explicit clear", () => {
    const legacy: Record<string, unknown> = { ...qualification };
    delete legacy.qualification_revision_id;
    expect(enrollmentQualificationSchema.parse(legacy)).toEqual(legacy);
  });
  it("accepts an explicit UUID or clear and rejects null and malformed references", () => {
    for (const value of [revision, ""])
      expect(
        enrollmentQualificationSchema.safeParse({
          ...qualification,
          qualification_revision_id: value,
        }).success,
      ).toBe(true);
    for (const value of [null, "invalid"])
      expect(
        enrollmentQualificationSchema.safeParse({
          ...qualification,
          qualification_revision_id: value,
        }).success,
      ).toBe(false);
  });
});

describe("contact master contract", () => {
  const contact = {
    ...EMPTY_EMERGENCY_CONTACTS,
    id: revision,
    name: "Synthetic contact",
    relationship: "Synthetic support",
    phone: "+61400000001",
  };
  it("preserves legacy omission instead of manufacturing an explicit clear", () => {
    const legacy: Record<string, unknown> = { ...contact };
    delete legacy.relationship_revision_id;
    expect(enrollmentContactSchema.parse(legacy)).toEqual(legacy);
  });
  it("keeps the existing sixty-code-point relationship bound", () => {
    expect(ENROLLMENT_CONTACT_RELATIONSHIP_MAX_LENGTH).toBe(60);
    expect(
      enrollmentContactSchema.safeParse({ ...contact, relationship: "𑀓".repeat(60) }).success,
    ).toBe(true);
    expect(
      enrollmentContactSchema.safeParse({ ...contact, relationship: "𑀓".repeat(61) }).success,
    ).toBe(false);
  });
  it("accepts UUID or explicit clear and rejects null or malformed references", () => {
    for (const value of [revision, ""])
      expect(
        enrollmentContactSchema.safeParse({ ...contact, relationship_revision_id: value }).success,
      ).toBe(true);
    for (const value of [null, "invalid"])
      expect(
        enrollmentContactSchema.safeParse({ ...contact, relationship_revision_id: value }).success,
      ).toBe(false);
  });
});
