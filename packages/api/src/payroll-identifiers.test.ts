import { expect, it } from "vitest";
import type { z } from "zod";
import {
  staffIdentifierRevealFormSchema,
  staffIdentifierRevealInputContract,
  staffIdentifiersFormSchema,
  staffIdentifiersInputContract,
  staffIdentifiersPath,
  toStaffIdentifiersInput,
  type StaffIdentifiersForm,
} from "./payroll";

// Synthetic identifiers only; none belongs to a real person.
const blank: StaffIdentifiersForm = {
  reason: "Synthetic onboarding",
  bank: "keep",
  account_number: "",
  ifsc: "",
  holder_name: "",
  pan: "keep",
  pan_value: "",
  uan: "keep",
  uan_value: "",
};
const issues = (schema: z.ZodTypeAny, v: object) =>
  schema.safeParse(v).error?.issues.map((i) => [i.path.join("."), i.message]);

it("builds the identifiers path under the staff member's pay", () => {
  expect(staffIdentifiersPath("/workforce/hospitals/h", "a b")).toBe(
    "/workforce/hospitals/h/hr/payroll/staff/a%20b/identifiers",
  );
});

it("normalises set values and sends only actions for the rest", () => {
  const form = {
    ...blank,
    bank: "set",
    account_number: " 0001 1122 2333 ",
    ifsc: " synt0000001 ",
    holder_name: " Synthetic Holder ",
    pan: "set",
    pan_value: " aaapz1234z ",
    uan: "clear",
  } as const;
  expect(staffIdentifiersFormSchema.safeParse(form).success).toBe(true);
  const input = toStaffIdentifiersInput(form, 3);
  expect(input).toEqual({
    expected_version: 3,
    reason: "Synthetic onboarding",
    bank: {
      action: "set",
      account_number: "000111222333",
      ifsc: "SYNT0000001",
      holder_name: "Synthetic Holder",
    },
    pan: { action: "set", value: "AAAPZ1234Z" },
    uan: { action: "clear" },
  });
  expect(staffIdentifiersInputContract.safeParse(input).success).toBe(true);
  const keepOnly = toStaffIdentifiersInput(
    { ...blank, uan: "set", uan_value: "1000 0000 0001" },
    0,
  );
  expect(keepOnly.bank).toEqual({ action: "keep" });
  expect(keepOnly.uan).toEqual({ action: "set", value: "100000000001" });
  expect(staffIdentifiersInputContract.safeParse(keepOnly).success).toBe(true);
});

it("names each malformed field and refuses a proposal that changes nothing", () => {
  expect(
    issues(staffIdentifiersFormSchema, {
      ...blank,
      bank: "set",
      account_number: "12345678",
      ifsc: "SYNT1000001",
      holder_name: " ",
      pan: "set",
      pan_value: "AAACZ1234Z",
      uan: "set",
      uan_value: "10000000001",
    }),
  ).toEqual([
    ["account_number", "Enter the 9 to 18 digit account number."],
    ["ifsc", "Enter the 11-character IFSC, such as ABCD0123456."],
    ["holder_name", "Enter the account holder's name, up to 150 characters."],
    ["pan_value", "Enter the individual's 10-character PAN; its fourth letter is P."],
    ["uan_value", "Enter the 12-digit UAN."],
  ]);
  expect(issues(staffIdentifiersFormSchema, { ...blank, reason: " " })).toEqual([
    ["reason", "Enter a reason."],
    ["bank", "Replace or remove at least one identifier."],
  ]);
  expect(
    issues(staffIdentifiersFormSchema, { ...blank, bank: "keep", pan: "keep", uan: "clear" }),
  ).toBeUndefined();
  expect(
    issues(staffIdentifiersFormSchema, { ...blank, pan: "keep", pan_value: "ignored" }),
  ).toEqual([["bank", "Replace or remove at least one identifier."]]);
});

it("requires a bounded reason to reveal a value", () => {
  expect(issues(staffIdentifierRevealFormSchema, { reason: " " })).toEqual([
    ["reason", "Enter why you need to see it."],
  ]);
  expect(issues(staffIdentifierRevealFormSchema, { reason: "x".repeat(201) })).toEqual([
    ["reason", "Keep why you need to see it within 200 characters."],
  ]);
  expect(
    staffIdentifierRevealInputContract.safeParse({
      revision_id: "11111111-1111-4111-8111-111111111111",
      field: "pan",
      reason: "Synthetic payout check",
    }).success,
  ).toBe(true);
});
