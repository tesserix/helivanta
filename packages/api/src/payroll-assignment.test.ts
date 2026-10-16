import { expect, it } from "vitest";
import type { z } from "zod";
import {
  salaryAssignmentAmountsFor,
  salaryAssignmentFormSchema,
  salaryAssignmentInputContract,
  salaryAssignmentsPath,
  staffPayPath,
  staffSalaryPreviewFormSchema,
  staffSalaryPreviewInputContract,
  staffSalaryPreviewContract,
  taxRegimeElectionContract,
  taxRegimeInputContract,
  taxRegimeSchemaFor,
  toSalaryAssignmentInput,
  toStaffSalaryPreviewInput,
  toTaxRegimeInput,
  type TaxRegimeElection,
} from "./payroll";

const structure = "11111111-1111-4111-8111-111111111111";
const basic = "22222222-2222-4222-8222-222222222222";
const hra = "33333333-3333-4333-8333-333333333333";
const pf = "44444444-4444-4444-8444-444444444444";
const issues = (schema: z.ZodTypeAny, v: object) =>
  schema.safeParse(v).error?.issues.map((i) => [i.path.join("."), i.message]);

it("keeps only the amounts a person overrides and sends the contract body", () => {
  const lines = [
    { head_id: basic, basis: "fixed" },
    { head_id: hra, basis: "fixed" },
    { head_id: pf, basis: "percent" },
  ];
  expect(salaryAssignmentAmountsFor(lines)).toEqual([
    { head_id: basic, amount: "" },
    { head_id: hra, amount: "" },
  ]);
  expect(
    salaryAssignmentAmountsFor(lines, { amounts: [{ head_id: basic, amount_paise: 3500050 }] }),
  ).toEqual([
    { head_id: basic, amount: "35000.50" },
    { head_id: hra, amount: "" },
  ]);
  const form = salaryAssignmentFormSchema.parse({
    structure_id: structure,
    effective_on: "2026-04-01",
    reason: "  Synthetic appointment  ",
    amounts: [
      { head_id: basic, amount: "35,000.5" },
      { head_id: hra, amount: " " },
    ],
  });
  const input = toSalaryAssignmentInput(form, 2);
  expect(input).toEqual({
    expected_version: 2,
    structure_id: structure,
    effective_on: "2026-04-01",
    reason: "Synthetic appointment",
    amounts: [{ head_id: basic, amount_paise: 3500050 }],
  });
  expect(salaryAssignmentInputContract.parse(input)).toEqual(input);
});

it("refuses a proposal without a structure, date or reason, and malformed amounts", () => {
  expect(
    issues(salaryAssignmentFormSchema, {
      structure_id: "",
      effective_on: "2026-02-30",
      reason: " ",
      amounts: [{ head_id: basic, amount: "12.345" }],
    }),
  ).toEqual([
    ["structure_id", "Choose a structure."],
    ["effective_on", "Choose a valid date."],
    ["reason", "Enter a reason."],
    ["amounts.0.amount", "Enter rupees with up to two decimals, or leave blank."],
  ]);
  expect(
    issues(salaryAssignmentFormSchema, {
      structure_id: structure,
      effective_on: "2026-04-01",
      reason: "x".repeat(501),
      amounts: [],
    }),
  ).toEqual([["reason", "Keep a reason within 500 characters."]]);
});

it("bounds staff preview days by the month and omits an unstated gender", () => {
  const form = staffSalaryPreviewFormSchema.parse({
    on: "2026-03-15",
    payable_days: "30.5",
    disability: false,
    gender: "",
  });
  const input = toStaffSalaryPreviewInput(form);
  expect(input).toEqual({ on: "2026-03-15", payable_half_days: 61, disability: false });
  expect(staffSalaryPreviewInputContract.parse(input)).toEqual(input);
  expect(toStaffSalaryPreviewInput({ ...form, gender: "female", disability: true })).toMatchObject({
    gender: "female",
    disability: true,
  });
  expect(
    issues(staffSalaryPreviewFormSchema, {
      on: "2026-02-10",
      payable_days: "28.5",
      disability: false,
      gender: "",
    }),
  ).toEqual([["payable_days", "Use no more than the 28 days in that month."]]);
  expect(staffPayPath("/b", basic)).toBe(`/b/hr/payroll/staff/${basic}`);
  expect(salaryAssignmentsPath("/b")).toBe("/b/hr/payroll/salary-assignments");
});

it("reports an unelected regime without assuming one", () => {
  const shape = staffSalaryPreviewContract.shape;
  expect(shape.tax_regime_status.options).toEqual(["elected", "regime_not_elected"]);
  expect(shape.tax_regime.safeParse(undefined).success).toBe(true);
  expect(shape.tax_regime.safeParse("default").success).toBe(false);
});

it("validates the tax year and intimation date, and needs a note to change an election", () => {
  const record = taxRegimeSchemaFor();
  const form = record.parse({
    tax_year: "2026-27",
    regime: "new",
    intimated_on: "2027-03-31",
    note: "  ",
  });
  const input = toTaxRegimeInput(form, 0);
  expect(input).toEqual({
    expected_version: 0,
    regime: "new",
    intimated_on: "2027-03-31",
    note: "",
  });
  expect(taxRegimeInputContract.parse(input)).toEqual(input);
  expect(
    issues(record, { tax_year: "2026-28", regime: "new", intimated_on: "2026-04-01", note: "" }),
  ).toEqual([["tax_year", "Write the tax year as 2026-27."]]);
  expect(
    issues(record, { tax_year: "1999-00", regime: "new", intimated_on: "2026-04-01", note: "" }),
  ).toEqual([["tax_year", "Write the tax year as 2026-27."]]);
  expect(
    issues(record, { tax_year: "2099-00", regime: "old", intimated_on: "2099-04-01", note: "" }),
  ).toBeUndefined();
  expect(
    issues(record, { tax_year: "2026-27", regime: "new", intimated_on: "2027-04-01", note: "" }),
  ).toEqual([["intimated_on", "Use a date no later than 31 March of that tax year."]]);
  expect(
    issues(record, { tax_year: "2026-27", regime: "", intimated_on: "2026-04-01", note: "" }),
  ).toEqual([["regime", "Choose the regime the employee intimated."]]);

  const current: TaxRegimeElection = taxRegimeElectionContract.parse({
    id: hra,
    staff_id: basic,
    tax_year: "2026-27",
    regime: "new",
    intimated_on: "2026-04-03",
    version: 1,
    created_at: "2026-04-03T00:00:00Z",
    updated_at: "2026-04-03T00:00:00Z",
  });
  const change = taxRegimeSchemaFor(current);
  expect(
    issues(change, { tax_year: "2026-27", regime: "old", intimated_on: "2026-04-03", note: " " }),
  ).toEqual([["note", "Give a reason for changing the recorded regime."]]);
  expect(
    issues(change, { tax_year: "2026-27", regime: "new", intimated_on: "2026-04-20", note: "" }),
  ).toEqual([["note", "Give a reason for changing the recorded regime."]]);
  expect(
    issues(change, { tax_year: "2026-27", regime: "new", intimated_on: "2026-04-03", note: "" }),
  ).toBeUndefined();
  expect(
    issues(change, {
      tax_year: "2026-27",
      regime: "old",
      intimated_on: "2026-04-03",
      note: "x".repeat(501),
    }),
  ).toEqual([["note", "Keep the note within 500 characters."]]);
});
