import { z } from "zod";
import type {
  IncomeTaxParams,
  IncomeTaxPreviewInput,
  SalaryAssignmentInput,
  SalaryAssignmentRevision,
  SalaryHead,
  SalaryHeadInput,
  SalaryPreviewInput,
  SalaryStructure,
  SalaryStructureInput,
  StatutoryLwfSlab,
  StatutoryPfParams,
  StatutoryPreviewInput,
  StatutoryPtSlab,
  StatutoryRevision,
  StatutoryRuleInput,
  StaffSalaryPreviewInput,
  StaffIdentifiersInput,
  TaxRegimeElection,
  TaxRegimeInput,
} from "./payroll-contract";

export * from "./payroll-contract";

export const SALARY_BASES = {
  fixed: "Fixed amount",
  percent: "Percentage of other heads",
} as const;
export const SALARY_PRORATIONS = {
  none: "Not prorated",
  calendar_days: "Payable days ÷ days in the month",
  fixed_days: "Payable days ÷ a fixed number of days",
} as const;
export const SALARY_ROUNDING_MODES = {
  nearest: "Nearest (half up)",
  up: "Always up",
  down: "Always down",
} as const;
export const SALARY_ROUNDING_UNITS = {
  1: "1 paisa",
  50: "50 paise",
  100: "₹1",
  500: "₹5",
  1000: "₹10",
} as const;
export const SALARY_TAXABILITY = {
  taxable: "Taxable",
  exempt: "Exempt",
  not_applicable: "Not applicable",
} as const;
export const SALARY_WAGE_CLASSES = {
  wages: "Wages",
  excluded: "Excluded from wages",
  outside: "Outside remuneration",
} as const;
export const MONTH_NAMES = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec",
] as const;

const MAX_AMOUNT_PAISE = 1_000_000_000;

// Decimal strings are parsed digit by digit so no amount or rate ever passes through a float.
export const parseRupees = (value: string): number | undefined => {
  const match = /^(\d{1,8})(?:\.(\d{1,2}))?$/.exec(value.trim().replaceAll(",", ""));
  if (!match) return undefined;
  const paise = Number(match[1]) * 100 + Number((match[2] ?? "").padEnd(2, "0"));
  return paise <= MAX_AMOUNT_PAISE ? paise : undefined;
};

export const parsePercent = (value: string): number | undefined => {
  const match = /^(\d{1,3})(?:\.(\d{1,4}))?$/.exec(value.trim());
  if (!match) return undefined;
  const ppm = Number(match[1]) * 10_000 + Number((match[2] ?? "").padEnd(4, "0"));
  return ppm >= 1 && ppm <= 1_000_000 ? ppm : undefined;
};

export const parseHalfDays = (value: string): number | undefined => {
  const match = /^(\d{1,2})(?:\.(0|5))?$/.exec(value.trim());
  if (!match) return undefined;
  const half = Number(match[1]) * 2 + (match[2] === "5" ? 1 : 0);
  return half <= 62 ? half : undefined;
};

const groupIndian = (digits: string) =>
  digits.length <= 3
    ? digits
    : `${digits.slice(0, -3).replace(/\B(?=(\d{2})+$)/g, ",")},${digits.slice(-3)}`;

export const formatPaise = (paise: number) => {
  const magnitude = Math.abs(paise);
  return `${paise < 0 ? "−" : ""}₹${groupIndian(String(Math.floor(magnitude / 100)))}.${String(magnitude % 100).padStart(2, "0")}`;
};

export const paiseToRupees = (paise: number) =>
  `${Math.floor(paise / 100)}${paise % 100 ? `.${String(paise % 100).padStart(2, "0")}` : ""}`;

export const ppmToPercent = (ppm: number) => {
  const fraction = String(ppm % 10_000)
    .padStart(4, "0")
    .replace(/0+$/, "");
  return `${Math.floor(ppm / 10_000)}${fraction ? `.${fraction}` : ""}`;
};

export const halfDaysToDays = (half: number) => `${Math.floor(half / 2)}${half % 2 ? ".5" : ""}`;

const text = (label: string, max: number) =>
  z
    .string()
    .trim()
    .min(1, `Enter ${label}.`)
    .max(max, `Keep ${label} within ${max} characters.`)
    .refine((value) => !value.includes("\0"), "Remove invalid characters.");

const code = z
  .string()
  .trim()
  .toUpperCase()
  .regex(/^[A-Z0-9_-]{1,40}$/, "Use up to 40 letters, numbers, underscores or hyphens.");

const date = z.string().date("Choose a valid date.");

export const salaryHeadFormSchema = z
  .object({
    expected_version: z.number().int().min(0),
    kind: z.enum(["earning", "deduction", "employer_contribution"]),
    code,
    name: text("a name", 80),
    active: z.boolean(),
    basis: z.enum(["fixed", "percent"]),
    rate: z.string(),
    base_head_ids: z.array(z.string()),
    proration: z.enum(["none", "calendar_days", "fixed_days"]),
    proration_days: z.string(),
    minimum_payable_days: z.string(),
    months: z.array(z.number().int().min(1).max(12)),
    rounding_mode: z.enum(["nearest", "up", "down"]),
    rounding_unit_paise: z.enum(["1", "50", "100", "500", "1000"]),
    min_amount: z.string(),
    max_amount: z.string(),
    in_gross: z.boolean(),
    in_ctc: z.boolean(),
    arrears_eligible: z.boolean(),
    taxability: z.enum(["taxable", "exempt", "not_applicable"]),
    tax_note: z.string(),
    wage_class: z.enum(["wages", "excluded", "outside"]),
    payslip_label: text("a payslip label", 40),
    payslip_order: z
      .number({ invalid_type_error: "Enter the payslip position." })
      .int("Enter a whole number.")
      .min(0, "Use a position from 0 to 999.")
      .max(999, "Use a position from 0 to 999."),
    show_when_zero: z.boolean(),
    effective_on: date,
    reviewed: z.boolean(),
  })
  .strict()
  .superRefine((value, ctx) => {
    const issue = (path: string, message: string) =>
      ctx.addIssue({ code: "custom", path: [path], message });
    const unit = Number(value.rounding_unit_paise);
    if (!value.reviewed) issue("reviewed", "Confirm that you reviewed the head.");
    if (value.basis === "percent") {
      if (parsePercent(value.rate) === undefined)
        issue("rate", "Enter a percentage above 0 and at most 100, with up to 4 decimals.");
      if (value.base_head_ids.length === 0) issue("base_head_ids", "Choose at least one base.");
      if (value.base_head_ids.length > 20) issue("base_head_ids", "Choose no more than 20 bases.");
    }
    if (value.proration === "fixed_days") {
      const days = Number(value.proration_days);
      if (!/^\d{1,2}$/.test(value.proration_days.trim()) || days < 1 || days > 31)
        issue("proration_days", "Enter a whole number of days from 1 to 31.");
    }
    if (parseHalfDays(value.minimum_payable_days) === undefined)
      issue("minimum_payable_days", "Enter 0 to 31 days, in steps of half a day.");
    if (value.months.length === 0) issue("months", "Choose at least one month.");
    const bound = (field: "min_amount" | "max_amount") => {
      if (value[field].trim() === "") return undefined;
      const paise = parseRupees(value[field]);
      if (paise === undefined)
        issue(field, "Enter rupees with up to 2 decimals, at most ₹1 crore.");
      else if (paise % unit !== 0)
        issue(field, `Use a multiple of the rounding unit (${SALARY_ROUNDING_UNITS[unit as 1]}).`);
      return paise;
    };
    const floor = bound("min_amount");
    const cap = bound("max_amount");
    if (floor !== undefined && cap !== undefined && floor > cap)
      issue("max_amount", "Set the cap at or above the floor.");
    if (value.kind !== "deduction" && value.taxability === "exempt" && value.tax_note.trim() === "")
      issue("tax_note", "Cite the basis for the exemption.");
    if (value.tax_note.trim().length > 300)
      issue("tax_note", "Keep the tax note within 300 characters.");
  });
export type SalaryHeadForm = z.infer<typeof salaryHeadFormSchema>;

// Kind fixes the inclusion, taxability and wage-class choices the server accepts.
export const headKindRules = (kind: SalaryHeadForm["kind"]) => ({
  grossAllowed: kind === "earning",
  ctcAllowed: kind !== "deduction",
  taxability:
    kind === "deduction" ? (["not_applicable"] as const) : (["taxable", "exempt"] as const),
  wageClasses:
    kind === "deduction" ? (["outside"] as const) : (["wages", "excluded", "outside"] as const),
});

export const newSalaryHeadForm = (today: string): SalaryHeadForm => ({
  expected_version: 0,
  kind: "earning",
  code: "",
  name: "",
  active: true,
  basis: "fixed",
  rate: "",
  base_head_ids: [],
  proration: "calendar_days",
  proration_days: "",
  minimum_payable_days: "0",
  months: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12],
  rounding_mode: "nearest",
  rounding_unit_paise: "100",
  min_amount: "",
  max_amount: "",
  in_gross: true,
  in_ctc: true,
  arrears_eligible: true,
  taxability: "taxable",
  tax_note: "",
  wage_class: "wages",
  payslip_label: "",
  payslip_order: 10,
  show_when_zero: false,
  effective_on: today,
  reviewed: false,
});

export const salaryHeadToForm = (head: SalaryHead): SalaryHeadForm => ({
  expected_version: head.version,
  kind: head.kind,
  code: head.code,
  name: head.name,
  active: head.active,
  basis: head.basis,
  rate: head.rate_ppm === undefined ? "" : ppmToPercent(head.rate_ppm),
  base_head_ids: head.base_head_ids,
  proration: head.proration,
  proration_days: head.proration_days === undefined ? "" : String(head.proration_days),
  minimum_payable_days: halfDaysToDays(head.minimum_payable_half_days),
  months: head.months,
  rounding_mode: head.rounding_mode,
  rounding_unit_paise: String(head.rounding_unit_paise) as SalaryHeadForm["rounding_unit_paise"],
  min_amount: head.min_amount_paise === undefined ? "" : paiseToRupees(head.min_amount_paise),
  max_amount: head.max_amount_paise === undefined ? "" : paiseToRupees(head.max_amount_paise),
  in_gross: head.in_gross,
  in_ctc: head.in_ctc,
  arrears_eligible: head.arrears_eligible,
  taxability: head.taxability,
  tax_note: head.tax_note ?? "",
  wage_class: head.wage_class,
  payslip_label: head.payslip_label,
  payslip_order: head.payslip_order,
  show_when_zero: head.show_when_zero,
  effective_on: head.effective_on,
  reviewed: false,
});

// Fields the chosen basis, proration, kind or taxability make irrelevant are dropped, so the body always passes the server's checks.
export const toSalaryHeadInput = (form: SalaryHeadForm): SalaryHeadInput => {
  const rules = headKindRules(form.kind);
  const percent = form.basis === "percent";
  const proration = percent ? "none" : form.proration;
  const floor = form.min_amount.trim() === "" ? undefined : parseRupees(form.min_amount);
  const cap = form.max_amount.trim() === "" ? undefined : parseRupees(form.max_amount);
  return {
    expected_version: form.expected_version,
    kind: form.kind,
    code: form.code,
    name: form.name,
    active: form.active,
    basis: form.basis,
    ...(percent && { rate_ppm: parsePercent(form.rate) }),
    base_head_ids: percent ? [...form.base_head_ids].sort() : [],
    proration,
    ...(proration === "fixed_days" && { proration_days: Number(form.proration_days) }),
    minimum_payable_half_days: parseHalfDays(form.minimum_payable_days) ?? 0,
    months: [...form.months].sort((a, b) => a - b),
    rounding_mode: form.rounding_mode,
    rounding_unit_paise: Number(form.rounding_unit_paise),
    ...(floor !== undefined && { min_amount_paise: floor }),
    ...(cap !== undefined && { max_amount_paise: cap }),
    in_gross: rules.grossAllowed && form.in_gross,
    in_ctc: rules.ctcAllowed && form.in_ctc,
    arrears_eligible: form.arrears_eligible,
    taxability: form.kind === "deduction" ? "not_applicable" : form.taxability,
    ...(form.kind !== "deduction" &&
      form.taxability === "exempt" && { tax_note: form.tax_note.trim() }),
    wage_class: form.kind === "deduction" ? "outside" : form.wage_class,
    payslip_label: form.payslip_label,
    payslip_order: form.payslip_order,
    show_when_zero: form.show_when_zero,
    effective_on: form.effective_on,
    reviewed: form.reviewed,
  };
};

export const salaryStructureFormSchema = z
  .object({
    expected_version: z.number().int().min(0),
    code,
    name: text("a name", 80),
    description: z
      .string()
      .trim()
      .max(500, "Keep the description within 500 characters.")
      .refine((value) => !value.includes("\0"), "Remove invalid characters."),
    active: z.boolean(),
    department_id: z.string(),
    designation_id: z.string(),
    grade_id: z.string(),
    lines: z
      .array(z.object({ head_id: z.string().min(1, "Choose a head."), amount: z.string() }))
      .min(1, "Add at least one head.")
      .max(60, "Use no more than 60 heads."),
    effective_on: date,
    reviewed: z.boolean(),
  })
  .strict();
export type SalaryStructureForm = z.infer<typeof salaryStructureFormSchema>;

// Fixed heads need an amount and percentage heads must not have one; the heads list decides which is which.
export const salaryStructureSchemaFor = (heads: Pick<SalaryHead, "id" | "basis">[]) => {
  const basis = new Map(heads.map((h) => [h.id, h.basis]));
  return salaryStructureFormSchema.superRefine((value, ctx) => {
    const issue = (path: (string | number)[], message: string) =>
      ctx.addIssue({ code: "custom", path, message });
    if (!value.reviewed) issue(["reviewed"], "Confirm that you reviewed the structure.");
    const seen = new Set<string>();
    value.lines.forEach((line, index) => {
      if (seen.has(line.head_id)) issue(["lines", index, "head_id"], "Use each head once.");
      seen.add(line.head_id);
      if (basis.get(line.head_id) === "fixed" && parseRupees(line.amount) === undefined)
        issue(["lines", index, "amount"], "Enter rupees with up to 2 decimals, at most ₹1 crore.");
    });
  });
};

export const newSalaryStructureForm = (today: string): SalaryStructureForm => ({
  expected_version: 0,
  code: "",
  name: "",
  description: "",
  active: true,
  department_id: "",
  designation_id: "",
  grade_id: "",
  lines: [{ head_id: "", amount: "" }],
  effective_on: today,
  reviewed: false,
});

export const salaryStructureToForm = (structure: SalaryStructure): SalaryStructureForm => ({
  expected_version: structure.version,
  code: structure.code,
  name: structure.name,
  description: structure.description,
  active: structure.active,
  department_id: structure.department_id ?? "",
  designation_id: structure.designation_id ?? "",
  grade_id: structure.grade_id ?? "",
  lines: structure.lines.map((l) => ({
    head_id: l.head_id,
    amount: l.amount_paise === undefined ? "" : paiseToRupees(l.amount_paise),
  })),
  effective_on: structure.effective_on,
  reviewed: false,
});

export const toSalaryStructureInput = (
  form: SalaryStructureForm,
  heads: Pick<SalaryHead, "id" | "basis">[],
): SalaryStructureInput => {
  const basis = new Map(heads.map((h) => [h.id, h.basis]));
  return {
    expected_version: form.expected_version,
    code: form.code,
    name: form.name,
    description: form.description,
    active: form.active,
    ...(form.department_id && { department_id: form.department_id }),
    ...(form.designation_id && { designation_id: form.designation_id }),
    ...(form.grade_id && { grade_id: form.grade_id }),
    lines: form.lines.map((l) =>
      basis.get(l.head_id) === "fixed"
        ? { head_id: l.head_id, amount_paise: parseRupees(l.amount) }
        : { head_id: l.head_id },
    ),
    effective_on: form.effective_on,
    reviewed: form.reviewed,
  };
};

const daysIn = (day: string) => {
  const [y, m] = day.split("-").map(Number);
  return new Date(Date.UTC(y!, m!, 0)).getUTCDate();
};

export const salaryPreviewFormSchema = z
  .object({ on: date, payable_days: z.string() })
  .strict()
  .superRefine((value, ctx) => {
    const half = parseHalfDays(value.payable_days);
    if (half === undefined)
      ctx.addIssue({
        code: "custom",
        path: ["payable_days"],
        message: "Enter days in steps of half a day.",
      });
    else if (z.string().date().safeParse(value.on).success && half > 2 * daysIn(value.on))
      ctx.addIssue({
        code: "custom",
        path: ["payable_days"],
        message: `Use no more than the ${daysIn(value.on)} days in that month.`,
      });
  });
export type SalaryPreviewForm = z.infer<typeof salaryPreviewFormSchema>;

export const toSalaryPreviewInput = (form: SalaryPreviewForm): SalaryPreviewInput => ({
  on: form.on,
  payable_half_days: parseHalfDays(form.payable_days) ?? 0,
});

export const salaryHeadsPath = (base: string, kind?: string) =>
  `${base}/hr/payroll/salary-heads${kind && kind !== "all" ? `?kind=${encodeURIComponent(kind)}` : ""}`;

export const salaryStructuresPath = (base: string) => `${base}/hr/payroll/salary-structures`;

// Describes how a head computes, for lists and history.
export const describeSalaryHead = (head: SalaryHead, names: Map<string, string>) => {
  const rule =
    head.basis === "percent"
      ? `${ppmToPercent(head.rate_ppm ?? 0)}% of ${head.base_head_ids.map((id) => names.get(id) ?? "an unavailable head").join(" + ")}`
      : SALARY_PRORATIONS[head.proration];
  const months =
    head.months.length === 12 ? "" : ` · ${head.months.map((m) => MONTH_NAMES[m - 1]).join(", ")}`;
  return `${rule}${months}`;
};

export const STATUTORY_WAGE_BASES = {
  statutory_wages: "Labour Code wages",
  gross: "Gross pay",
} as const;
export const STATUTORY_CONTRIBUTION_BASES = {
  capped: "Capped at the wage ceiling",
  actual: "Actual wages",
} as const;
export const STATUTORY_STATUSES = {
  pending: "Awaiting approval",
  approved: "Approved",
  rejected: "Rejected",
  withdrawn: "Withdrawn",
} as const;
export const STATUTORY_RESULT_STATUSES = {
  applied: "Applied",
  not_covered: "Not covered",
  above_ceiling: "Above the coverage ceiling",
  not_configured: "Not configured",
  not_due: "Not due this month",
  gender_required: "Needs the employee's gender",
} as const;
export const STATUTORY_COMPONENTS = {
  pf_employee: "PF employee share",
  pf_employer_epf: "PF employer share (EPF)",
  pf_employer_eps: "Pension (EPS)",
  pf_edli: "EDLI",
  pf_admin: "PF admin charges",
  esi_employee: "ESI employee share",
  esi_employer: "ESI employer share",
  pt_employee: "Professional tax",
  lwf_employee: "LWF employee share",
  lwf_employer: "LWF employer share",
} as const;
export const STATUTORY_GENDERS = {
  female: "Women",
  male: "Men",
  other: "Other genders",
} as const;
type Gender = keyof typeof STATUTORY_GENDERS;
const GENDERS = Object.keys(STATUTORY_GENDERS) as Gender[];
const MAX_LEVY_PAISE = 10_000_000;

// Statutory rates may legitimately be zero, unlike salary head rates.
export const parseRate = (value: string): number | undefined => {
  const match = /^(\d{1,3})(?:\.(\d{1,4}))?$/.exec(value.trim());
  if (!match) return undefined;
  const ppm = Number(match[1]) * 10_000 + Number((match[2] ?? "").padEnd(4, "0"));
  return ppm <= 1_000_000 ? ppm : undefined;
};

const choice = <T extends string>(options: Record<T, string>) =>
  z.string().refine((value) => value in options, "Choose an option.");

// Every statutory choice starts blank: nothing is pre-loaded, so each value is read from the cited source.
const pfFormSchema = z
  .object({
    wage_basis: z.string(),
    contribution_basis: z.string(),
    wage_ceiling: z.string(),
    employee_rate: z.string(),
    employer_rate: z.string(),
    eps_rate: z.string(),
    eps_wage_ceiling: z.string(),
    edli_rate: z.string(),
    edli_wage_ceiling: z.string(),
    admin_rate: z.string(),
    admin_minimum: z.string(),
    rounding_mode: z.string(),
    rounding_unit: z.string(),
  })
  .strict();
const esiFormSchema = z
  .object({
    wage_basis: z.string(),
    wage_ceiling: z.string(),
    disability_wage_ceiling: z.string(),
    employee_rate: z.string(),
    employer_rate: z.string(),
    employee_exempt_daily: z.string(),
    period_start_months: z.array(z.number().int().min(1).max(12)),
    rounding_mode: z.string(),
    rounding_unit: z.string(),
  })
  .strict();

const ptSlabFormSchema = z
  .object({
    months: z.array(z.number().int().min(1).max(12)),
    genders: z.array(z.enum(["female", "male", "other"])),
    from: z.string(),
    to: z.string(),
    amount: z.string(),
  })
  .strict();
const ptFormSchema = z
  .object({ state_code: z.string(), wage_basis: z.string(), slabs: z.array(ptSlabFormSchema) })
  .strict();
const lwfSlabFormSchema = z
  .object({
    from: z.string(),
    to: z.string(),
    employee_fixed: z.string(),
    employee_rate: z.string(),
    employee_cap: z.string(),
    employer_fixed: z.string(),
    employer_rate: z.string(),
    employer_cap: z.string(),
  })
  .strict();
const lwfFormSchema = z
  .object({
    state_code: z.string(),
    wage_basis: z.string(),
    deduction_months: z.array(z.number().int().min(1).max(12)),
    rounding_mode: z.string(),
    rounding_unit: z.string(),
    slabs: z.array(lwfSlabFormSchema),
  })
  .strict();
export type StatutoryPtSlabForm = z.infer<typeof ptSlabFormSchema>;
export type StatutoryLwfSlabForm = z.infer<typeof lwfSlabFormSchema>;

const optionalRupees = (raw: string) => (raw.trim() === "" ? undefined : parseRupees(raw));

const MAX_INCOME_PAISE = 100_000_000_000;
const MAX_STANDARD_DEDUCTION_PAISE = 100_000_000;

// Income-tax amounts reach surcharge thresholds in crores, beyond the salary parser's ceiling.
export const parseIncome = (value: string): number | undefined => {
  const match = /^(\d{1,10})(?:\.(\d{1,2}))?$/.exec(value.trim().replaceAll(",", ""));
  if (!match) return undefined;
  const paise = Number(match[1]) * 100 + Number((match[2] ?? "").padEnd(2, "0"));
  return paise <= MAX_INCOME_PAISE ? paise : undefined;
};
const optionalIncome = (raw: string) => (raw.trim() === "" ? undefined : parseIncome(raw));

// The Act's tax year runs from 1 April to 31 March and is written YYYY-YY.
export const taxYearOf = (day: string) => {
  const [year, month] = day.split("-").map(Number) as [number, number];
  const start = month < 4 ? year - 1 : year;
  return `${start}-${String((start + 1) % 100).padStart(2, "0")}`;
};

export const INCOME_TAX_REBATES = {
  none: "No rebate",
  relief: "Rebate with marginal relief",
  cliff: "Rebate without marginal relief",
} as const;
export const INCOME_TAX_RESULT_STATUSES = {
  applied: "Applied",
  not_covered: "Not covered",
  not_configured: "Not configured",
  tax_year_missing: "No rule for this tax year",
  age_required: "Needs the employee's age",
} as const;

const taxSlabFormSchema = z.object({ from: z.string(), to: z.string(), rate: z.string() }).strict();
const incomeTaxBandFormSchema = z
  .object({ min_age: z.string(), slabs: z.array(taxSlabFormSchema) })
  .strict();
const surchargeFormSchema = z.object({ above: z.string(), rate: z.string() }).strict();
const incomeTaxFormSchema = z
  .object({
    tax_year: z.string(),
    standard_deduction: z.string(),
    taxable_rounding_mode: z.string(),
    taxable_rounding_unit: z.string(),
    tax_rounding_mode: z.string(),
    tax_rounding_unit: z.string(),
    age_bands: z.array(incomeTaxBandFormSchema),
    rebate: z.string(),
    rebate_income_limit: z.string(),
    rebate_max: z.string(),
    surcharges: z.array(surchargeFormSchema),
    cess_rate: z.string(),
  })
  .strict();
export type IncomeTaxSlabForm = z.infer<typeof taxSlabFormSchema>;
export type IncomeTaxBandForm = z.infer<typeof incomeTaxBandFormSchema>;
export type IncomeTaxSurchargeForm = z.infer<typeof surchargeFormSchema>;
type IncomeTaxForm = z.infer<typeof incomeTaxFormSchema>;
type Issue = (path: (string | number)[], message: string) => void;

const parseAge = (raw: string) => (/^\d{1,3}$/.test(raw.trim()) ? Number(raw) : undefined);

// Mirrors the server: the year contains the effective date, one band starts at age 0, slabs partition ₹0 upward and surcharge thresholds rise.
const incomeTaxIssues = (form: IncomeTaxForm, effectiveOn: string, issue: Issue) => {
  const rateIssue = (path: (string | number)[], raw: string) => {
    if (parseRate(raw) === undefined)
      issue(path, "Enter a percentage from 0 to 100, with up to 4 decimals.");
  };
  const pick = (field: string, raw: string, options: Record<string, string>) => {
    if (!choice(options).safeParse(raw).success) issue([field], "Choose an option.");
  };
  const amount = (path: (string | number)[], raw: string) => {
    const paise = parseIncome(raw);
    if (paise === undefined || paise < 1)
      issue(path, "Enter rupees above 0, with up to 2 decimals.");
    return paise;
  };
  const year = /^(\d{4})-(\d{2})$/.exec(form.tax_year.trim());
  if (
    !year ||
    (Number(year[1]) + 1) % 100 !== Number(year[2]) ||
    Number(year[1]) < 2000 ||
    Number(year[1]) > 2199
  )
    issue(["tax_year"], "Enter the tax year as YYYY-YY, for example 2026-27.");
  else if (
    /^\d{4}-\d{2}-\d{2}$/.test(effectiveOn) &&
    taxYearOf(effectiveOn) !== form.tax_year.trim()
  )
    issue(
      ["tax_year"],
      `The effective date falls in tax year ${taxYearOf(effectiveOn)}. A rule covers one tax year, from 1 April to 31 March.`,
    );
  const deduction = parseRupees(form.standard_deduction);
  if (deduction === undefined || deduction > MAX_STANDARD_DEDUCTION_PAISE)
    issue(["standard_deduction"], "Enter up to ₹10 lakh, with up to 2 decimals. Enter 0 for none.");
  pick("taxable_rounding_mode", form.taxable_rounding_mode, SALARY_ROUNDING_MODES);
  pick("taxable_rounding_unit", form.taxable_rounding_unit, SALARY_ROUNDING_UNITS);
  pick("tax_rounding_mode", form.tax_rounding_mode, SALARY_ROUNDING_MODES);
  pick("tax_rounding_unit", form.tax_rounding_unit, SALARY_ROUNDING_UNITS);
  rateIssue(["cess_rate"], form.cess_rate);
  pick("rebate", form.rebate, INCOME_TAX_REBATES);
  if (form.rebate === "relief" || form.rebate === "cliff") {
    amount(["rebate_income_limit"], form.rebate_income_limit);
    amount(["rebate_max"], form.rebate_max);
  }
  if (form.age_bands.length === 0 || form.age_bands.length > 4)
    issue(["age_bands"], "Add between 1 and 4 age bands.");
  const ages = form.age_bands.map((b) => parseAge(b.min_age));
  for (const [i, band] of form.age_bands.entries()) {
    const age = ages[i];
    if (age === undefined || age > 120)
      issue(["age_bands", i, "min_age"], "Enter an age from 0 to 120.");
    else if (ages.indexOf(age) !== i)
      issue(["age_bands", i, "min_age"], "Each band needs its own starting age.");
    if (band.slabs.length === 0 || band.slabs.length > 16) {
      issue(["age_bands", i, "slabs"], "Add between 1 and 16 slabs.");
      continue;
    }
    let complete = true;
    for (const [j, slab] of band.slabs.entries()) {
      const path = ["age_bands", i, "slabs", j];
      const start = parseIncome(slab.from);
      if (start === undefined) issue([...path, "from"], "Enter rupees with up to 2 decimals.");
      const end = optionalIncome(slab.to);
      const bounded = slab.to.trim() !== "";
      if (bounded && (end === undefined || (start !== undefined && end <= start)))
        issue([...path, "to"], "Enter an amount above the start, or leave it blank for no limit.");
      rateIssue([...path, "rate"], slab.rate);
      complete =
        complete && start !== undefined && (!bounded || (end !== undefined && end > start));
    }
    const problem = complete
      ? statutoryBandProblem(
          band.slabs.map((s) => ({
            from: parseIncome(s.from) as number,
            to: optionalIncome(s.to),
          })),
        )
      : undefined;
    if (problem)
      issue(["age_bands", i, "slabs"], `The table ${problem.replaceAll("wages", "income")}.`);
  }
  if (!ages.includes(0) && form.age_bands.length > 0)
    issue(["age_bands"], "One band must start at age 0, so everyone has a slab table.");
  if (form.surcharges.length > 8) issue(["surcharges"], "Add at most 8 surcharge bands.");
  let previous: number | undefined;
  for (const [i, band] of form.surcharges.entries()) {
    const above = amount(["surcharges", i, "above"], band.above);
    rateIssue(["surcharges", i, "rate"], band.rate);
    if (above !== undefined && previous !== undefined && above <= previous)
      issue(["surcharges", i, "above"], "Each threshold must be above the one before it.");
    previous = above ?? previous;
  }
};

type Band = { from: number; to?: number };
// Mirrors the server's partition rule and names the first fault, so the editor can say what to fix.
export const statutoryBandProblem = (bands: Band[]): string | undefined => {
  const sorted = [...bands].sort((a, b) => a.from - b.from);
  if (sorted.length === 0) return "has no slab";
  let next = 0;
  for (const [i, band] of sorted.entries()) {
    if (band.from !== next)
      return band.from > next
        ? `leaves wages from ${formatPaise(next)} to ${formatPaise(band.from)} without a slab`
        : `has slabs that overlap from ${formatPaise(band.from)}`;
    if (band.to === undefined && i < sorted.length - 1)
      return "has an open-ended slab before the last one";
    if (band.to === undefined) return undefined;
    next = band.to;
  }
  return `needs a last slab from ${formatPaise(next)} with no upper limit`;
};

const parsedPtSlabs = (slabs: StatutoryPtSlabForm[]) =>
  slabs.map((s) => ({
    months: s.months,
    genders: s.genders,
    from: parseRupees(s.from),
    to: optionalRupees(s.to),
    amount: parseRupees(s.amount),
  }));

// Every gender must be partitioned in every month any slab names; an unnamed month has no PT.
const ptProblem = (slabs: StatutoryPtSlabForm[]) => {
  const parsed = parsedPtSlabs(slabs) as {
    months: number[];
    genders: Gender[];
    from: number;
    to?: number;
  }[];
  for (let month = 1; month <= 12; month++) {
    if (!parsed.some((s) => s.months.includes(month))) continue;
    for (const gender of GENDERS) {
      const problem = statutoryBandProblem(
        parsed.filter((s) => s.months.includes(month) && s.genders.includes(gender)),
      );
      if (problem)
        return `${MONTH_NAMES[month - 1]}, ${STATUTORY_GENDERS[gender].toLowerCase()}: the table ${problem}.`;
    }
  }
  return undefined;
};

// The highest yearly PT a person of each gender can owe, for the approver to check against the constitutional ceiling.
export const ptAnnualTotals = (
  slabs: StatutoryPtSlabForm[],
): Record<Gender, number> | undefined => {
  const parsed = parsedPtSlabs(slabs);
  if (parsed.some((s) => s.from === undefined || s.amount === undefined)) return undefined;
  const totals = { female: 0, male: 0, other: 0 };
  for (const gender of GENDERS)
    for (const wages of new Set(parsed.map((s) => s.from as number))) {
      let year = 0;
      for (let month = 1; month <= 12; month++)
        year +=
          parsed.find(
            (s) =>
              s.months.includes(month) &&
              s.genders.includes(gender) &&
              wages >= (s.from as number) &&
              (s.to === undefined || wages < s.to),
          )?.amount ?? 0;
      totals[gender] = Math.max(totals[gender], year);
    }
  return totals;
};

export const statutoryRuleFormSchema = z
  .object({
    expected_version: z.number().int().min(0),
    kind: z.enum(["pf", "esi", "pt", "lwf", "income_tax_new", "income_tax_old"]),
    name: text("a name", 80),
    coverage: z.string(),
    effective_on: date,
    citation: text("the notification or section relied on", 500),
    reference: z
      .string()
      .trim()
      .max(300, "Keep the reference within 300 characters.")
      .refine((value) => !value.includes("\0"), "Remove invalid characters."),
    pf: pfFormSchema,
    esi: esiFormSchema,
    pt: ptFormSchema,
    lwf: lwfFormSchema,
    income_tax: incomeTaxFormSchema,
  })
  .strict()
  .superRefine((value, ctx) => {
    const issue = (path: string[], message: string) =>
      ctx.addIssue({ code: "custom", path, message });
    if (!["covered", "not_covered"].includes(value.coverage))
      issue(["coverage"], "State whether the hospital is covered.");
    if (value.coverage !== "covered") return;
    if (value.kind === "income_tax_new" || value.kind === "income_tax_old")
      return incomeTaxIssues(value.income_tax, value.effective_on, (path, message) =>
        issue(["income_tax", ...path] as string[], message),
      );
    const at = (...field: (string | number)[]) => [value.kind, ...field] as string[];
    const pick = (field: string, raw: string, options: Record<string, string>) => {
      if (!choice(options).safeParse(raw).success) issue(at(field), "Choose an option.");
    };
    const money = (field: string, raw: string, min = 0) => {
      const paise = parseRupees(raw);
      if (paise !== undefined && paise >= min) return paise;
      issue(at(field), `Enter rupees with up to 2 decimals${min ? ", above 0" : ""}.`);
      return undefined;
    };
    const rate = (field: string, raw: string) => {
      const ppm = parseRate(raw);
      if (ppm === undefined)
        issue(at(field), "Enter a percentage from 0 to 100, with up to 4 decimals.");
      return ppm;
    };
    // Slab fields report under their row so the editor can show each error beside its input.
    const levy = (path: (string | number)[], raw: string, optional = false) => {
      const paise = optional ? optionalRupees(raw) : parseRupees(raw);
      if (paise === undefined ? !(optional && raw.trim() === "") : paise > MAX_LEVY_PAISE)
        issue(
          at(...path),
          `Enter up to ₹1 lakh, with up to 2 decimals${optional ? ", or leave it blank" : ""}.`,
        );
    };
    const band = (path: (string | number)[], from: string, to: string) => {
      const start = parseRupees(from);
      if (start === undefined) issue(at(...path, "from"), "Enter rupees with up to 2 decimals.");
      const end = optionalRupees(to);
      if (to.trim() !== "" && (end === undefined || (start !== undefined && end <= start)))
        issue(
          at(...path, "to"),
          "Enter an amount above the start, or leave it blank for no limit.",
        );
      return start !== undefined && (to.trim() === "" || (end !== undefined && end > start));
    };
    const state = (raw: string) => {
      if (!/^IN-[A-Z]{2}$/.test(raw.trim().toUpperCase()))
        issue(at("state_code"), "Enter the state code, for example IN-MH.");
    };
    const months = (field: string, chosen: number[], message: string) => {
      if (chosen.length === 0) issue(at(field), message);
    };
    if (value.kind === "pt") {
      const pt = value.pt;
      state(pt.state_code);
      pick("wage_basis", pt.wage_basis, STATUTORY_WAGE_BASES);
      if (pt.slabs.length === 0 || pt.slabs.length > 96)
        return issue(at("slabs"), "Add between 1 and 96 slabs.");
      let complete = true;
      for (const [i, slab] of pt.slabs.entries()) {
        if (slab.months.length === 0) issue(at("slabs", i, "months"), "Choose its months.");
        if (slab.genders.length === 0)
          issue(at("slabs", i, "genders"), "Choose who it applies to.");
        levy(["slabs", i, "amount"], slab.amount);
        complete = band(["slabs", i], slab.from, slab.to) && slab.months.length > 0 && complete;
      }
      const problem = complete ? ptProblem(pt.slabs) : undefined;
      if (problem) issue(at("slabs"), problem);
      return;
    }
    if (value.kind === "lwf") {
      const lwf = value.lwf;
      state(lwf.state_code);
      pick("wage_basis", lwf.wage_basis, STATUTORY_WAGE_BASES);
      pick("rounding_mode", lwf.rounding_mode, SALARY_ROUNDING_MODES);
      pick("rounding_unit", lwf.rounding_unit, SALARY_ROUNDING_UNITS);
      months("deduction_months", lwf.deduction_months, "Choose the months LWF is deducted in.");
      if (lwf.slabs.length === 0 || lwf.slabs.length > 24)
        return issue(at("slabs"), "Add between 1 and 24 slabs.");
      let complete = true;
      for (const [i, slab] of lwf.slabs.entries()) {
        for (const party of ["employee", "employer"] as const) {
          levy(["slabs", i, `${party}_fixed`], slab[`${party}_fixed`]);
          if (parseRate(slab[`${party}_rate`]) === undefined)
            issue(
              at("slabs", i, `${party}_rate`),
              "Enter a percentage from 0 to 100, with up to 4 decimals.",
            );
          levy(["slabs", i, `${party}_cap`], slab[`${party}_cap`], true);
        }
        complete = band(["slabs", i], slab.from, slab.to) && complete;
      }
      const problem = complete
        ? statutoryBandProblem(
            lwf.slabs.map((s) => ({
              from: parseRupees(s.from) as number,
              to: optionalRupees(s.to),
            })),
          )
        : undefined;
      if (problem) issue(at("slabs"), `The table ${problem}.`);
      return;
    }
    const params = value.kind === "pf" ? value.pf : value.esi;
    pick("wage_basis", params.wage_basis, STATUTORY_WAGE_BASES);
    pick("rounding_mode", params.rounding_mode, SALARY_ROUNDING_MODES);
    pick("rounding_unit", params.rounding_unit, SALARY_ROUNDING_UNITS);
    if (value.kind === "pf") {
      const pf = value.pf;
      pick("contribution_basis", pf.contribution_basis, STATUTORY_CONTRIBUTION_BASES);
      const ceiling = money("wage_ceiling", pf.wage_ceiling, 1);
      rate("employee_rate", pf.employee_rate);
      const employer = rate("employer_rate", pf.employer_rate);
      const eps = rate("eps_rate", pf.eps_rate);
      const epsCeiling = money("eps_wage_ceiling", pf.eps_wage_ceiling);
      rate("edli_rate", pf.edli_rate);
      money("edli_wage_ceiling", pf.edli_wage_ceiling);
      rate("admin_rate", pf.admin_rate);
      money("admin_minimum", pf.admin_minimum);
      if (eps !== undefined && employer !== undefined && eps > employer)
        issue(
          at("eps_rate"),
          "The pension share comes out of the employer share, so it cannot exceed it.",
        );
      if (epsCeiling !== undefined && ceiling !== undefined && epsCeiling > ceiling)
        issue(at("eps_wage_ceiling"), "Keep the pension ceiling at or below the wage ceiling.");
    } else {
      const esi = value.esi;
      const ceiling = money("wage_ceiling", esi.wage_ceiling, 1);
      const disability = money("disability_wage_ceiling", esi.disability_wage_ceiling, 1);
      rate("employee_rate", esi.employee_rate);
      rate("employer_rate", esi.employer_rate);
      if (esi.employee_exempt_daily.trim() !== "") {
        const exempt = parseRupees(esi.employee_exempt_daily);
        if (exempt === undefined || exempt < 1 || exempt > 10_000_000)
          issue(
            at("employee_exempt_daily"),
            "Enter a daily wage above 0 and at most ₹1 lakh, or leave it blank.",
          );
      }
      if (esi.period_start_months.length === 0)
        issue(at("period_start_months"), "Choose the months that start a contribution period.");
      if (ceiling !== undefined && disability !== undefined && disability < ceiling)
        issue(
          at("disability_wage_ceiling"),
          "Keep the disability ceiling at or above the standard one.",
        );
    }
  });
export type StatutoryRuleForm = z.infer<typeof statutoryRuleFormSchema>;

const blankPf: StatutoryRuleForm["pf"] = {
  wage_basis: "",
  contribution_basis: "",
  wage_ceiling: "",
  employee_rate: "",
  employer_rate: "",
  eps_rate: "",
  eps_wage_ceiling: "",
  edli_rate: "",
  edli_wage_ceiling: "",
  admin_rate: "",
  admin_minimum: "",
  rounding_mode: "",
  rounding_unit: "",
};
const blankEsi: StatutoryRuleForm["esi"] = {
  wage_basis: "",
  wage_ceiling: "",
  disability_wage_ceiling: "",
  employee_rate: "",
  employer_rate: "",
  employee_exempt_daily: "",
  period_start_months: [],
  rounding_mode: "",
  rounding_unit: "",
};

export const blankPtSlab = (): StatutoryPtSlabForm => ({
  months: [],
  genders: [],
  from: "",
  to: "",
  amount: "",
});
export const blankLwfSlab = (): StatutoryLwfSlabForm => ({
  from: "",
  to: "",
  employee_fixed: "",
  employee_rate: "",
  employee_cap: "",
  employer_fixed: "",
  employer_rate: "",
  employer_cap: "",
});
export const blankTaxSlab = (): IncomeTaxSlabForm => ({ from: "", to: "", rate: "" });
export const blankIncomeTaxBand = (): IncomeTaxBandForm => ({
  min_age: "",
  slabs: [blankTaxSlab()],
});
export const blankSurcharge = (): IncomeTaxSurchargeForm => ({ above: "", rate: "" });
const blankIncomeTax = (): IncomeTaxForm => ({
  tax_year: "",
  standard_deduction: "",
  taxable_rounding_mode: "",
  taxable_rounding_unit: "",
  tax_rounding_mode: "",
  tax_rounding_unit: "",
  age_bands: [blankIncomeTaxBand()],
  rebate: "",
  rebate_income_limit: "",
  rebate_max: "",
  surcharges: [],
  cess_rate: "",
});
const blankPt = (): StatutoryRuleForm["pt"] => ({
  state_code: "",
  wage_basis: "",
  slabs: [blankPtSlab()],
});
const blankLwf = (): StatutoryRuleForm["lwf"] => ({
  state_code: "",
  wage_basis: "",
  deduction_months: [],
  rounding_mode: "",
  rounding_unit: "",
  slabs: [blankLwfSlab()],
});

export const newStatutoryRuleForm = (
  kind: StatutoryRuleForm["kind"],
  today: string,
): StatutoryRuleForm => ({
  expected_version: 0,
  kind,
  name: "",
  coverage: "",
  effective_on: today,
  citation: "",
  reference: "",
  pf: blankPf,
  esi: blankEsi,
  pt: blankPt(),
  lwf: blankLwf(),
  income_tax: blankIncomeTax(),
});

const incomeTaxToForm = (p: IncomeTaxParams): IncomeTaxForm => ({
  tax_year: p.tax_year,
  standard_deduction: paiseToRupees(p.standard_deduction_paise),
  taxable_rounding_mode: p.taxable_rounding_mode,
  taxable_rounding_unit: String(p.taxable_rounding_unit_paise),
  tax_rounding_mode: p.tax_rounding_mode,
  tax_rounding_unit: String(p.tax_rounding_unit_paise),
  age_bands: p.age_bands.map((b) => ({
    min_age: String(b.min_age),
    slabs: b.slabs.map((s) => ({
      from: paiseToRupees(s.from_paise),
      to: s.to_paise === undefined ? "" : paiseToRupees(s.to_paise),
      rate: ppmToPercent(s.rate_ppm),
    })),
  })),
  rebate: !p.rebate ? "none" : p.rebate.marginal_relief ? "relief" : "cliff",
  rebate_income_limit: p.rebate ? paiseToRupees(p.rebate.income_limit_paise) : "",
  rebate_max: p.rebate ? paiseToRupees(p.rebate.max_paise) : "",
  surcharges: p.surcharge_bands.map((s) => ({
    above: paiseToRupees(s.above_paise),
    rate: ppmToPercent(s.rate_ppm),
  })),
  cess_rate: ppmToPercent(p.cess_rate_ppm),
});

const toIncomeTaxParams = (form: IncomeTaxForm): IncomeTaxParams => {
  const income = (raw: string) => parseIncome(raw) ?? 0;
  const rate = (raw: string) => parseRate(raw) ?? 0;
  return {
    tax_year: form.tax_year.trim(),
    standard_deduction_paise: parseRupees(form.standard_deduction) ?? 0,
    taxable_rounding_mode: form.taxable_rounding_mode as Mode,
    taxable_rounding_unit_paise: Number(form.taxable_rounding_unit),
    tax_rounding_mode: form.tax_rounding_mode as Mode,
    tax_rounding_unit_paise: Number(form.tax_rounding_unit),
    age_bands: form.age_bands.map((b) => ({
      min_age: Number(b.min_age),
      slabs: b.slabs.map((s) => {
        const to = optionalIncome(s.to);
        return {
          from_paise: income(s.from),
          ...(to !== undefined && { to_paise: to }),
          rate_ppm: rate(s.rate),
        };
      }),
    })),
    ...(form.rebate !== "none" && {
      rebate: {
        income_limit_paise: income(form.rebate_income_limit),
        max_paise: income(form.rebate_max),
        marginal_relief: form.rebate === "relief",
      },
    }),
    surcharge_bands: form.surcharges.map((s) => ({
      above_paise: income(s.above),
      rate_ppm: rate(s.rate),
    })),
    cess_rate_ppm: rate(form.cess_rate),
  };
};

// A revision opens as the base of the next proposal; the date and citation stay so a reviewer sees what changed.
export const statutoryRevisionToForm = (
  revision: StatutoryRevision,
  version: number,
): StatutoryRuleForm => ({
  expected_version: version,
  kind: revision.kind,
  name: revision.name,
  coverage: revision.covered ? "covered" : "not_covered",
  effective_on: revision.effective_on,
  citation: revision.citation,
  reference: revision.reference,
  pf: revision.pf
    ? {
        wage_basis: revision.pf.wage_basis,
        contribution_basis: revision.pf.contribution_basis,
        wage_ceiling: paiseToRupees(revision.pf.wage_ceiling_paise),
        employee_rate: ppmToPercent(revision.pf.employee_rate_ppm),
        employer_rate: ppmToPercent(revision.pf.employer_rate_ppm),
        eps_rate: ppmToPercent(revision.pf.eps_rate_ppm),
        eps_wage_ceiling: paiseToRupees(revision.pf.eps_wage_ceiling_paise),
        edli_rate: ppmToPercent(revision.pf.edli_rate_ppm),
        edli_wage_ceiling: paiseToRupees(revision.pf.edli_wage_ceiling_paise),
        admin_rate: ppmToPercent(revision.pf.admin_rate_ppm),
        admin_minimum: paiseToRupees(revision.pf.admin_minimum_paise),
        rounding_mode: revision.pf.rounding_mode,
        rounding_unit: String(revision.pf.rounding_unit_paise),
      }
    : blankPf,
  esi: revision.esi
    ? {
        wage_basis: revision.esi.wage_basis,
        wage_ceiling: paiseToRupees(revision.esi.wage_ceiling_paise),
        disability_wage_ceiling: paiseToRupees(revision.esi.disability_wage_ceiling_paise),
        employee_rate: ppmToPercent(revision.esi.employee_rate_ppm),
        employer_rate: ppmToPercent(revision.esi.employer_rate_ppm),
        employee_exempt_daily:
          revision.esi.employee_exempt_daily_paise === undefined
            ? ""
            : paiseToRupees(revision.esi.employee_exempt_daily_paise),
        period_start_months: revision.esi.period_start_months,
        rounding_mode: revision.esi.rounding_mode,
        rounding_unit: String(revision.esi.rounding_unit_paise),
      }
    : blankEsi,
  pt: revision.pt
    ? {
        state_code: revision.pt.state_code,
        wage_basis: revision.pt.wage_basis,
        slabs: revision.pt.slabs.map((s) => ({
          months: s.months,
          genders: s.genders,
          from: paiseToRupees(s.from_paise),
          to: s.to_paise === undefined ? "" : paiseToRupees(s.to_paise),
          amount: paiseToRupees(s.amount_paise),
        })),
      }
    : blankPt(),
  lwf: revision.lwf
    ? {
        state_code: revision.lwf.state_code,
        wage_basis: revision.lwf.wage_basis,
        deduction_months: revision.lwf.deduction_months,
        rounding_mode: revision.lwf.rounding_mode,
        rounding_unit: String(revision.lwf.rounding_unit_paise),
        slabs: revision.lwf.slabs.map((s) => ({
          from: paiseToRupees(s.from_paise),
          to: s.to_paise === undefined ? "" : paiseToRupees(s.to_paise),
          employee_fixed: paiseToRupees(s.employee_fixed_paise),
          employee_rate: ppmToPercent(s.employee_rate_ppm),
          employee_cap:
            s.employee_cap_paise === undefined ? "" : paiseToRupees(s.employee_cap_paise),
          employer_fixed: paiseToRupees(s.employer_fixed_paise),
          employer_rate: ppmToPercent(s.employer_rate_ppm),
          employer_cap:
            s.employer_cap_paise === undefined ? "" : paiseToRupees(s.employer_cap_paise),
        })),
      }
    : blankLwf(),
  income_tax: revision.income_tax ? incomeTaxToForm(revision.income_tax) : blankIncomeTax(),
});

type Mode = StatutoryPfParams["rounding_mode"];

// Only the kind's own parameters are sent, and only when covered, matching the server's coverage rule.
export const toStatutoryRuleInput = (form: StatutoryRuleForm): StatutoryRuleInput => {
  const covered = form.coverage === "covered";
  const money = (raw: string) => parseRupees(raw) ?? 0;
  const rate = (raw: string) => parseRate(raw) ?? 0;
  const { pf, esi, pt, lwf } = form;
  const exempt = esi.employee_exempt_daily.trim();
  const upper = (raw: string) => {
    const paise = optionalRupees(raw);
    return paise === undefined ? {} : { to_paise: paise };
  };
  const cap = (raw: string) => optionalRupees(raw);
  const sortedMonths = (months: number[]) => [...months].sort((a, b) => a - b);
  return {
    expected_version: form.expected_version,
    kind: form.kind,
    name: form.name,
    covered,
    effective_on: form.effective_on,
    citation: form.citation,
    reference: form.reference,
    ...(covered &&
      form.kind === "pf" && {
        pf: {
          wage_basis: pf.wage_basis as keyof typeof STATUTORY_WAGE_BASES,
          contribution_basis: pf.contribution_basis as keyof typeof STATUTORY_CONTRIBUTION_BASES,
          wage_ceiling_paise: money(pf.wage_ceiling),
          employee_rate_ppm: rate(pf.employee_rate),
          employer_rate_ppm: rate(pf.employer_rate),
          eps_rate_ppm: rate(pf.eps_rate),
          eps_wage_ceiling_paise: money(pf.eps_wage_ceiling),
          edli_rate_ppm: rate(pf.edli_rate),
          edli_wage_ceiling_paise: money(pf.edli_wage_ceiling),
          admin_rate_ppm: rate(pf.admin_rate),
          admin_minimum_paise: money(pf.admin_minimum),
          rounding_mode: pf.rounding_mode as Mode,
          rounding_unit_paise: Number(pf.rounding_unit),
        },
      }),
    ...(covered &&
      form.kind === "esi" && {
        esi: {
          wage_basis: esi.wage_basis as keyof typeof STATUTORY_WAGE_BASES,
          wage_ceiling_paise: money(esi.wage_ceiling),
          disability_wage_ceiling_paise: money(esi.disability_wage_ceiling),
          employee_rate_ppm: rate(esi.employee_rate),
          employer_rate_ppm: rate(esi.employer_rate),
          ...(exempt !== "" && { employee_exempt_daily_paise: money(exempt) }),
          period_start_months: sortedMonths(esi.period_start_months),
          rounding_mode: esi.rounding_mode as Mode,
          rounding_unit_paise: Number(esi.rounding_unit),
        },
      }),
    ...(covered &&
      form.kind === "pt" && {
        pt: {
          state_code: pt.state_code.trim().toUpperCase(),
          wage_basis: pt.wage_basis as keyof typeof STATUTORY_WAGE_BASES,
          slabs: pt.slabs.map((s): StatutoryPtSlab => ({
            months: sortedMonths(s.months),
            genders: GENDERS.filter((g) => s.genders.includes(g)),
            from_paise: money(s.from),
            ...upper(s.to),
            amount_paise: money(s.amount),
          })),
        },
      }),
    ...(covered &&
      form.kind === "lwf" && {
        lwf: {
          state_code: lwf.state_code.trim().toUpperCase(),
          wage_basis: lwf.wage_basis as keyof typeof STATUTORY_WAGE_BASES,
          deduction_months: sortedMonths(lwf.deduction_months),
          rounding_mode: lwf.rounding_mode as Mode,
          rounding_unit_paise: Number(lwf.rounding_unit),
          slabs: lwf.slabs.map((s): StatutoryLwfSlab => {
            const employeeCap = cap(s.employee_cap);
            const employerCap = cap(s.employer_cap);
            return {
              from_paise: money(s.from),
              ...upper(s.to),
              employee_fixed_paise: money(s.employee_fixed),
              employee_rate_ppm: rate(s.employee_rate),
              ...(employeeCap !== undefined && { employee_cap_paise: employeeCap }),
              employer_fixed_paise: money(s.employer_fixed),
              employer_rate_ppm: rate(s.employer_rate),
              ...(employerCap !== undefined && { employer_cap_paise: employerCap }),
            };
          }),
        },
      }),
    ...(covered &&
      (form.kind === "income_tax_new" || form.kind === "income_tax_old") && {
        income_tax: toIncomeTaxParams(form.income_tax),
      }),
  };
};

export const statutoryDecisionFormSchema = z
  .object({ decision: z.enum(["approve", "reject"]), note: z.string() })
  .strict()
  .superRefine((value, ctx) => {
    const note = value.note.trim();
    if (value.decision === "reject" && note === "")
      ctx.addIssue({ code: "custom", path: ["note"], message: "Give a reason for rejecting." });
    if (note.length > 500 || note.includes("\0"))
      ctx.addIssue({
        code: "custom",
        path: ["note"],
        message: "Keep the note within 500 characters.",
      });
  });
export type StatutoryDecisionForm = z.infer<typeof statutoryDecisionFormSchema>;

export const statutoryPreviewFormSchema = z
  .object({
    structure_id: z.string().uuid("Choose a structure."),
    on: date,
    payable_days: z.string(),
    disability: z.boolean(),
    gender: z.enum(["", "female", "male", "other"]),
  })
  .strict()
  .superRefine((value, ctx) => {
    const result = salaryPreviewFormSchema.safeParse({
      on: value.on,
      payable_days: value.payable_days,
    });
    for (const i of result.error?.issues ?? [])
      if (i.path[0] === "payable_days") ctx.addIssue({ ...i, code: "custom" });
  });
export type StatutoryPreviewForm = z.infer<typeof statutoryPreviewFormSchema>;

export const toStatutoryPreviewInput = (form: StatutoryPreviewForm): StatutoryPreviewInput => ({
  structure_id: form.structure_id,
  on: form.on,
  payable_half_days: parseHalfDays(form.payable_days) ?? 0,
  disability: form.disability,
  ...(form.gender !== "" && { gender: form.gender }),
});

export const incomeTaxPreviewFormSchema = z
  .object({ on: date, annual_income: z.string(), age: z.string() })
  .strict()
  .superRefine((value, ctx) => {
    if (parseIncome(value.annual_income) === undefined)
      ctx.addIssue({
        code: "custom",
        path: ["annual_income"],
        message: "Enter the annual salary in rupees, with up to 2 decimals.",
      });
    const age = parseAge(value.age);
    if (value.age.trim() !== "" && (age === undefined || age > 120))
      ctx.addIssue({
        code: "custom",
        path: ["age"],
        message: "Enter an age from 0 to 120, or leave it blank.",
      });
  });
export type IncomeTaxPreviewForm = z.infer<typeof incomeTaxPreviewFormSchema>;

export const toIncomeTaxPreviewInput = (form: IncomeTaxPreviewForm): IncomeTaxPreviewInput => ({
  on: form.on,
  annual_income_paise: parseIncome(form.annual_income) ?? 0,
  ...(form.age.trim() !== "" && { age: Number(form.age) }),
});

export const incomeTaxPreviewPath = (base: string) => `${base}/hr/payroll/income-tax-preview`;
export const statutoryRulesPath = (base: string) => `${base}/hr/payroll/statutory-rules`;
export const statutoryPreviewPath = (base: string) => `${base}/hr/payroll/statutory-preview`;

// Summarises a revision's parameters for cards and history.
export const describeStatutoryRevision = (revision: StatutoryRevision) => {
  if (!revision.covered) return "Not covered";
  if (revision.pf)
    return `Employee ${ppmToPercent(revision.pf.employee_rate_ppm)}% · employer ${ppmToPercent(revision.pf.employer_rate_ppm)}% (pension ${ppmToPercent(revision.pf.eps_rate_ppm)}%) · ceiling ${formatPaise(revision.pf.wage_ceiling_paise)}`;
  if (revision.esi)
    return `Employee ${ppmToPercent(revision.esi.employee_rate_ppm)}% · employer ${ppmToPercent(revision.esi.employer_rate_ppm)}% · ceiling ${formatPaise(revision.esi.wage_ceiling_paise)} · periods from ${revision.esi.period_start_months.map((m) => MONTH_NAMES[m - 1]).join(", ")}`;
  const slabs = (n: number) => `${n} slab${n === 1 ? "" : "s"}`;
  if (revision.pt) {
    const months = new Set(revision.pt.slabs.flatMap((s) => s.months));
    return `${revision.pt.state_code} · ${slabs(revision.pt.slabs.length)} · due ${
      months.size === 12
        ? "every month"
        : `in ${[...months]
            .sort((a, b) => a - b)
            .map((m) => MONTH_NAMES[m - 1])
            .join(", ")}`
    }`;
  }
  if (revision.lwf)
    return `${revision.lwf.state_code} · ${slabs(revision.lwf.slabs.length)} · deducted in ${revision.lwf.deduction_months.map((m) => MONTH_NAMES[m - 1]).join(", ")}`;
  if (revision.income_tax) {
    const it = revision.income_tax;
    const bands = it.age_bands.length;
    return `Tax year ${it.tax_year} · ${bands === 1 ? "one slab table" : `${bands} age bands`} · ${
      it.rebate ? `rebate to ${formatPaise(it.rebate.income_limit_paise)}` : "no rebate"
    } · cess ${ppmToPercent(it.cess_rate_ppm)}%`;
  }
  return "Parameters unavailable";
};

export const TAX_REGIMES = {
  new: "New regime",
  old: "Old regime",
} as const;

export const staffPayPath = (base: string, staff: string) =>
  `${base}/hr/payroll/staff/${encodeURIComponent(staff)}`;
export const salaryAssignmentsPath = (base: string) => `${base}/hr/payroll/salary-assignments`;

const assignmentAmountFormSchema = z.object({ head_id: z.string(), amount: z.string() }).strict();
export type SalaryAssignmentAmountForm = z.infer<typeof assignmentAmountFormSchema>;

// A blank amount keeps the structure's own figure for that head.
export const salaryAssignmentFormSchema = z
  .object({
    structure_id: z.string().uuid("Choose a structure."),
    effective_on: date,
    reason: text("a reason", 500),
    amounts: z.array(assignmentAmountFormSchema).max(60),
  })
  .strict()
  .superRefine((value, ctx) => {
    value.amounts.forEach((a, i) => {
      if (a.amount.trim() !== "" && parseRupees(a.amount) === undefined)
        ctx.addIssue({
          code: "custom",
          path: ["amounts", i, "amount"],
          message: "Enter rupees with up to two decimals, or leave blank.",
        });
    });
  });
export type SalaryAssignmentForm = z.infer<typeof salaryAssignmentFormSchema>;

// Lists every fixed line of the structure, prefilled from the revision's own amounts where it set one.
export const salaryAssignmentAmountsFor = (
  lines: { head_id: string; basis: string }[],
  revision?: Pick<SalaryAssignmentRevision, "amounts">,
): SalaryAssignmentAmountForm[] => {
  const set = new Map(revision?.amounts.map((a) => [a.head_id, a.amount_paise]));
  return lines
    .filter((l) => l.basis === "fixed")
    .map((l) => {
      const paise = set.get(l.head_id);
      return { head_id: l.head_id, amount: paise === undefined ? "" : paiseToRupees(paise) };
    });
};

export const toSalaryAssignmentInput = (
  form: SalaryAssignmentForm,
  expectedVersion: number,
): SalaryAssignmentInput => ({
  expected_version: expectedVersion,
  structure_id: form.structure_id,
  effective_on: form.effective_on,
  reason: form.reason.trim(),
  amounts: form.amounts.flatMap((a) => {
    const paise = a.amount.trim() === "" ? undefined : parseRupees(a.amount);
    return paise === undefined ? [] : [{ head_id: a.head_id, amount_paise: paise }];
  }),
});

export const staffSalaryPreviewFormSchema = z
  .object({
    on: date,
    payable_days: z.string(),
    disability: z.boolean(),
    gender: z.enum(["", "female", "male", "other"]),
  })
  .strict()
  .superRefine((value, ctx) => {
    const result = salaryPreviewFormSchema.safeParse({
      on: value.on,
      payable_days: value.payable_days,
    });
    for (const i of result.error?.issues ?? [])
      if (i.path[0] === "payable_days") ctx.addIssue({ ...i, code: "custom" });
  });
export type StaffSalaryPreviewForm = z.infer<typeof staffSalaryPreviewFormSchema>;

export const toStaffSalaryPreviewInput = (
  form: StaffSalaryPreviewForm,
): StaffSalaryPreviewInput => ({
  on: form.on,
  payable_half_days: parseHalfDays(form.payable_days) ?? 0,
  disability: form.disability,
  ...(form.gender !== "" && { gender: form.gender }),
});

const validTaxYear = (year: string) => {
  const match = /^(\d{4})-(\d{2})$/.exec(year);
  if (!match) return false;
  const start = Number(match[1]);
  return start >= 2000 && start <= 2199 && Number(match[2]) === (start + 1) % 100;
};

// Mirrors the server: an intimation may precede its year but never follow 31 March, and a change needs a note.
export const taxRegimeSchemaFor = (current?: TaxRegimeElection) =>
  z
    .object({
      tax_year: z.string().trim(),
      regime: z.enum(["new", "old"], { message: "Choose the regime the employee intimated." }),
      intimated_on: date,
      note: z.string(),
    })
    .strict()
    .superRefine((value, ctx) => {
      if (!validTaxYear(value.tax_year)) {
        ctx.addIssue({
          code: "custom",
          path: ["tax_year"],
          message: "Write the tax year as 2026-27.",
        });
        return;
      }
      if (
        z.string().date().safeParse(value.intimated_on).success &&
        value.intimated_on > `${Number(value.tax_year.slice(0, 4)) + 1}-03-31`
      )
        ctx.addIssue({
          code: "custom",
          path: ["intimated_on"],
          message: "Use a date no later than 31 March of that tax year.",
        });
      const note = value.note.trim();
      if (note.length > 500 || note.includes("\0"))
        ctx.addIssue({
          code: "custom",
          path: ["note"],
          message: "Keep the note within 500 characters.",
        });
      else if (
        current &&
        note === "" &&
        (current.regime !== value.regime || current.intimated_on !== value.intimated_on)
      )
        ctx.addIssue({
          code: "custom",
          path: ["note"],
          message: "Give a reason for changing the recorded regime.",
        });
    });
export type TaxRegimeForm = z.infer<ReturnType<typeof taxRegimeSchemaFor>>;

export const toTaxRegimeInput = (form: TaxRegimeForm, expectedVersion: number): TaxRegimeInput => ({
  expected_version: expectedVersion,
  regime: form.regime,
  intimated_on: form.intimated_on,
  note: form.note.trim(),
});

export const staffIdentifiersPath = (base: string, staff: string) =>
  `${staffPayPath(base, staff)}/identifiers`;

export const IDENTIFIER_ACTIONS = {
  keep: "Keep as it is",
  set: "Replace",
  clear: "Remove",
} as const;
export const IDENTIFIER_FIELDS = {
  bank_account: "Bank account",
  pan: "PAN",
  uan: "UAN",
} as const;
export type IdentifierField = keyof typeof IDENTIFIER_FIELDS;

const identifierAction = z.enum(["keep", "set", "clear"]);
const compact = (value: string) => value.replace(/\s+/g, "");

// Mirrors the API's shapes so a mistyped value is caught inline; the API checks again.
export const staffIdentifiersFormSchema = z
  .object({
    reason: text("a reason", 500),
    bank: identifierAction,
    account_number: z.string(),
    ifsc: z.string(),
    holder_name: z.string(),
    pan: identifierAction,
    pan_value: z.string(),
    uan: identifierAction,
    uan_value: z.string(),
  })
  .strict()
  .superRefine((value, ctx) => {
    const issue = (path: string, message: string) =>
      ctx.addIssue({ code: "custom", path: [path], message });
    if (value.bank === "set") {
      if (!/^\d{9,18}$/.test(compact(value.account_number)))
        issue("account_number", "Enter the 9 to 18 digit account number.");
      if (!/^[A-Z]{4}0[A-Z0-9]{6}$/.test(value.ifsc.trim().toUpperCase()))
        issue("ifsc", "Enter the 11-character IFSC, such as ABCD0123456.");
      const holder = value.holder_name.trim();
      if (holder === "" || holder.length > 150 || holder.includes("\0"))
        issue("holder_name", "Enter the account holder's name, up to 150 characters.");
    }
    if (
      value.pan === "set" &&
      !/^[A-Z]{3}P[A-Z]\d{4}[A-Z]$/.test(value.pan_value.trim().toUpperCase())
    )
      issue("pan_value", "Enter the individual's 10-character PAN; its fourth letter is P.");
    if (value.uan === "set" && !/^\d{12}$/.test(compact(value.uan_value)))
      issue("uan_value", "Enter the 12-digit UAN.");
    if (value.bank === "keep" && value.pan === "keep" && value.uan === "keep")
      issue("bank", "Replace or remove at least one identifier.");
  });
export type StaffIdentifiersForm = z.infer<typeof staffIdentifiersFormSchema>;

export const toStaffIdentifiersInput = (
  form: StaffIdentifiersForm,
  expectedVersion: number,
): StaffIdentifiersInput => ({
  expected_version: expectedVersion,
  reason: form.reason.trim(),
  bank:
    form.bank === "set"
      ? {
          action: "set",
          account_number: compact(form.account_number),
          ifsc: form.ifsc.trim().toUpperCase(),
          holder_name: form.holder_name.trim(),
        }
      : { action: form.bank },
  pan:
    form.pan === "set"
      ? { action: "set", value: form.pan_value.trim().toUpperCase() }
      : { action: form.pan },
  uan:
    form.uan === "set" ? { action: "set", value: compact(form.uan_value) } : { action: form.uan },
});

export const staffIdentifierRevealFormSchema = z
  .object({ reason: text("why you need to see it", 200) })
  .strict();
export type StaffIdentifierRevealForm = z.infer<typeof staffIdentifierRevealFormSchema>;
