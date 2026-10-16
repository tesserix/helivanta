import { z } from "zod";
import type {
  SalaryHead,
  SalaryHeadInput,
  SalaryPreviewInput,
  SalaryStructure,
  SalaryStructureInput,
  StatutoryPfParams,
  StatutoryPreviewInput,
  StatutoryRevision,
  StatutoryRuleInput,
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
} as const;
export const STATUTORY_COMPONENTS = {
  pf_employee: "PF employee share",
  pf_employer_epf: "PF employer share (EPF)",
  pf_employer_eps: "Pension (EPS)",
  pf_edli: "EDLI",
  pf_admin: "PF admin charges",
  esi_employee: "ESI employee share",
  esi_employer: "ESI employer share",
} as const;

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

export const statutoryRuleFormSchema = z
  .object({
    expected_version: z.number().int().min(0),
    kind: z.enum(["pf", "esi"]),
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
  })
  .strict()
  .superRefine((value, ctx) => {
    const issue = (path: string[], message: string) =>
      ctx.addIssue({ code: "custom", path, message });
    if (!["covered", "not_covered"].includes(value.coverage))
      issue(["coverage"], "State whether the hospital is covered.");
    if (value.coverage !== "covered") return;
    const params = value.kind === "pf" ? value.pf : value.esi;
    const at = (field: string) => [value.kind, field];
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
});

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
});

type Mode = StatutoryPfParams["rounding_mode"];

// Only the kind's own parameters are sent, and only when covered, matching the server's coverage rule.
export const toStatutoryRuleInput = (form: StatutoryRuleForm): StatutoryRuleInput => {
  const covered = form.coverage === "covered";
  const money = (raw: string) => parseRupees(raw) ?? 0;
  const rate = (raw: string) => parseRate(raw) ?? 0;
  const { pf, esi } = form;
  const exempt = esi.employee_exempt_daily.trim();
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
          period_start_months: [...esi.period_start_months].sort((a, b) => a - b),
          rounding_mode: esi.rounding_mode as Mode,
          rounding_unit_paise: Number(esi.rounding_unit),
        },
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
});

export const statutoryRulesPath = (base: string) => `${base}/hr/payroll/statutory-rules`;
export const statutoryPreviewPath = (base: string) => `${base}/hr/payroll/statutory-preview`;

// Summarises a revision's parameters for cards and history.
export const describeStatutoryRevision = (revision: StatutoryRevision) => {
  if (!revision.covered) return "Not covered";
  if (revision.pf)
    return `Employee ${ppmToPercent(revision.pf.employee_rate_ppm)}% · employer ${ppmToPercent(revision.pf.employer_rate_ppm)}% (pension ${ppmToPercent(revision.pf.eps_rate_ppm)}%) · ceiling ${formatPaise(revision.pf.wage_ceiling_paise)}`;
  if (revision.esi)
    return `Employee ${ppmToPercent(revision.esi.employee_rate_ppm)}% · employer ${ppmToPercent(revision.esi.employer_rate_ppm)}% · ceiling ${formatPaise(revision.esi.wage_ceiling_paise)} · periods from ${revision.esi.period_start_months.map((m) => MONTH_NAMES[m - 1]).join(", ")}`;
  return "Parameters unavailable";
};
