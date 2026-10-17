import { expect, it } from "vitest";
import {
  blankClaim,
  describeStatutoryRevision,
  incomeTaxPreviewFormSchema,
  incomeTaxPreviewPath,
  newStatutoryRuleForm,
  parseIncome,
  statutoryRevisionContract,
  statutoryRevisionToForm,
  statutoryRuleFormSchema,
  statutoryRuleInputContract,
  taxYearOf,
  toIncomeTaxPreviewInput,
  toStatutoryRuleInput,
} from "./payroll";

const id = "11111111-1111-4111-8111-111111111111";
const paths = (
  schema: typeof statutoryRuleFormSchema | typeof incomeTaxPreviewFormSchema,
  v: object,
) => schema.safeParse(v).error?.issues.map((i) => i.path.join("."));
const messages = (v: object) =>
  statutoryRuleFormSchema.safeParse(v).error?.issues.map((i) => i.message);

// Synthetic ladder only, never the Act's slabs: nil to ₹1 lakh, 10% to ₹2 lakh, 20% above.
const incomeTax = {
  tax_year: "2026-27",
  standard_deduction: "10,000",
  taxable_rounding_mode: "nearest",
  taxable_rounding_unit: "1000",
  tax_rounding_mode: "up",
  tax_rounding_unit: "1000",
  age_bands: [
    { min_age: "60", slabs: [{ from: "0", to: "", rate: "0" }] },
    {
      min_age: "0",
      slabs: [
        { from: "100000", to: "200000", rate: "10" },
        { from: "0", to: "100000", rate: "0" },
        { from: "200000", to: "", rate: "20" },
      ],
    },
  ],
  rebate: "relief",
  rebate_income_limit: "150000",
  rebate_max: "10000",
  surcharges: [
    { above: "500000", rate: "10" },
    { above: "1,00,00,000", rate: "20" },
  ],
  cess_rate: "5",
  claim_groups: [] as { code: string; label: string; cap: string }[],
  claims: [] as ReturnType<typeof blankClaim>[],
};
const form = {
  ...newStatutoryRuleForm("income_tax_new", "2026-04-01"),
  name: "Synthetic new regime",
  coverage: "covered",
  citation: "Synthetic notification",
  income_tax: incomeTax,
};
const withTax = (patch: object) => ({ ...form, income_tax: { ...incomeTax, ...patch } });

it("parses incomes in crores and names tax years", () => {
  expect(parseIncome("1,00,00,000.5")).toBe(1_000_000_050);
  expect(parseIncome("1000000000")).toBe(100_000_000_000);
  expect(parseIncome("1000000000.01")).toBeUndefined();
  expect(parseIncome("1.234")).toBeUndefined();
  expect(parseIncome("-1")).toBeUndefined();
  expect(taxYearOf("2027-03-31")).toBe("2026-27");
  expect(taxYearOf("2027-04-01")).toBe("2027-28");
  expect(taxYearOf("2099-04-01")).toBe("2099-00");
  expect(incomeTaxPreviewPath("/b")).toBe("/b/hr/payroll/income-tax-preview");
});

it("converts an income-tax rule into the contract's paise and ppm", () => {
  const input = toStatutoryRuleInput(statutoryRuleFormSchema.parse(form));
  expect(statutoryRuleInputContract.parse(input)).toEqual(input);
  expect(input.pf ?? input.esi ?? input.pt ?? input.lwf).toBeUndefined();
  expect(input.income_tax).toEqual({
    tax_year: "2026-27",
    standard_deduction_paise: 1_000_000,
    taxable_rounding_mode: "nearest",
    taxable_rounding_unit_paise: 1000,
    tax_rounding_mode: "up",
    tax_rounding_unit_paise: 1000,
    age_bands: [
      { min_age: 60, slabs: [{ from_paise: 0, rate_ppm: 0 }] },
      {
        min_age: 0,
        slabs: [
          { from_paise: 10_000_000, to_paise: 20_000_000, rate_ppm: 100_000 },
          { from_paise: 0, to_paise: 10_000_000, rate_ppm: 0 },
          { from_paise: 20_000_000, rate_ppm: 200_000 },
        ],
      },
    ],
    rebate: { income_limit_paise: 15_000_000, max_paise: 1_000_000, marginal_relief: true },
    surcharge_bands: [
      { above_paise: 50_000_000, rate_ppm: 100_000 },
      { above_paise: 1_000_000_000, rate_ppm: 200_000 },
    ],
    cess_rate_ppm: 50_000,
  });
  expect(
    toStatutoryRuleInput(statutoryRuleFormSchema.parse(withTax({ rebate: "cliff" }))).income_tax
      ?.rebate,
  ).toEqual({ income_limit_paise: 15_000_000, max_paise: 1_000_000, marginal_relief: false });
  const none = toStatutoryRuleInput(
    statutoryRuleFormSchema.parse(withTax({ rebate: "none", rebate_max: "" })),
  );
  expect(none.income_tax).not.toHaveProperty("rebate");
  const uncovered = toStatutoryRuleInput(
    statutoryRuleFormSchema.parse({
      ...form,
      coverage: "not_covered",
      income_tax: newStatutoryRuleForm("income_tax_old", "2026-04-01").income_tax,
    }),
  );
  expect(uncovered).not.toHaveProperty("income_tax");
});

it("requires every income-tax value on a blank rule", () => {
  const blank = {
    ...newStatutoryRuleForm("income_tax_old", "2026-04-01"),
    name: "S",
    citation: "S",
    coverage: "covered",
  };
  expect(blank.income_tax.age_bands).toHaveLength(1);
  expect(paths(statutoryRuleFormSchema, blank)).toEqual([
    "income_tax.tax_year",
    "income_tax.standard_deduction",
    "income_tax.taxable_rounding_mode",
    "income_tax.taxable_rounding_unit",
    "income_tax.tax_rounding_mode",
    "income_tax.tax_rounding_unit",
    "income_tax.cess_rate",
    "income_tax.rebate",
    "income_tax.age_bands.0.min_age",
    "income_tax.age_bands.0.slabs.0.from",
    "income_tax.age_bands.0.slabs.0.rate",
    "income_tax.age_bands",
  ]);
});

it("refuses income-tax rules the server would refuse", () => {
  for (const year of ["2026-28", "1999-00", "2200-01", "2026/27"])
    expect(messages(withTax({ tax_year: year }))).toEqual([
      "Enter the tax year as YYYY-YY, for example 2026-27.",
    ]);
  expect(messages({ ...form, effective_on: "2027-04-01" })).toEqual([
    "The effective date falls in tax year 2027-28. A rule covers one tax year, from 1 April to 31 March.",
  ]);
  expect(paths(statutoryRuleFormSchema, { ...form, effective_on: "2027-03-31" })).toBeUndefined();
  expect(paths(statutoryRuleFormSchema, withTax({ standard_deduction: "1000000.01" }))).toEqual([
    "income_tax.standard_deduction",
  ]);
  expect(paths(statutoryRuleFormSchema, withTax({ standard_deduction: "0" }))).toBeUndefined();
  expect(paths(statutoryRuleFormSchema, withTax({ taxable_rounding_unit: "7" }))).toEqual([
    "income_tax.taxable_rounding_unit",
  ]);
  expect(paths(statutoryRuleFormSchema, withTax({ cess_rate: "100.5" }))).toEqual([
    "income_tax.cess_rate",
  ]);
  expect(paths(statutoryRuleFormSchema, withTax({ rebate_max: "0" }))).toEqual([
    "income_tax.rebate_max",
  ]);
  expect(
    paths(statutoryRuleFormSchema, withTax({ rebate: "cliff", rebate_income_limit: "" })),
  ).toEqual(["income_tax.rebate_income_limit"]);

  const [senior, base] = incomeTax.age_bands as [
    (typeof incomeTax.age_bands)[0],
    (typeof incomeTax.age_bands)[1],
  ];
  expect(messages(withTax({ age_bands: [senior] }))).toEqual([
    "One band must start at age 0, so everyone has a slab table.",
  ]);
  expect(
    paths(statutoryRuleFormSchema, withTax({ age_bands: [base, { ...senior, min_age: "0" }] })),
  ).toEqual(["income_tax.age_bands.1.min_age"]);
  expect(
    paths(statutoryRuleFormSchema, withTax({ age_bands: [base, { ...senior, min_age: "121" }] })),
  ).toEqual(["income_tax.age_bands.1.min_age"]);
  expect(paths(statutoryRuleFormSchema, withTax({ age_bands: [] }))).toEqual([
    "income_tax.age_bands",
  ]);
  expect(
    paths(
      statutoryRuleFormSchema,
      withTax({
        age_bands: [
          base,
          senior,
          { ...senior, min_age: "70" },
          { ...senior, min_age: "80" },
          { ...senior, min_age: "90" },
        ],
      }),
    ),
  ).toEqual(["income_tax.age_bands"]);

  const slabs = (s: object[]) => withTax({ age_bands: [{ min_age: "0", slabs: s }] });
  expect(
    messages(
      slabs([
        { from: "0", to: "100", rate: "0" },
        { from: "150", to: "", rate: "1" },
      ]),
    ),
  ).toEqual(["The table leaves income from ₹100.00 to ₹150.00 without a slab."]);
  expect(paths(statutoryRuleFormSchema, slabs([{ from: "0", to: "0", rate: "0" }]))).toEqual([
    "income_tax.age_bands.0.slabs.0.to",
  ]);
  expect(paths(statutoryRuleFormSchema, slabs([{ from: "0", to: "", rate: "101" }]))).toEqual([
    "income_tax.age_bands.0.slabs.0.rate",
  ]);
  expect(paths(statutoryRuleFormSchema, slabs([]))).toEqual(["income_tax.age_bands.0.slabs"]);
  const ladder = Array.from({ length: 17 }, (_, i) => ({
    from: String(i),
    to: i === 16 ? "" : String(i + 1),
    rate: "0",
  }));
  expect(messages(slabs(ladder))).toEqual(["Add between 1 and 16 slabs."]);
  expect(
    messages(slabs(ladder.slice(1).map((s, i) => (i ? s : { ...s, from: "0" })))),
  ).toBeUndefined();

  const surcharges = (s: object[]) => withTax({ surcharges: s });
  expect(
    paths(
      statutoryRuleFormSchema,
      surcharges([
        { above: "500", rate: "1" },
        { above: "500", rate: "2" },
      ]),
    ),
  ).toEqual(["income_tax.surcharges.1.above"]);
  expect(
    paths(
      statutoryRuleFormSchema,
      surcharges([
        { above: "500", rate: "1" },
        { above: "400", rate: "2" },
      ]),
    ),
  ).toEqual(["income_tax.surcharges.1.above"]);
  expect(paths(statutoryRuleFormSchema, surcharges([{ above: "0", rate: "1" }]))).toEqual([
    "income_tax.surcharges.0.above",
  ]);
  expect(
    paths(
      statutoryRuleFormSchema,
      surcharges(Array.from({ length: 9 }, (_, i) => ({ above: String(i + 1), rate: "1" }))),
    ),
  ).toEqual(["income_tax.surcharges"]);
  expect(paths(statutoryRuleFormSchema, surcharges([]))).toBeUndefined();
});

it("round-trips an income-tax revision and describes it", () => {
  for (const [patch, text] of [
    [{}, "Tax year 2026-27 · 2 age bands · rebate to ₹1,50,000.00 · cess 5%"],
    [
      { rebate: "none", age_bands: [incomeTax.age_bands[1]] },
      "Tax year 2026-27 · one slab table · no rebate · cess 5%",
    ],
    [{ rebate: "cliff" }, "Tax year 2026-27 · 2 age bands · rebate to ₹1,50,000.00 · cess 5%"],
  ] as const) {
    const input = toStatutoryRuleInput(statutoryRuleFormSchema.parse(withTax(patch)));
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
});

it("validates and converts the income-tax preview", () => {
  const preview = {
    on: "2026-10-01",
    annual_income: "12,00,000.50",
    age: "",
    claims: [],
    hra: { received: "", salary: "", rent_paid: "", city: "" },
  };
  expect(toIncomeTaxPreviewInput(incomeTaxPreviewFormSchema.parse(preview))).toEqual({
    on: "2026-10-01",
    annual_income_paise: 120_000_050,
  });
  expect(
    toIncomeTaxPreviewInput(incomeTaxPreviewFormSchema.parse({ ...preview, age: "61" })),
  ).toEqual({
    on: "2026-10-01",
    annual_income_paise: 120_000_050,
    age: 61,
  });
  expect(paths(incomeTaxPreviewFormSchema, { ...preview, annual_income: "x" })).toEqual([
    "annual_income",
  ]);
  expect(paths(incomeTaxPreviewFormSchema, { ...preview, age: "121" })).toEqual(["age"]);
  expect(paths(incomeTaxPreviewFormSchema, { ...preview, age: "6.5" })).toEqual(["age"]);
  expect(paths(incomeTaxPreviewFormSchema, { ...preview, on: "2026-13-01" })).toEqual(["on"]);
});

// Synthetic claim heads only: HRA at 10%/50%/40%, one ₹15,000 group over two investments and ₹20,000 interest.
const claimHead = (patch: Partial<ReturnType<typeof blankClaim>>) => ({
  ...blankClaim(),
  ...patch,
});
const claims = {
  claim_groups: [{ code: "invest", label: " Investments ", cap: "15,000" }],
  claims: [
    claimHead({
      code: "HRA",
      label: "House rent",
      kind: "hra",
      cap: "9",
      cap_group: "INVEST",
      rent_excess_rate: "10",
      metro_rate: "50",
      other_rate: "40",
      landlord_pan_above: "1,00,000",
    }),
    claimHead({
      code: "savings",
      label: "Savings",
      kind: "deduction",
      cap: "12,000",
      cap_group: "invest",
    }),
    claimHead({ code: "PENSION", label: "Pension", kind: "deduction", cap_group: "INVEST" }),
    claimHead({
      code: "HOMELOAN",
      label: "Home loan interest",
      kind: "house_property_interest",
      cap: "20,000",
      cap_group: "INVEST",
    }),
  ],
};

it("converts claim heads, sending only each kind's own fields", () => {
  const input = toStatutoryRuleInput(statutoryRuleFormSchema.parse(withTax(claims)));
  expect(statutoryRuleInputContract.parse(input)).toEqual(input);
  expect(input.income_tax?.claim_groups).toEqual([
    { code: "INVEST", label: "Investments", cap_paise: 1_500_000 },
  ]);
  expect(input.income_tax?.claims).toEqual([
    {
      code: "HRA",
      label: "House rent",
      kind: "hra",
      hra_rent_excess_rate_ppm: 100_000,
      hra_metro_rate_ppm: 500_000,
      hra_other_rate_ppm: 400_000,
      landlord_pan_rent_above_paise: 10_000_000,
    },
    {
      code: "SAVINGS",
      label: "Savings",
      kind: "deduction",
      cap_paise: 1_200_000,
      cap_group: "INVEST",
    },
    { code: "PENSION", label: "Pension", kind: "deduction", cap_group: "INVEST" },
    {
      code: "HOMELOAN",
      label: "Home loan interest",
      kind: "house_property_interest",
      cap_paise: 2_000_000,
    },
  ]);
  const none = toStatutoryRuleInput(statutoryRuleFormSchema.parse(form)).income_tax;
  expect(none).not.toHaveProperty("claims");
  expect(none).not.toHaveProperty("claim_groups");
});

it("refuses claim heads the server would refuse", () => {
  const change = (i: number, patch: Partial<ReturnType<typeof blankClaim>>) =>
    withTax({ ...claims, claims: claims.claims.map((c, j) => (j === i ? { ...c, ...patch } : c)) });
  expect(paths(statutoryRuleFormSchema, change(2, { code: " Savings " }))).toEqual([
    "income_tax.claims.2.code",
  ]);
  expect(paths(statutoryRuleFormSchema, change(2, { code: "PEN SION" }))).toEqual([
    "income_tax.claims.2.code",
  ]);
  expect(paths(statutoryRuleFormSchema, change(2, { label: " " }))).toEqual([
    "income_tax.claims.2.label",
  ]);
  expect(paths(statutoryRuleFormSchema, change(2, { kind: "lta" }))).toEqual([
    "income_tax.claims.2.kind",
  ]);
  expect(paths(statutoryRuleFormSchema, change(1, { cap_group: "OTHER" }))).toEqual([
    "income_tax.claims.1.cap_group",
  ]);
  expect(paths(statutoryRuleFormSchema, change(1, { cap: "1,00,00,000.01" }))).toEqual([
    "income_tax.claims.1.cap",
  ]);
  expect(paths(statutoryRuleFormSchema, change(3, { cap: "" }))).toEqual([
    "income_tax.claims.3.cap",
  ]);
  expect(
    paths(statutoryRuleFormSchema, change(0, { metro_rate: "100.5", landlord_pan_above: "" })),
  ).toEqual(["income_tax.claims.0.metro_rate", "income_tax.claims.0.landlord_pan_above"]);
  expect(paths(statutoryRuleFormSchema, change(2, { ...claims.claims[0], code: "HRA2" }))).toEqual([
    "income_tax.claims.2.kind",
  ]);
  expect(messages(change(3, { code: "LOAN" }))).toBeUndefined();
  const unused = withTax({
    ...claims,
    claims: claims.claims.map((c) => ({ ...c, cap_group: "" })),
  });
  expect(paths(statutoryRuleFormSchema, unused)).toEqual(["income_tax.claim_groups.0.code"]);
  expect(
    paths(
      statutoryRuleFormSchema,
      withTax({ ...claims, claim_groups: [...claims.claim_groups, claims.claim_groups[0]] }),
    ),
  ).toEqual(["income_tax.claim_groups.1.code"]);
  expect(
    paths(
      statutoryRuleFormSchema,
      withTax({ ...claims, claim_groups: [{ ...claims.claim_groups[0], cap: "" }] }),
    ),
  ).toEqual(["income_tax.claim_groups.0.cap"]);
  expect(
    paths(
      statutoryRuleFormSchema,
      withTax({ ...claims, claims: Array.from({ length: 25 }, () => claims.claims[2]) }),
    ),
  ).toContain("income_tax.claims");
});

it("round-trips claim heads and counts them in the summary", () => {
  const input = toStatutoryRuleInput(statutoryRuleFormSchema.parse(withTax(claims)));
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
  expect(describeStatutoryRevision(revision)).toBe(
    "Tax year 2026-27 · 2 age bands · rebate to ₹1,50,000.00 · cess 5% · 4 claim heads",
  );
});

it("validates and converts claims and HRA in the preview", () => {
  const preview = {
    on: "2026-10-01",
    annual_income: "3,00,000",
    age: "40",
    claims: [
      { code: "medical", amount: "3,000" },
      { code: "SAVINGS", amount: "0" },
    ],
    hra: { received: "3,000", salary: "10,000", rent_paid: "6,000", city: "metro" },
  };
  expect(toIncomeTaxPreviewInput(incomeTaxPreviewFormSchema.parse(preview))).toEqual({
    on: "2026-10-01",
    annual_income_paise: 30_000_000,
    age: 40,
    claims: [
      { code: "MEDICAL", amount_paise: 300_000 },
      { code: "SAVINGS", amount_paise: 0 },
    ],
    hra: {
      received_paise: 300_000,
      salary_paise: 1_000_000,
      rent_paid_paise: 600_000,
      metro: true,
    },
  });
  const other = { ...preview, hra: { ...preview.hra, city: "other" } };
  expect(toIncomeTaxPreviewInput(incomeTaxPreviewFormSchema.parse(other)).hra?.metro).toBe(false);
  expect(
    paths(incomeTaxPreviewFormSchema, {
      ...preview,
      claims: [...preview.claims, { code: "Medical ", amount: "1" }],
    }),
  ).toEqual(["claims.2.code"]);
  expect(
    paths(incomeTaxPreviewFormSchema, { ...preview, claims: [{ code: "MEDICAL", amount: "-1" }] }),
  ).toEqual(["claims.0.amount"]);
  expect(
    paths(incomeTaxPreviewFormSchema, {
      ...preview,
      hra: { received: "1", salary: "", rent_paid: "", city: "" },
    }),
  ).toEqual(["hra.salary", "hra.rent_paid", "hra.city"]);
});
