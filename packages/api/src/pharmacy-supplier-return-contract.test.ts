import { expect, it } from "vitest";
import type { z } from "zod";
import * as contract from "./pharmacy-contract";

const line = {
  receipt_id: "0199e000-0000-7000-8000-000000000001",
  quarantine_location_id: "0199e000-0000-7000-8000-000000000002",
  quantity_units: 4,
  expected_credit_minor: 12000,
};
const draft = {
  payable_id: "0199e000-0000-7000-8000-000000000003",
  lines: [line],
  reason: "TEST return physically quarantined short-dated stock",
};
function schema() {
  return Reflect.get(contract, "pharmacySupplierReturnDraftInputSchema") as z.ZodType | undefined;
}
it("records an explicit commercial credit expectation without treating it as settlement", () => {
  expect(schema()?.safeParse(draft)).toMatchObject({ success: true, data: draft });
});
it.each([
  ["empty lines", { ...draft, lines: [] }],
  ["too many lines", { ...draft, lines: Array.from({ length: 101 }, () => line) }],
  ["missing reason", { ...draft, reason: undefined }],
  ["missing payable", { ...draft, payable_id: undefined }],
  ["zero units", { ...draft, lines: [{ ...line, quantity_units: 0 }] }],
  ["fractional units", { ...draft, lines: [{ ...line, quantity_units: 1.5 }] }],
  ["excess units", { ...draft, lines: [{ ...line, quantity_units: 1000000001 }] }],
  ["missing credit", { ...draft, lines: [{ ...line, expected_credit_minor: undefined }] }],
  ["negative credit", { ...draft, lines: [{ ...line, expected_credit_minor: -1 }] }],
  ["fractional credit", { ...draft, lines: [{ ...line, expected_credit_minor: 0.5 }] }],
  ["excess credit", { ...draft, lines: [{ ...line, expected_credit_minor: 1000000000001 }] }],
  ["forged supplier", { ...draft, supplier_tenant_id: "TEST-not-authority" }],
])("refuses %s in a supplier-return command", (_name, input) => {
  expect(schema()?.safeParse(input).success).toBe(false);
});
