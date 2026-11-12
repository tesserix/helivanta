import { describe, expect, it } from "vitest";
import { accountProfileSchema } from "./account";

const profile = {
  subject: "synthetic-owner",
  tenant_id: "11111111-1111-4111-8111-111111111111",
  display_name: "Synthetic Owner",
  email: "owner@example.test",
  roles: [],
};

describe("Current account tenant name", () => {
  it("preserves legacy profiles without guessing a tenant name", () => {
    expect(accountProfileSchema.parse(profile).tenant_name).toBeUndefined();
  });
  it.each(["Yes Hospital", "Independent Pharmacy", "ଔ".repeat(200), "🏥".repeat(200)])(
    "preserves valid business names (%s)",
    (tenant_name) => {
      expect(accountProfileSchema.parse({ ...profile, tenant_name }).tenant_name).toBe(tenant_name);
    },
  );
  it.each([
    "",
    " Yes Hospital",
    "Yes Hospital ",
    "Yes\nHospital",
    "ଔ".repeat(201),
    "🏥".repeat(201),
  ])("refuses invalid business names (%s)", (tenant_name) => {
    expect(accountProfileSchema.safeParse({ ...profile, tenant_name }).success).toBe(false);
  });
});
