import { describe, expect, it } from "vitest";
import { supplyAccessGrantInputSchema } from "./supply-contract";

const input = {
  expected_version: 0,
  subject: "TEST-member",
  role: "warehouse",
  premise_id: "019a0000-0000-7000-8000-000000000001",
  valid_from: "2026-10-17T01:00:00Z",
  expires_at: "2026-10-17T12:00:00Z",
  reason: "TEST approved location access",
  status: "active",
};
describe("explicit Supply access scope", () => {
  it("rejects unsupported location roles and requires an exact regulator location", () => {
    for (const role of ["administrator", "purchasing", "finance", "compliance", "sales"])
      expect(supplyAccessGrantInputSchema.safeParse({ ...input, role }).success).toBe(false);
    expect(
      supplyAccessGrantInputSchema.safeParse({ ...input, role: "regulator", premise_id: null })
        .success,
    ).toBe(false);
  });
  it("accepts supported location access and permits revoking historical invalid scope", () => {
    for (const role of [
      "warehouse",
      "inventory",
      "picker",
      "packer",
      "dispatcher",
      "driver",
      "auditor",
      "regulator",
    ])
      expect(supplyAccessGrantInputSchema.safeParse({ ...input, role }).success).toBe(true);
    expect(
      supplyAccessGrantInputSchema.safeParse({ ...input, role: "purchasing", status: "revoked" })
        .success,
    ).toBe(true);
  });
});
