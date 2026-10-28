import { expect, it } from "vitest";
import {
  describeSalaryHead,
  formatPaise,
  halfDaysToDays,
  newSalaryHeadForm,
  paiseToRupees,
  parseHalfDays,
  parsePercent,
  parseRupees,
  ppmToPercent,
  salaryHeadContract,
  salaryHeadFormSchema,
  salaryHeadInputContract,
  salaryHeadsPath,
  salaryHeadToForm,
  salaryPreviewFormSchema,
  salaryPreviewInputContract,
  salaryStructureInputContract,
  salaryStructureSchemaFor,
  salaryStructuresPath,
  salaryStructureToForm,
  toSalaryHeadInput,
  toSalaryPreviewInput,
  toSalaryStructureInput,
  type SalaryHead,
  type SalaryStructure,
} from "./payroll";
const basic = "11111111-1111-4111-8111-111111111111";
const hra = "22222222-2222-4222-8222-222222222222";
const issues = (
  schema: {
    safeParse: (v: unknown) => {
      error?: { issues: { path: PropertyKey[]; message: string }[] };
    };
  },
  value: object,
) => schema.safeParse(value).error?.issues.map((i) => [i.path.join("."), i.message]);
const fixed = {
  ...newSalaryHeadForm("2026-11-01"),
  code: "syn-basic",
  name: "Synthetic Basic",
  payslip_label: "Basic",
  reviewed: true,
};
const percent = {
  ...fixed,
  kind: "deduction" as const,
  code: "SYN-PF",
  name: "Synthetic PF",
  basis: "percent" as const,
  rate: "12",
  base_head_ids: [basic],
  max_amount: "1800",
};
const record: SalaryHead = {
  id: hra,
  revision_id: basic,
  kind: "earning",
  code: "SYN-HRA",
  name: "Synthetic HRA",
  active: true,
  basis: "percent",
  rate_ppm: 125000,
  base_head_ids: [basic],
  proration: "none",
  minimum_payable_half_days: 31,
  months: [3, 9],
  rounding_mode: "up",
  rounding_unit_paise: 50,
  min_amount_paise: 150,
  in_gross: true,
  in_ctc: true,
  arrears_eligible: false,
  taxability: "exempt",
  tax_note: "Synthetic exemption basis",
  wage_class: "excluded",
  payslip_label: "HRA",
  payslip_order: 20,
  show_when_zero: true,
  effective_on: "2026-11-01",
  reviewed_by_subject: "user-admin",
  version: 3,
  created_at: "2026-10-16T00:00:00Z",
  updated_at: "2026-10-16T00:00:00Z",
};

it("parses rupees, percentages and half days exactly, refusing anything ambiguous", () => {
  expect(parseRupees("1234.5")).toBe(123450);
  expect(parseRupees("0.01")).toBe(1);
  expect(parseRupees(" 1,23,456.78 ")).toBe(12345678);
  expect(parseRupees("10000000")).toBe(1_000_000_000);
  for (const bad of ["10000000.01", "1e3", "-1", "1.234", "", "₹10", "0x10"])
    expect(parseRupees(bad), bad).toBeUndefined();
  expect(parsePercent("12")).toBe(120000);
  expect(parsePercent("12.5")).toBe(125000);
  expect(parsePercent("0.0001")).toBe(1);
  expect(parsePercent("100")).toBe(1_000_000);
  for (const bad of ["0", "100.0001", "12.34567", "-5", "1e2", ""])
    expect(parsePercent(bad), bad).toBeUndefined();
  expect(parseHalfDays("15.5")).toBe(31);
  expect(parseHalfDays("31")).toBe(62);
  expect(parseHalfDays("0")).toBe(0);
  for (const bad of ["31.5", "1.25", "-1", "", "2.0.5"])
    expect(parseHalfDays(bad), bad).toBeUndefined();
});

it("formats paise with Indian grouping and round-trips every display helper", () => {
  expect(formatPaise(12345678)).toBe("₹1,23,456.78");
  expect(formatPaise(5)).toBe("₹0.05");
  expect(formatPaise(-150)).toBe("−₹1.50");
  expect(formatPaise(100_000_000_000)).toBe("₹1,00,00,00,000.00");
  expect(formatPaise(99_900)).toBe("₹999.00");
  expect(paiseToRupees(123450)).toBe("1234.50");
  expect(paiseToRupees(100)).toBe("1");
  expect(ppmToPercent(120000)).toBe("12");
  expect(ppmToPercent(125000)).toBe("12.5");
  expect(ppmToPercent(1)).toBe("0.0001");
  expect(halfDaysToDays(31)).toBe("15.5");
  expect(halfDaysToDays(62)).toBe("31");
  for (const paise of [1, 99, 100, 123450, 1_000_000_000])
    expect(parseRupees(paiseToRupees(paise))).toBe(paise);
  for (const ppm of [1, 120000, 125000, 333333, 1_000_000])
    expect(parsePercent(ppmToPercent(ppm))).toBe(ppm);
});

it("sends a fixed earning as the contract body with irrelevant fields dropped", () => {
  const body = toSalaryHeadInput(
    salaryHeadFormSchema.parse({
      ...fixed,
      rate: "40",
      base_head_ids: [hra],
      proration_days: "26",
    }),
  );
  expect(salaryHeadInputContract.parse(body)).toEqual(body);
  expect(body).toMatchObject({
    code: "SYN-BASIC",
    basis: "fixed",
    base_head_ids: [],
    proration: "calendar_days",
  });
  expect(body).not.toHaveProperty("rate_ppm");
  expect(body).not.toHaveProperty("proration_days");
  expect(body).not.toHaveProperty("min_amount_paise");
});

it("forces a deduction to be untaxed, outside wages and out of gross and CTC", () => {
  const body = toSalaryHeadInput(
    salaryHeadFormSchema.parse({
      ...percent,
      taxability: "exempt",
      tax_note: "x",
      wage_class: "wages",
    }),
  );
  expect(body).toMatchObject({
    rate_ppm: 120000,
    base_head_ids: [basic],
    proration: "none",
    max_amount_paise: 180000,
    in_gross: false,
    in_ctc: false,
    taxability: "not_applicable",
    wage_class: "outside",
  });
  expect(body).not.toHaveProperty("tax_note");
  expect(issues(salaryHeadFormSchema, { ...percent, taxability: "exempt" })).toBeUndefined();
});

it("refuses incomplete percentages, off-unit bounds, unexplained exemptions and unreviewed heads", () => {
  expect(issues(salaryHeadFormSchema, { ...percent, rate: "", base_head_ids: [] })).toEqual([
    ["rate", "Enter a percentage above 0 and at most 100, with up to 4 decimals."],
    ["base_head_ids", "Choose at least one base."],
  ]);
  expect(issues(salaryHeadFormSchema, { ...fixed, min_amount: "10.50", max_amount: "20" })).toEqual(
    [["min_amount", "Use a multiple of the rounding unit (₹1)."]],
  );
  expect(issues(salaryHeadFormSchema, { ...fixed, min_amount: "10", max_amount: "5" })).toEqual([
    ["max_amount", "Set the cap at or above the floor."],
  ]);
  expect(issues(salaryHeadFormSchema, { ...fixed, taxability: "exempt" })).toEqual([
    ["tax_note", "Cite the basis for the exemption."],
  ]);
  expect(
    issues(salaryHeadFormSchema, { ...fixed, proration: "fixed_days", proration_days: "32" }),
  ).toEqual([["proration_days", "Enter a whole number of days from 1 to 31."]]);
  expect(
    issues(salaryHeadFormSchema, {
      ...fixed,
      minimum_payable_days: "1.25",
      months: [],
      reviewed: false,
    }),
  ).toEqual([
    ["reviewed", "Confirm that you reviewed the head."],
    ["minimum_payable_days", "Enter 0 to 31 days, in steps of half a day."],
    ["months", "Choose at least one month."],
  ]);
  expect(issues(salaryHeadFormSchema, { ...fixed, code: "SYN BASIC" })?.[0]?.[0]).toBe("code");
});

it("edits a stored head without changing it", () => {
  salaryHeadContract.parse(record);
  const form = salaryHeadToForm(record);
  expect(form).toMatchObject({
    rate: "12.5",
    minimum_payable_days: "15.5",
    min_amount: "1.50",
    reviewed: false,
  });
  const body = toSalaryHeadInput(salaryHeadFormSchema.parse({ ...form, reviewed: true }));
  expect(body).toEqual({
    expected_version: 3,
    kind: "earning",
    code: "SYN-HRA",
    name: "Synthetic HRA",
    active: true,
    basis: "percent",
    rate_ppm: 125000,
    base_head_ids: [basic],
    proration: "none",
    minimum_payable_half_days: 31,
    months: [3, 9],
    rounding_mode: "up",
    rounding_unit_paise: 50,
    min_amount_paise: 150,
    in_gross: true,
    in_ctc: true,
    arrears_eligible: false,
    taxability: "exempt",
    tax_note: "Synthetic exemption basis",
    wage_class: "excluded",
    payslip_label: "HRA",
    payslip_order: 20,
    show_when_zero: true,
    effective_on: "2026-11-01",
    reviewed: true,
  });
  expect(describeSalaryHead(record, new Map([[basic, "Basic"]]))).toBe("12.5% of Basic · Mar, Sep");
});

it("needs amounts on fixed lines only and sends structures as the contract body", () => {
  const heads = [
    { id: basic, basis: "fixed" as const },
    { id: hra, basis: "percent" as const },
  ];
  const schema = salaryStructureSchemaFor(heads);
  const form = {
    expected_version: 0,
    code: "syn-nurse",
    name: "Synthetic Nurse",
    description: "",
    active: true,
    department_id: "",
    designation_id: "",
    grade_id: basic,
    lines: [
      { head_id: basic, amount: "30000" },
      { head_id: hra, amount: "999" },
    ],
    effective_on: "2026-11-01",
    reviewed: true,
  };
  const body = toSalaryStructureInput(schema.parse(form), heads);
  expect(salaryStructureInputContract.parse(body)).toEqual(body);
  expect(body.lines).toEqual([{ head_id: basic, amount_paise: 3000000 }, { head_id: hra }]);
  expect(body).not.toHaveProperty("department_id");
  expect(body.grade_id).toBe(basic);
  expect(
    issues(schema, {
      ...form,
      reviewed: false,
      lines: [
        { head_id: basic, amount: "" },
        { head_id: basic, amount: "1" },
      ],
    }),
  ).toEqual([
    ["reviewed", "Confirm that you reviewed the structure."],
    ["lines.0.amount", "Enter rupees with up to 2 decimals, at most ₹1 crore."],
    ["lines.1.head_id", "Use each head once."],
  ]);
  expect(issues(schema, { ...form, lines: [] })).toEqual([["lines", "Add at least one head."]]);

  const stored: SalaryStructure = {
    id: hra,
    revision_id: basic,
    code: "SYN-NURSE",
    name: "Synthetic Nurse",
    description: "",
    active: true,
    grade_id: basic,
    lines: [
      {
        position: 1,
        head_id: basic,
        code: "SYN-BASIC",
        name: "Basic",
        kind: "earning",
        basis: "fixed",
        amount_paise: 3000050,
      },
      {
        position: 2,
        head_id: hra,
        code: "SYN-HRA",
        name: "HRA",
        kind: "earning",
        basis: "percent",
      },
    ],
    effective_on: "2026-11-01",
    reviewed_by_subject: "user-admin",
    version: 2,
    created_at: "2026-10-16T00:00:00Z",
    updated_at: "2026-10-16T00:00:00Z",
  };
  expect(
    toSalaryStructureInput(
      schema.parse({ ...salaryStructureToForm(stored), reviewed: true }),
      heads,
    ),
  ).toEqual({
    expected_version: 2,
    code: "SYN-NURSE",
    name: "Synthetic Nurse",
    description: "",
    active: true,
    grade_id: basic,
    lines: [{ head_id: basic, amount_paise: 3000050 }, { head_id: hra }],
    effective_on: "2026-11-01",
    reviewed: true,
  });
});

it("bounds preview days by the month and builds paths", () => {
  expect(
    toSalaryPreviewInput(salaryPreviewFormSchema.parse({ on: "2026-03-15", payable_days: "30.5" })),
  ).toEqual({
    on: "2026-03-15",
    payable_half_days: 61,
  });
  expect(
    salaryPreviewInputContract.parse({ on: "2026-03-15", payable_half_days: 61 }),
  ).toBeTruthy();
  expect(issues(salaryPreviewFormSchema, { on: "2026-02-10", payable_days: "28.5" })).toEqual([
    ["payable_days", "Use no more than the 28 days in that month."],
  ]);
  expect(issues(salaryPreviewFormSchema, { on: "2028-02-10", payable_days: "29" })).toBeUndefined();
  expect(issues(salaryPreviewFormSchema, { on: "2026-03-15", payable_days: "x" })).toEqual([
    ["payable_days", "Enter days in steps of half a day."],
  ]);
  expect(salaryHeadsPath("/b", "deduction")).toBe("/b/hr/payroll/salary-heads?kind=deduction");
  expect(salaryHeadsPath("/b", "all")).toBe("/b/hr/payroll/salary-heads");
  expect(salaryStructuresPath("/b")).toBe("/b/hr/payroll/salary-structures");
});
