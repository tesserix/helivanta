import { describe, expect, it } from "vitest";
import {
  supplyAccessGrantInputSchema,
  supplyCorporateInputSchema,
  supplyTerritoryInputSchema,
} from "./supply-contract";

describe("Supply operations contract", () => {
  it("accepts explicit corporate unlink and rejects unknown properties", () => {
    expect(
      supplyCorporateInputSchema.parse({ expected_version: 2, parent_party_id: null })
        .parent_party_id,
    ).toBeNull();
    expect(
      supplyCorporateInputSchema.safeParse({ expected_version: 0, administrator: true }).success,
    ).toBe(false);
  });

  it("validates representative assignments without accepting duplicated identities", () => {
    const territory = {
      expected_version: 0,
      code: "WEST",
      name: "West",
      country_code: "IN",
      region: "Maharashtra",
      city: "",
      description: "",
      status: "active",
      branch_premise_id: null,
      assigned_sales_subjects: ["rep", "rep"],
    };
    expect(supplyTerritoryInputSchema.safeParse(territory).success).toBe(false);
  });

  it("rejects malformed and reversed grant validity intervals", () => {
    const grant = {
      expected_version: 0,
      subject: "member",
      role: "sales",
      premise_id: null,
      reason: "Approved sales assignment",
      status: "active",
      valid_from: "2026-10-17T00:00:00Z",
      expires_at: "2026-10-18T00:00:00Z",
    };
    expect(supplyAccessGrantInputSchema.safeParse(grant).success).toBe(true);
    expect(
      supplyAccessGrantInputSchema.safeParse({ ...grant, valid_from: "not-a-real-timestamp-----" })
        .success,
    ).toBe(false);
    expect(
      supplyAccessGrantInputSchema.safeParse({ ...grant, expires_at: "2026-10-16T00:00:00Z" })
        .success,
    ).toBe(false);
  });
});
