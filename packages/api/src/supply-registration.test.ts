import { expect, it } from "vitest";
import { supplyPremiseInputSchema, supplyLicenseInputSchema } from "./supply-contract";

const address = {
  line1: "TEST Warehouse",
  line2: "",
  city: "TEST City",
  region: "TEST Region",
  postal_code: "TEST-001",
  country_code: "IN",
};
it("captures standalone premises without accepting tenant or hospital overrides", () => {
  const input = {
    ...address,
    expected_version: 0,
    code: " test-depot ",
    name: "TEST Depot",
    kind: "warehouse",
    time_zone: "Asia/Kolkata",
    status: "active",
  };
  expect(supplyPremiseInputSchema.parse(input).code).toBe("TEST-DEPOT");
  expect(
    supplyPremiseInputSchema.safeParse({ ...input, tenant_id: crypto.randomUUID() }).success,
  ).toBe(false);
  expect(
    supplyPremiseInputSchema.safeParse({ ...input, hospital_id: crypto.randomUUID() }).success,
  ).toBe(false);
  expect(supplyPremiseInputSchema.safeParse({ ...input, kind: "unknown" }).success).toBe(false);
});
it("requires declared licence identity, full calendar dates and bounded product scope", () => {
  const input = {
    expected_version: 0,
    kind: "WHOLESALE",
    number: "TEST-LIC-001",
    issuing_authority: "TEST Authority",
    jurisdiction: "TEST-MH",
    premise_id: "0199e000-0000-7000-8000-000000000001",
    issued_on: "2026-01-01",
    expires_on: "2030-12-31",
    product_categories: ["otc", "prescription"],
  };
  expect(supplyLicenseInputSchema.parse(input)).toEqual(input);
  expect(supplyLicenseInputSchema.safeParse({ ...input, expires_on: "2026-02-30" }).success).toBe(
    false,
  );
  expect(supplyLicenseInputSchema.safeParse({ ...input, product_categories: [] }).success).toBe(
    false,
  );
  expect(
    supplyLicenseInputSchema.safeParse({ ...input, product_categories: ["unrestricted"] }).success,
  ).toBe(false);
  expect(supplyLicenseInputSchema.safeParse({ ...input, status: "verified" }).success).toBe(false);
});

it("rejects cross-country operating time zones, reversed validity and duplicate product scope", async () => {
  const { supplyLicenseFormSchema, supplyPremiseFormSchema } =
    await import("./supply-registration");
  const premise = {
    ...address,
    expected_version: 0,
    code: "TEST",
    name: "TEST Depot",
    kind: "warehouse",
    time_zone: "Asia/Karachi",
    status: "active",
  };
  expect(supplyPremiseFormSchema.safeParse(premise).success).toBe(false);
  const licence = {
    expected_version: 0,
    kind: "WHOLESALE",
    number: "TEST",
    issuing_authority: "TEST Authority",
    jurisdiction: "TEST",
    premise_id: "0199e000-0000-7000-8000-000000000001",
    issued_on: "2026-01-01",
    expires_on: "2025-12-31",
    product_categories: ["otc"],
  };
  expect(supplyLicenseFormSchema.safeParse(licence).success).toBe(false);
  expect(
    supplyLicenseFormSchema.safeParse({
      ...licence,
      expires_on: "2030-12-31",
      product_categories: ["otc", "otc"],
    }).success,
  ).toBe(false);
});

it("accepts scoped branch references, explicit unlinking and old-client omission", () => {
  const input = {
    ...address,
    expected_version: 0,
    code: "TEST-WH",
    name: "TEST Warehouse",
    kind: "warehouse",
    time_zone: "Asia/Kolkata",
    status: "active",
  };
  const branch = "0199e000-0000-7000-8000-000000000001";
  expect(supplyPremiseInputSchema.parse({ ...input, branch_premise_id: branch })).toMatchObject({
    branch_premise_id: branch,
  });
  expect(supplyPremiseInputSchema.parse({ ...input, branch_premise_id: null })).toMatchObject({
    branch_premise_id: null,
  });
  expect(supplyPremiseInputSchema.parse(input)).not.toHaveProperty("branch_premise_id");
  expect(
    supplyPremiseInputSchema.safeParse({ ...input, branch_premise_id: crypto.randomUUID() })
      .success,
  ).toBe(false);
});
