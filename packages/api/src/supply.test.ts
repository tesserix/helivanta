import { expect, it } from "vitest";
import { newSupplyId, supplyKey, supplyPartyFormSchema, newSupplyPartyInput } from "./supply";

it("normalises private supplier input without accepting tenant or approval overrides", () => {
  const input = {
    ...newSupplyPartyInput(),
    code: " test-one ",
    legal_name: " TEST Supplier ",
    jurisdiction: "TEST-MH",
    registration_number: "test-reg-001",
  };
  const parsed = supplyPartyFormSchema.parse(input);
  expect(parsed.code).toBe("TEST-ONE");
  expect(parsed.registration_number).toBe("TEST-REG-001");
  expect(supplyPartyFormSchema.safeParse({ ...input, tenant_id: newSupplyId() }).success).toBe(
    false,
  );
  expect(supplyPartyFormSchema.safeParse({ ...input, onboarding_status: "approved" }).success).toBe(
    false,
  );
});

it("rejects duplicate contact identities and country-incompatible declarations", () => {
  const input = {
    ...newSupplyPartyInput(),
    code: "TEST-ONE",
    legal_name: "TEST Supplier",
    jurisdiction: "TEST-MH",
    registration_number: "TEST-REG-001",
  };
  const contact = {
    id: newSupplyId(),
    name: "TEST Contact",
    role: "general" as const,
    email: "contact@example.test",
    phone: "+12025550101",
    alternate_phone: "",
  };
  expect(supplyPartyFormSchema.safeParse({ ...input, contacts: [contact, contact] }).success).toBe(
    false,
  );
  expect(
    supplyPartyFormSchema.safeParse({
      ...input,
      contacts: [{ ...contact, id: crypto.randomUUID() }],
    }).success,
  ).toBe(false);
  expect(
    supplyPartyFormSchema.safeParse({ ...input, contacts: [{ ...contact, phone: "123" }] }).success,
  ).toBe(false);
  expect(
    supplyPartyFormSchema.safeParse({
      ...input,
      tax_registrations: [
        { id: newSupplyId(), kind: "ntn", number: "TEST-TAX", jurisdiction: "TEST" },
      ],
    }).success,
  ).toBe(false);
  // #1287: Pakistan, and its identifier kinds, are not supported.
  expect(
    supplyPartyFormSchema.safeParse({
      ...input,
      country_code: "PK",
      tax_registrations: [
        { id: newSupplyId(), kind: "ntn", number: "TEST-TAX", jurisdiction: "TEST" },
      ],
    }).success,
  ).toBe(false);
  expect(
    supplyPartyFormSchema.safeParse({
      ...input,
      addresses: [
        {
          id: newSupplyId(),
          kind: "registered",
          line1: "TEST Office",
          line2: "",
          city: "TEST City",
          region: "TEST Region",
          postal_code: "TEST",
          country_code: "PK",
        },
      ],
    }).success,
  ).toBe(false);
});

it("partitions supplier queries by both tenant and signed-in user", () => {
  const scope = { tenantId: newSupplyId(), subject: "TEST-owner" };
  expect(supplyKey(scope, "parties")).not.toEqual(
    supplyKey({ ...scope, tenantId: newSupplyId() }, "parties"),
  );
  expect(supplyKey(scope, "parties")).not.toEqual(
    supplyKey({ ...scope, subject: "TEST-auditor" }, "parties"),
  );
  expect(new Set(Array.from({ length: 100 }, () => newSupplyId())).size).toBe(100);
});
