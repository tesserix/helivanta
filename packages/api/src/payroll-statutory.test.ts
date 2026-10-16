import { expect, it } from "vitest";
import {
  describeStatutoryRevision,
  newStatutoryRuleForm,
  parseRate,
  statutoryDecisionFormSchema,
  statutoryPreviewFormSchema,
  statutoryPreviewPath,
  statutoryRevisionContract,
  statutoryRevisionToForm,
  statutoryRuleFormSchema,
  statutoryRuleInputContract,
  statutoryRulesPath,
  toStatutoryPreviewInput,
  toStatutoryRuleInput,
  type StatutoryRevision,
} from "./payroll";

const id = "11111111-1111-4111-8111-111111111111";
const issues = (
  schema: {
    safeParse: (v: unknown) => {
      error?: { issues: { path: (string | number)[]; message: string }[] };
    };
  },
  value: object,
) => schema.safeParse(value).error?.issues.map((i) => i.path.join("."));

// Synthetic values: these prove conversion, never statutory rates.
const pf = {
  ...newStatutoryRuleForm("pf", "2026-09-17"),
  name: "Synthetic PF",
  coverage: "covered",
  citation: "Synthetic notification",
  pf: {
    wage_basis: "statutory_wages",
    contribution_basis: "capped",
    wage_ceiling: "10,000.50",
    employee_rate: "10",
    employer_rate: "10",
    eps_rate: "6.5",
    eps_wage_ceiling: "10000",
    edli_rate: "0.4",
    edli_wage_ceiling: "8000",
    admin_rate: "0",
    admin_minimum: "0",
    rounding_mode: "nearest",
    rounding_unit: "100",
  },
};
const esi = {
  ...newStatutoryRuleForm("esi", "2026-04-01"),
  name: "Synthetic ESI",
  coverage: "covered",
  citation: "Synthetic notification",
  esi: {
    wage_basis: "gross",
    wage_ceiling: "15000",
    disability_wage_ceiling: "20000",
    employee_rate: "1",
    employer_rate: "3",
    employee_exempt_daily: "",
    period_start_months: [10, 4],
    rounding_mode: "up",
    rounding_unit: "100",
  },
};

it("parses statutory rates including zero, without floats", () => {
  expect(parseRate("0")).toBe(0);
  expect(parseRate("0.0001")).toBe(1);
  expect(parseRate("8.33")).toBe(83_300);
  expect(parseRate("100")).toBe(1_000_000);
  expect(parseRate("100.0001")).toBeUndefined();
  expect(parseRate("1e2")).toBeUndefined();
  expect(parseRate("-1")).toBeUndefined();
});

it("starts every statutory choice blank so nothing is pre-loaded", () => {
  const blank = newStatutoryRuleForm("pf", "2026-09-17");
  expect(blank.coverage).toBe("");
  expect(Object.values(blank.pf).every((v) => v === "")).toBe(true);
  expect(blank.esi.period_start_months).toEqual([]);
  expect(issues(statutoryRuleFormSchema, blank)).toEqual(["name", "citation", "coverage"]);
  expect(
    issues(statutoryRuleFormSchema, {
      ...blank,
      name: "Synthetic",
      citation: "Synthetic",
      coverage: "covered",
    }),
  ).toEqual([
    "pf.wage_basis",
    "pf.rounding_mode",
    "pf.rounding_unit",
    "pf.contribution_basis",
    "pf.wage_ceiling",
    "pf.employee_rate",
    "pf.employer_rate",
    "pf.eps_rate",
    "pf.eps_wage_ceiling",
    "pf.edli_rate",
    "pf.edli_wage_ceiling",
    "pf.admin_rate",
    "pf.admin_minimum",
  ]);
});

it("converts a covered PF form into exactly the PF parameters", () => {
  expect(issues(statutoryRuleFormSchema, pf)).toBeUndefined();
  const input = toStatutoryRuleInput(statutoryRuleFormSchema.parse(pf));
  expect(statutoryRuleInputContract.parse(input)).toEqual(input);
  expect(input.esi).toBeUndefined();
  expect(input.pf).toEqual({
    wage_basis: "statutory_wages",
    contribution_basis: "capped",
    wage_ceiling_paise: 1_000_050,
    employee_rate_ppm: 100_000,
    employer_rate_ppm: 100_000,
    eps_rate_ppm: 65_000,
    eps_wage_ceiling_paise: 1_000_000,
    edli_rate_ppm: 4_000,
    edli_wage_ceiling_paise: 800_000,
    admin_rate_ppm: 0,
    admin_minimum_paise: 0,
    rounding_mode: "nearest",
    rounding_unit_paise: 100,
  });
});

it("refuses PF parameters the server would reject", () => {
  expect(issues(statutoryRuleFormSchema, { ...pf, pf: { ...pf.pf, eps_rate: "10.0001" } })).toEqual(
    ["pf.eps_rate"],
  );
  expect(
    issues(statutoryRuleFormSchema, { ...pf, pf: { ...pf.pf, eps_wage_ceiling: "10000.51" } }),
  ).toEqual(["pf.eps_wage_ceiling"]);
  expect(issues(statutoryRuleFormSchema, { ...pf, pf: { ...pf.pf, wage_ceiling: "0" } })).toEqual([
    "pf.wage_ceiling",
  ]);
  expect(
    issues(statutoryRuleFormSchema, { ...pf, pf: { ...pf.pf, contribution_basis: "basic" } }),
  ).toEqual(["pf.contribution_basis"]);
  expect(issues(statutoryRuleFormSchema, { ...pf, pf: { ...pf.pf, rounding_unit: "25" } })).toEqual(
    ["pf.rounding_unit"],
  );
});

it("converts ESI with sorted periods and an optional exemption", () => {
  const input = toStatutoryRuleInput(statutoryRuleFormSchema.parse(esi));
  expect(input.pf).toBeUndefined();
  expect(input.esi?.period_start_months).toEqual([4, 10]);
  expect(input.esi).not.toHaveProperty("employee_exempt_daily_paise");
  expect(
    toStatutoryRuleInput(
      statutoryRuleFormSchema.parse({ ...esi, esi: { ...esi.esi, employee_exempt_daily: "176" } }),
    ).esi?.employee_exempt_daily_paise,
  ).toBe(17_600);
  expect(
    issues(statutoryRuleFormSchema, {
      ...esi,
      esi: { ...esi.esi, disability_wage_ceiling: "14999.99" },
    }),
  ).toEqual(["esi.disability_wage_ceiling"]);
  expect(
    issues(statutoryRuleFormSchema, { ...esi, esi: { ...esi.esi, period_start_months: [] } }),
  ).toEqual(["esi.period_start_months"]);
  expect(
    issues(statutoryRuleFormSchema, { ...esi, esi: { ...esi.esi, employee_exempt_daily: "0" } }),
  ).toEqual(["esi.employee_exempt_daily"]);
});

it("sends no parameters for an uncovered hospital", () => {
  const uncovered = {
    ...pf,
    coverage: "not_covered",
    pf: { ...pf.pf, wage_ceiling: "not checked" },
  };
  expect(issues(statutoryRuleFormSchema, uncovered)).toBeUndefined();
  const input = toStatutoryRuleInput(statutoryRuleFormSchema.parse(uncovered));
  expect(input.covered).toBe(false);
  expect(input).not.toHaveProperty("pf");
  expect(input).not.toHaveProperty("esi");
});

it("reopens a revision as the base of the next proposal and round-trips", () => {
  const input = toStatutoryRuleInput(statutoryRuleFormSchema.parse(pf));
  const content: Omit<typeof input, "expected_version"> & { expected_version?: number } = {
    ...input,
  };
  delete content.expected_version;
  const revision: StatutoryRevision = statutoryRevisionContract.parse({
    ...content,
    id,
    rule_id: id,
    sequence: 1,
    base_version: 0,
    status: "approved",
    approved_version: 1,
    proposed_by_subject: "user-a",
    decided_by_subject: "user-b",
    decided_at: "2026-09-16T10:00:00Z",
    decision_note: "",
    created_at: "2026-09-16T09:00:00Z",
  });
  const form = statutoryRevisionToForm(revision, 1);
  expect(form.expected_version).toBe(1);
  expect(toStatutoryRuleInput(statutoryRuleFormSchema.parse(form))).toEqual({
    ...input,
    expected_version: 1,
  });
  expect(describeStatutoryRevision(revision)).toBe(
    "Employee 10% · employer 10% (pension 6.5%) · ceiling ₹10,000.50",
  );
  expect(describeStatutoryRevision({ ...revision, covered: false, pf: undefined })).toBe(
    "Not covered",
  );
});

it("requires a reason to reject but not to approve", () => {
  expect(issues(statutoryDecisionFormSchema, { decision: "reject", note: "  " })).toEqual(["note"]);
  expect(issues(statutoryDecisionFormSchema, { decision: "approve", note: "" })).toBeUndefined();
  expect(
    issues(statutoryDecisionFormSchema, { decision: "approve", note: "x".repeat(501) }),
  ).toEqual(["note"]);
});

it("validates and converts the statutory preview", () => {
  const form = { structure_id: id, on: "2026-09-17", payable_days: "29.5", disability: true };
  expect(toStatutoryPreviewInput(statutoryPreviewFormSchema.parse(form))).toEqual({
    structure_id: id,
    on: "2026-09-17",
    payable_half_days: 59,
    disability: true,
  });
  expect(issues(statutoryPreviewFormSchema, { ...form, payable_days: "30.5" })).toEqual([
    "payable_days",
  ]);
  expect(issues(statutoryPreviewFormSchema, { ...form, structure_id: "" })).toEqual([
    "structure_id",
  ]);
  expect(statutoryRulesPath("/b")).toBe("/b/hr/payroll/statutory-rules");
  expect(statutoryPreviewPath("/b")).toBe("/b/hr/payroll/statutory-preview");
});
