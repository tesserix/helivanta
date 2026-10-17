import { expect, it } from "vitest";
import type { z } from "zod";
import {
  blankLandlord,
  blankLender,
  taxDeclarationFormOf,
  taxDeclarationFormSchema,
  taxDeclarationInputContract,
  taxDeclarationPath,
  taxDeclarationRevealFormSchema,
  taxDeclarationRevealInputContract,
  taxYearsAround,
  toTaxDeclarationInput,
  type TaxDeclarationForm,
} from "./payroll";

// Synthetic names and PANs only; none belongs to a real landlord or lender.
const blank: TaxDeclarationForm = {
  reason: "Synthetic Form 124",
  claims: [],
  hra: false,
  metro: false,
  landlords: [],
  home_loan: false,
  interest: "",
  lenders: [],
};
const landlord = {
  name: "Synthetic Landlord",
  address: "1 Synthetic Lane",
  rent: "90,000",
  pan: "none",
  pan_value: "",
} as const;
const lender = {
  name: "Synthetic Bank",
  address: "2 Synthetic Road",
  pan: "keep",
  pan_value: "",
} as const;
const issues = (schema: z.ZodTypeAny, v: object) =>
  schema.safeParse(v).error?.issues.map((i) => [i.path.join("."), i.message]);

it("builds the declaration path under the staff member's pay", () => {
  expect(taxDeclarationPath("/workforce/hospitals/h", "a b", "2026-27")).toBe(
    "/workforce/hospitals/h/hr/payroll/staff/a%20b/tax-declarations/2026-27",
  );
});

it("normalises the form into the contract the API accepts", () => {
  const form: TaxDeclarationForm = {
    ...blank,
    reason: "  Synthetic Form 124 ",
    claims: [{ code: " savings ", amount: "1,50,000.50" }],
    hra: true,
    metro: true,
    landlords: [
      { ...landlord, name: " Synthetic Landlord ", pan: "set", pan_value: " synfa1234a " },
    ],
    home_loan: true,
    interest: "2,00,000",
    lenders: [lender],
  };
  expect(taxDeclarationFormSchema.safeParse(form).success).toBe(true);
  const input = toTaxDeclarationInput(form, 2);
  expect(input).toEqual({
    expected_version: 2,
    reason: "Synthetic Form 124",
    claims: [{ code: "SAVINGS", amount_paise: 15000050 }],
    hra: {
      metro: true,
      landlords: [
        {
          name: "Synthetic Landlord",
          address: "1 Synthetic Lane",
          rent_paise: 9000000,
          pan: { action: "set", value: "SYNFA1234A" },
        },
      ],
    },
    home_loan: {
      interest_paise: 20000000,
      lenders: [{ name: "Synthetic Bank", address: "2 Synthetic Road", pan: { action: "keep" } }],
    },
  });
  expect(taxDeclarationInputContract.safeParse(input).success).toBe(true);
});

it("omits HRA and home loan when not claimed, whatever rows remain in the form", () => {
  const input = toTaxDeclarationInput(
    { ...blank, landlords: [landlord], lenders: [lender], interest: "x" },
    0,
  );
  expect(input).toEqual({ expected_version: 0, reason: "Synthetic Form 124", claims: [] });
  expect(taxDeclarationInputContract.safeParse(input).success).toBe(true);
  expect(
    taxDeclarationFormSchema.safeParse({ ...blank, landlords: [landlord], interest: "x" }).success,
  ).toBe(true);
});

it("reports each mistake inline on its own field", () => {
  expect(
    issues(taxDeclarationFormSchema, {
      ...blank,
      reason: " ",
      claims: [
        { code: "A B", amount: "1" },
        { code: "x", amount: "0" },
        { code: " X ", amount: "1" },
      ],
      hra: true,
      landlords: [
        {
          ...landlord,
          name: " ",
          address: "",
          rent: "1,00,00,000.01",
          pan: "set",
          pan_value: "SYN123",
        },
      ],
      home_loan: true,
      interest: "",
      lenders: [],
    }),
  ).toEqual([
    ["reason", "Enter a reason."],
    ["claims.0.code", "Use up to 40 letters, numbers, underscores or hyphens."],
    ["claims.1.amount", "Enter an amount up to ₹1,000 crore."],
    ["claims.2.code", "Declare each claim code once."],
    ["landlords.0.name", "Enter the name, up to 150 characters."],
    ["landlords.0.address", "Enter the address, up to 300 characters."],
    ["landlords.0.pan_value", "Enter the 10-character PAN, such as ABCDE1234F."],
    ["landlords.0.rent", "Enter the rent paid this year, up to ₹1 crore."],
    ["interest", "Enter the interest payable this year, up to ₹1 crore."],
    ["lenders", "Add 1 to 4 lenders."],
  ]);
});

it("caps parties and claims at what the API accepts", () => {
  const five = Array.from({ length: 5 }, () => lender);
  const many = Array.from({ length: 25 }, (_, i) => ({ code: `C${i}`, amount: "1" }));
  expect(
    issues(taxDeclarationFormSchema, { ...blank, home_loan: true, interest: "1", lenders: five }),
  ).toEqual([["lenders", "Add 1 to 4 lenders."]]);
  expect(issues(taxDeclarationFormSchema, { ...blank, hra: true, landlords: [] })).toEqual([
    ["landlords", "Add 1 to 4 landlords."],
  ]);
  expect(issues(taxDeclarationFormSchema, { ...blank, claims: many })).toEqual([
    ["claims", "Declare at most 24 claims."],
  ]);
});

it("accepts any holder type for a third party's PAN", () => {
  for (const pan of ["SYNFA1234A", "SYNCB1234B", "synhc1234c"])
    expect(
      taxDeclarationFormSchema.safeParse({
        ...blank,
        home_loan: true,
        interest: "1",
        lenders: [{ ...lender, pan: "set", pan_value: pan }],
      }).success,
    ).toBe(true);
});

it("requires a reason to reveal a PAN", () => {
  expect(issues(taxDeclarationRevealFormSchema, { reason: " " })).toEqual([
    ["reason", "Enter why you need to see it."],
  ]);
  expect(
    taxDeclarationRevealInputContract.safeParse({
      revision_id: "00000000-0000-4000-8000-000000000001",
      role: "lender",
      position: 5,
      reason: "Synthetic",
    }).success,
  ).toBe(false);
});

it("offers the tax years either side of today", () => {
  expect(taxYearsAround("2026-03-31")).toEqual(["2024-25", "2025-26", "2026-27"]);
  expect(taxYearsAround("2026-04-01")).toEqual(["2025-26", "2026-27", "2027-28"]);
  expect(taxYearsAround("2099-12-31")).toEqual(["2098-99", "2099-00", "2100-01"]);
});

it("starts a change from the declaration in force, keeping PANs on record", () => {
  expect(taxDeclarationFormOf()).toEqual({
    ...blank,
    reason: "",
    landlords: [blankLandlord()],
    lenders: [blankLender()],
  });
  const form = taxDeclarationFormOf({
    id: "00000000-0000-4000-8000-000000000001",
    staff_id: "00000000-0000-4000-8000-000000000002",
    tax_year: "2026-27",
    sequence: 1,
    base_version: 0,
    status: "approved",
    approved_version: 1,
    reason: "Synthetic",
    hra_metro: false,
    home_loan_interest_paise: 20000050,
    claims: [{ code: "SAVINGS", amount_paise: 15000000 }],
    parties: [
      {
        role: "landlord",
        position: 1,
        name: "Synthetic Landlord",
        address: "1 Synthetic Lane",
        rent_paise: 9000000,
        pan_masked: "XXXXXX234A",
      },
      { role: "lender", position: 1, name: "Synthetic Bank", address: "2 Synthetic Road" },
    ],
    proposed_by_subject: "user-admin",
    decision_note: "",
    created_at: "2026-04-01T00:00:00Z",
  });
  expect(form).toEqual({
    reason: "",
    claims: [{ code: "SAVINGS", amount: "150000" }],
    hra: true,
    metro: false,
    landlords: [
      {
        name: "Synthetic Landlord",
        address: "1 Synthetic Lane",
        rent: "90000",
        pan: "keep",
        pan_value: "",
      },
    ],
    home_loan: true,
    interest: "200000.50",
    lenders: [{ name: "Synthetic Bank", address: "2 Synthetic Road", pan: "none", pan_value: "" }],
  });
  expect(taxDeclarationFormSchema.safeParse({ ...form, reason: "Synthetic" }).success).toBe(true);
});
