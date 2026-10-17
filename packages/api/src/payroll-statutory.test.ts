import { expect, it } from "vitest";
import {
  describeStatutoryRevision,
  newStatutoryRuleForm,
  parseRate,
  ptAnnualTotals,
  statutoryBandProblem,
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
  const form = {
    structure_id: id,
    on: "2026-09-17",
    payable_days: "29.5",
    disability: true,
    gender: "",
  };
  expect(toStatutoryPreviewInput(statutoryPreviewFormSchema.parse(form))).toEqual({
    structure_id: id,
    on: "2026-09-17",
    payable_half_days: 59,
    disability: true,
  });
  expect(
    toStatutoryPreviewInput(statutoryPreviewFormSchema.parse({ ...form, gender: "female" })).gender,
  ).toBe("female");
  expect(issues(statutoryPreviewFormSchema, { ...form, gender: "unknown" })).toEqual(["gender"]);
  expect(issues(statutoryPreviewFormSchema, { ...form, payable_days: "30.5" })).toEqual([
    "payable_days",
  ]);
  expect(issues(statutoryPreviewFormSchema, { ...form, structure_id: "" })).toEqual([
    "structure_id",
  ]);
  expect(statutoryRulesPath("/b")).toBe("/b/hr/payroll/statutory-rules");
  expect(statutoryPreviewPath("/b")).toBe("/b/hr/payroll/statutory-preview");
});

const all = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12];
const everyone = ["female", "male", "other"] as const;
const slab = (
  months: number[],
  genders: readonly string[],
  from: string,
  to: string,
  amount: string,
) => ({
  months,
  genders: [...genders] as ("female" | "male" | "other")[],
  from,
  to,
  amount,
});
// Synthetic: a gender split below ₹100 and a higher amount in March, never a real state's slabs.
const pt = {
  ...newStatutoryRuleForm("pt", "2026-04-01"),
  name: "Synthetic PT",
  coverage: "covered",
  citation: "Synthetic notification",
  pt: {
    state_code: " in-zz ",
    wage_basis: "gross",
    cadence: "monthly",
    period_start_months: [] as number[],
    slabs: [
      slab(all, ["female"], "0", "200", "0"),
      slab(all, ["male", "other"], "0", "100", "0"),
      slab(all, ["other", "male"], "100", "200", "1"),
      slab([1, 2, 4, 5, 6, 7, 8, 9, 10, 11, 12], everyone, "200", "", "2"),
      slab([3], everyone, "200", "", "5.5"),
    ],
  },
};
const lwfSlab = {
  from: "0",
  to: "",
  employee_fixed: "1",
  employee_rate: "0.5",
  employee_cap: "3",
  employer_fixed: "2",
  employer_rate: "0",
  employer_cap: "",
};
const lwf = {
  ...newStatutoryRuleForm("lwf", "2026-04-01"),
  name: "Synthetic LWF",
  coverage: "covered",
  citation: "Synthetic notification",
  lwf: {
    state_code: "IN-ZZ",
    wage_basis: "statutory_wages",
    deduction_months: [12, 6],
    rounding_mode: "nearest",
    rounding_unit: "1",
    slabs: [
      { ...lwfSlab, to: "50" },
      { ...lwfSlab, from: "50", employee_cap: "" },
    ],
  },
};
const message = (value: object) =>
  statutoryRuleFormSchema.safeParse(value).error?.issues[0]?.message;

it("names the first fault in a slab table", () => {
  expect(statutoryBandProblem([])).toBe("has no slab");
  expect(statutoryBandProblem([{ from: 0, to: 100 }, { from: 100 }])).toBeUndefined();
  expect(statutoryBandProblem([{ from: 100 }, { from: 0, to: 100 }])).toBeUndefined();
  expect(statutoryBandProblem([{ from: 100 }])).toBe(
    "leaves wages from ₹0.00 to ₹1.00 without a slab",
  );
  expect(statutoryBandProblem([{ from: 0, to: 100 }, { from: 150 }])).toBe(
    "leaves wages from ₹1.00 to ₹1.50 without a slab",
  );
  expect(statutoryBandProblem([{ from: 0, to: 200 }, { from: 100 }])).toBe(
    "has slabs that overlap from ₹1.00",
  );
  expect(statutoryBandProblem([{ from: 0 }, { from: 0 }])).toBe(
    "has an open-ended slab before the last one",
  );
  expect(statutoryBandProblem([{ from: 0, to: 100 }])).toBe(
    "needs a last slab from ₹1.00 with no upper limit",
  );
});

it("starts PT and LWF with one blank slab and requires every value", () => {
  const blank = {
    ...newStatutoryRuleForm("pt", "2026-04-01"),
    name: "S",
    citation: "S",
    coverage: "covered",
  };
  expect(blank.pt.slabs).toHaveLength(1);
  expect(issues(statutoryRuleFormSchema, blank)).toEqual([
    "pt.state_code",
    "pt.wage_basis",
    "pt.cadence",
    "pt.slabs.0.months",
    "pt.slabs.0.genders",
    "pt.slabs.0.amount",
    "pt.slabs.0.from",
  ]);
  expect(issues(statutoryRuleFormSchema, { ...blank, pt: { ...blank.pt, slabs: [] } })).toEqual([
    "pt.state_code",
    "pt.wage_basis",
    "pt.cadence",
    "pt.slabs",
  ]);
  const blankLwf = {
    ...newStatutoryRuleForm("lwf", "2026-04-01"),
    name: "S",
    citation: "S",
    coverage: "covered",
  };
  expect(issues(statutoryRuleFormSchema, blankLwf)).toEqual([
    "lwf.state_code",
    "lwf.wage_basis",
    "lwf.rounding_mode",
    "lwf.rounding_unit",
    "lwf.deduction_months",
    "lwf.slabs.0.employee_fixed",
    "lwf.slabs.0.employee_rate",
    "lwf.slabs.0.employer_fixed",
    "lwf.slabs.0.employer_rate",
    "lwf.slabs.0.from",
  ]);
});

it("converts PT slabs with canonical months, genders and state", () => {
  expect(issues(statutoryRuleFormSchema, pt)).toBeUndefined();
  const input = toStatutoryRuleInput(
    statutoryRuleFormSchema.parse({
      ...pt,
      pt: {
        ...pt.pt,
        slabs: [
          { ...pt.pt.slabs[0], months: [12, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11] },
          ...pt.pt.slabs.slice(1),
        ],
      },
    }),
  );
  expect(statutoryRuleInputContract.parse(input)).toEqual(input);
  expect(input).not.toHaveProperty("lwf");
  expect(input.pt?.state_code).toBe("IN-ZZ");
  expect(input.pt?.slabs[0]).toEqual({
    months: all,
    genders: ["female"],
    from_paise: 0,
    to_paise: 20_000,
    amount_paise: 0,
  });
  expect(input.pt?.slabs[2]?.genders).toEqual(["male", "other"]);
  expect(input.pt?.slabs[4]).toEqual({
    months: [3],
    genders: ["female", "male", "other"],
    from_paise: 20_000,
    amount_paise: 550,
  });
  expect(input.pt?.slabs[4]).not.toHaveProperty("to_paise");
});

it("refuses incomplete PT tables with a message naming the month and gender", () => {
  const without = (i: number) => ({
    ...pt,
    pt: { ...pt.pt, slabs: pt.pt.slabs.filter((_, j) => j !== i) },
  });
  expect(message(without(0))).toBe(
    "Jan, women: the table leaves wages from ₹0.00 to ₹200.00 without a slab.",
  );
  expect(message(without(4))).toBe(
    "Mar, women: the table needs a last slab from ₹200.00 with no upper limit.",
  );
  expect(issues(statutoryRuleFormSchema, without(4))).toEqual(["pt.slabs"]);
  const overlap = {
    ...pt,
    pt: { ...pt.pt, slabs: [...pt.pt.slabs, slab([3], ["other"], "150", "", "1")] },
  };
  expect(message(overlap)).toBe(
    "Mar, other genders: the table has slabs that overlap from ₹150.00.",
  );
  // A month only some genders cover would let a declared gender decide whether PT exists.
  const partial = { ...pt, pt: { ...pt.pt, slabs: [slab([5], ["male"], "0", "", "1")] } };
  expect(message(partial)).toBe("May, women: the table has no slab.");
  expect(
    issues(statutoryRuleFormSchema, {
      ...pt,
      pt: { ...pt.pt, slabs: [slab([5], everyone, "0", "", "1")] },
    }),
  ).toBeUndefined();
});

it("checks each PT slab field before the table", () => {
  const bad = (patch: object) => ({
    ...pt,
    pt: { ...pt.pt, slabs: [{ ...pt.pt.slabs[0], ...patch }, ...pt.pt.slabs.slice(1)] },
  });
  expect(issues(statutoryRuleFormSchema, bad({ to: "0" }))).toEqual(["pt.slabs.0.to"]);
  expect(issues(statutoryRuleFormSchema, bad({ from: "x" }))).toEqual(["pt.slabs.0.from"]);
  expect(issues(statutoryRuleFormSchema, bad({ amount: "100000.01" }))).toEqual([
    "pt.slabs.0.amount",
  ]);
  expect(issues(statutoryRuleFormSchema, bad({ amount: "100000" }))).toBeUndefined();
  expect(issues(statutoryRuleFormSchema, bad({ months: [] }))).toEqual(["pt.slabs.0.months"]);
  expect(issues(statutoryRuleFormSchema, bad({ genders: [] }))).toEqual([
    "pt.slabs.0.genders",
    "pt.slabs",
  ]);
  expect(issues(statutoryRuleFormSchema, { ...pt, pt: { ...pt.pt, state_code: "IN-Z1" } })).toEqual(
    ["pt.state_code"],
  );
});

it("totals the highest yearly PT per gender", () => {
  expect(ptAnnualTotals(pt.pt.slabs)).toEqual({ female: 2_750, male: 2_750, other: 2_750 });
  const low = pt.pt.slabs.slice(0, 3);
  expect(ptAnnualTotals(low)).toEqual({ female: 0, male: 1_200, other: 1_200 });
  // The maximum is over every band, not the top one: a lower band can owe more.
  expect(
    ptAnnualTotals([slab(all, everyone, "0", "1", "5"), slab(all, everyone, "1", "", "1")]),
  ).toEqual({ female: 6_000, male: 6_000, other: 6_000 });
  expect(ptAnnualTotals([{ ...pt.pt.slabs[0], amount: "" }])).toBeUndefined();
});

it("converts LWF slabs with optional caps and sorted months", () => {
  expect(issues(statutoryRuleFormSchema, lwf)).toBeUndefined();
  const input = toStatutoryRuleInput(statutoryRuleFormSchema.parse(lwf));
  expect(statutoryRuleInputContract.parse(input)).toEqual(input);
  expect(input).not.toHaveProperty("pt");
  expect(input.lwf).toEqual({
    state_code: "IN-ZZ",
    wage_basis: "statutory_wages",
    deduction_months: [6, 12],
    rounding_mode: "nearest",
    rounding_unit_paise: 1,
    slabs: [
      {
        from_paise: 0,
        to_paise: 5_000,
        employee_fixed_paise: 100,
        employee_rate_ppm: 5_000,
        employee_cap_paise: 300,
        employer_fixed_paise: 200,
        employer_rate_ppm: 0,
      },
      {
        from_paise: 5_000,
        employee_fixed_paise: 100,
        employee_rate_ppm: 5_000,
        employer_fixed_paise: 200,
        employer_rate_ppm: 0,
      },
    ],
  });
  expect(message({ ...lwf, lwf: { ...lwf.lwf, slabs: [lwf.lwf.slabs[1]] } })).toBe(
    "The table leaves wages from ₹0.00 to ₹50.00 without a slab.",
  );
  expect(
    issues(statutoryRuleFormSchema, { ...lwf, lwf: { ...lwf.lwf, deduction_months: [] } }),
  ).toEqual(["lwf.deduction_months"]);
  const badSlab = (patch: object) => ({
    ...lwf,
    lwf: { ...lwf.lwf, slabs: [{ ...lwf.lwf.slabs[0], ...patch }, lwf.lwf.slabs[1]] },
  });
  expect(issues(statutoryRuleFormSchema, badSlab({ employer_rate: "100.1" }))).toEqual([
    "lwf.slabs.0.employer_rate",
  ]);
  expect(issues(statutoryRuleFormSchema, badSlab({ employee_cap: "x" }))).toEqual([
    "lwf.slabs.0.employee_cap",
  ]);
  expect(issues(statutoryRuleFormSchema, badSlab({ employer_fixed: "100000.01" }))).toEqual([
    "lwf.slabs.0.employer_fixed",
  ]);
});

it("round-trips PT and LWF revisions and describes them", () => {
  for (const [form, text] of [
    [pt, "IN-ZZ · 5 slabs · due every month"],
    [lwf, "IN-ZZ · 2 slabs · deducted in Jun, Dec"],
  ] as const) {
    const input = toStatutoryRuleInput(statutoryRuleFormSchema.parse(form));
    const content: Omit<typeof input, "expected_version"> & { expected_version?: number } = {
      ...input,
    };
    delete content.expected_version;
    const revision = statutoryRevisionContract.parse({
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
    expect(
      toStatutoryRuleInput(statutoryRuleFormSchema.parse(statutoryRevisionToForm(revision, 1))),
    ).toEqual({
      ...input,
      expected_version: 1,
    });
    expect(describeStatutoryRevision(revision)).toBe(text);
  }
  const febOnly = { ...pt, pt: { ...pt.pt, slabs: [slab([2], everyone, "0", "", "1")] } };
  const input = toStatutoryRuleInput(statutoryRuleFormSchema.parse(febOnly));
  expect(
    describeStatutoryRevision({ ...(input as unknown as StatutoryRevision), covered: true }),
  ).toBe("IN-ZZ · 1 slab · due in Feb");
});

// Synthetic half-yearly PT charged in each period's last month, never a real state's schedule.
const halfYearly = {
  ...pt,
  pt: {
    ...pt.pt,
    cadence: "half_yearly",
    period_start_months: [10, 4],
    slabs: [slab([9, 3], everyone, "0", "72000", "0"), slab([9, 3], everyone, "72000", "", "600")],
  },
};

it("converts, round-trips and describes a half-yearly PT revision", () => {
  expect(issues(statutoryRuleFormSchema, halfYearly)).toBeUndefined();
  const input = toStatutoryRuleInput(statutoryRuleFormSchema.parse(halfYearly));
  expect(statutoryRuleInputContract.parse(input)).toEqual(input);
  expect(input.pt?.cadence).toBe("half_yearly");
  expect(input.pt?.period_start_months).toEqual([4, 10]);
  const monthly = toStatutoryRuleInput(statutoryRuleFormSchema.parse(pt));
  expect(monthly.pt?.cadence).toBe("monthly");
  expect(monthly.pt).not.toHaveProperty("period_start_months");
  const content: Omit<typeof input, "expected_version"> & { expected_version?: number } = {
    ...input,
  };
  delete content.expected_version;
  const revision = statutoryRevisionContract.parse({
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
  expect(
    toStatutoryRuleInput(statutoryRuleFormSchema.parse(statutoryRevisionToForm(revision, 1))),
  ).toEqual({ ...input, expected_version: 1 });
  expect(describeStatutoryRevision(revision)).toBe(
    "IN-ZZ · half-yearly from Apr, Oct · 2 slabs · due in Mar, Sep",
  );
});

it("refuses PT periods that skip or double a period's charge", () => {
  const withPt = (patch: object) => ({ ...halfYearly, pt: { ...halfYearly.pt, ...patch } });
  expect(issues(statutoryRuleFormSchema, withPt({ cadence: "" }))).toEqual(["pt.cadence"]);
  expect(message(withPt({ period_start_months: [4] }))).toBe(
    "Choose two start months six months apart.",
  );
  expect(message(withPt({ period_start_months: [4, 9] }))).toBe(
    "Choose two start months six months apart.",
  );
  expect(message(withPt({ cadence: "annual" }))).toBe("Choose the one month the year starts.");
  expect(message(withPt({ slabs: [slab([9], everyone, "0", "", "1")] }))).toBe(
    "The period starting in Oct must be charged in exactly one month; the slabs name none.",
  );
  expect(message(withPt({ slabs: [slab([8, 9, 3], everyone, "0", "", "1")] }))).toBe(
    "The period starting in Apr must be charged in exactly one month; the slabs name Aug, Sep.",
  );
  expect(
    issues(statutoryRuleFormSchema, withPt({ slabs: [slab([8, 9, 3], everyone, "0", "", "1")] })),
  ).toEqual(["pt.period_start_months"]);
  expect(
    issues(
      statutoryRuleFormSchema,
      withPt({
        cadence: "annual",
        period_start_months: [1],
        slabs: [slab([12], everyone, "0", "", "1")],
      }),
    ),
  ).toBeUndefined();
});
