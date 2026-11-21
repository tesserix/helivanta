import { expect, it } from "vitest";
import {
  pharmacySupplierCreditDocumentInputSchema,
  pharmacySupplierCreditVoidInputSchema,
} from "./pharmacy-contract";

const input = {
  return_id: "0199e000-0000-7000-8000-000000000001",
  document_reference: " test-cn-1 ",
  issued_on: "2026-11-16",
  amount_minor: 750,
  currency: "INR",
  reason: " TEST actual supplier document ",
};
it("requires explicit actual credit facts independently of expected return values", () => {
  expect(pharmacySupplierCreditDocumentInputSchema.parse(input)).toMatchObject({
    document_reference: "TEST-CN-1",
    amount_minor: 750,
    reason: "TEST actual supplier document",
  });
  expect(
    pharmacySupplierCreditDocumentInputSchema.parse({ ...input, amount_minor: 1000000000000 })
      .amount_minor,
  ).toBe(1000000000000);
  for (const change of [
    { amount_minor: undefined },
    { amount_minor: 0 },
    { amount_minor: 1000000000001 },
    { issued_on: "1969-12-31" },
    { issued_on: "2026-02-30" },
    { currency: "USD" },
    { recorded_by: "TEST forged actor" },
    { correction_of: "00000000-0000-0000-0000-000000000000" },
  ]) {
    expect(() =>
      pharmacySupplierCreditDocumentInputSchema.parse({ ...input, ...change }),
    ).toThrow();
  }
});
it("requires a current positive version and reason to void retained credit", () => {
  expect(
    pharmacySupplierCreditVoidInputSchema.parse({
      expected_version: 1,
      reason: " TEST correction ",
    }),
  ).toEqual({ expected_version: 1, reason: "TEST correction" });
  for (const value of [
    { reason: "TEST correction" },
    { expected_version: 0, reason: "TEST correction" },
    { expected_version: 1, reason: " " },
  ]) {
    expect(() => pharmacySupplierCreditVoidInputSchema.parse(value)).toThrow();
  }
});
