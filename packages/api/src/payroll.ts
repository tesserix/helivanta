import { z } from "zod";
import type {
  SalaryHead,
  SalaryHeadInput,
  SalaryPreviewInput,
  SalaryStructure,
  SalaryStructureInput,
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
