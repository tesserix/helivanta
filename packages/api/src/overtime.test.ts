import { expect, it } from "vitest";
import {
  formatMinutes,
  noteRequiredFor,
  overtimeDecisionFormSchema,
  overtimeDecisionInputContract,
  overtimeMinutes,
  overtimePath,
  overtimeRequestContract,
  overtimeRequestFormSchema,
  overtimeRequestInputContract,
  overtimeSettingsInputContract,
  overtimeSettingsSchema,
  overtimeSummaryPath,
  toOvertimeDecision,
  toOvertimeInput,
} from "./overtime";
const a = "11111111-1111-4111-8111-111111111111";
const b = "22222222-2222-4222-8222-222222222222";
const settings = {
  expected_version: 0,
  daily_limit_minutes: 240,
  weekly_limit_minutes: 720,
  monthly_limit_minutes: 2400,
  filing_window_days: 7,
  effective_on: "2026-11-01",
  reviewed: true,
};
const request = {
  staff_id: a,
  work_date: "2026-11-02",
  start_time: "18:00",
  end_time: "20:30",
  reason: " Synthetic theatre overrun ",
};
const issues = (
  schema: {
    safeParse: (v: unknown) => {
      error?: { issues: { path: (string | number)[]; message: string }[] };
    };
  },
  value: object,
) => schema.safeParse(value).error?.issues.map((i) => [i.path.join("."), i.message]);

it("accepts nested limits and sends them as the contract body", () => {
  const parsed = overtimeSettingsSchema.parse(settings);
  expect(overtimeSettingsInputContract.parse(parsed)).toEqual(settings);
});

it("refuses limits that do not nest, an unreviewed policy and out-of-range values", () => {
  expect(issues(overtimeSettingsSchema, { ...settings, weekly_limit_minutes: 200 })).toEqual([
    ["weekly_limit_minutes", "Allow at least the daily limit each week."],
  ]);
  expect(issues(overtimeSettingsSchema, { ...settings, monthly_limit_minutes: 700 })).toEqual([
    ["monthly_limit_minutes", "Allow at least the weekly limit each month."],
  ]);
  expect(issues(overtimeSettingsSchema, { ...settings, reviewed: false })).toEqual([
    ["reviewed", "Confirm that you reviewed the policy."],
  ]);
  expect(issues(overtimeSettingsSchema, { ...settings, daily_limit_minutes: Number.NaN })).toEqual([
    ["daily_limit_minutes", "Enter the daily limit in minutes."],
  ]);
  expect(
    issues(overtimeSettingsSchema, {
      ...settings,
      daily_limit_minutes: 1441,
      weekly_limit_minutes: 2000,
    }),
  ).toEqual([["daily_limit_minutes", "Keep the daily limit within 1440 minutes."]]);
  expect(issues(overtimeSettingsSchema, { ...settings, filing_window_days: 93 })).toEqual([
    ["filing_window_days", "Keep the filing window within 92 days."],
  ]);
});

it("counts an end at or before the start as the next day", () => {
  expect(overtimeMinutes("18:00", "20:30")).toBe(150);
  expect(overtimeMinutes("22:00", "02:00")).toBe(240);
  expect(overtimeMinutes("08:00", "08:00")).toBe(1440);
  expect(overtimeMinutes("8:00", "09:00")).toBe(0);
});

it("turns the filing form into the contract body", () => {
  const input = toOvertimeInput(overtimeRequestFormSchema.parse(request));
  expect(input).toEqual({ ...request, reason: "Synthetic theatre overrun" });
  expect(overtimeRequestInputContract.parse(input)).toEqual(input);
});

it("refuses a request without a person, a short span, bad times or a blank reason", () => {
  expect(issues(overtimeRequestFormSchema, { ...request, staff_id: "" })).toEqual([
    ["staff_id", "Choose a staff member."],
  ]);
  expect(issues(overtimeRequestFormSchema, { ...request, end_time: "18:10" })).toEqual([
    ["end_time", "Record at least 15 minutes of overtime."],
  ]);
  expect(issues(overtimeRequestFormSchema, { ...request, start_time: "24:00" })).toEqual([
    ["start_time", "Use a 24-hour time such as 18:30."],
  ]);
  expect(issues(overtimeRequestFormSchema, { ...request, reason: "  " })).toEqual([
    ["reason", "Add a reason for the overtime."],
  ]);
  expect(issues(overtimeRequestFormSchema, { ...request, work_date: "" })).toEqual([
    ["work_date", "Choose the work date."],
  ]);
});

it("bounds approved minutes by the request and requires a note for partial approval", () => {
  const schema = overtimeDecisionFormSchema(150);
  expect(issues(schema, { approved_minutes: 151, note: "" })).toEqual([
    ["approved_minutes", "Approve no more than the 150 minutes requested."],
  ]);
  expect(issues(schema, { approved_minutes: 0, note: "" })).toEqual([
    ["approved_minutes", "Approve at least 1 minute, or reject the request."],
  ]);
  const full = schema.parse({ approved_minutes: 150, note: "" });
  const partial = schema.parse({ approved_minutes: 90, note: " Ward closed early " });
  expect(noteRequiredFor("approve", full, 150)).toBe(false);
  expect(noteRequiredFor("approve", partial, 150)).toBe(true);
  expect(noteRequiredFor("reject", full, 150)).toBe(true);
  expect(noteRequiredFor("revoke", full, 150)).toBe(true);
  expect(noteRequiredFor("withdraw", full, 150)).toBe(false);
  const approve = toOvertimeDecision(3, "approve", partial);
  expect(approve).toEqual({
    expected_version: 3,
    decision: "approve",
    approved_minutes: 90,
    note: "Ward closed early",
  });
  expect(overtimeDecisionInputContract.parse(approve)).toEqual(approve);
  expect(toOvertimeDecision(3, "reject", partial)).toEqual({
    expected_version: 3,
    decision: "reject",
    note: "Ward closed early",
  });
});

it("builds list and summary paths with only the filters that are set", () => {
  expect(overtimePath("/b", {})).toBe("/b/hr/overtime");
  expect(overtimePath("/b", { status: "all", staff_id: "" })).toBe("/b/hr/overtime");
  expect(overtimePath("/b", { status: "approved", staff_id: b, to: "2026-11-30" })).toBe(
    `/b/hr/overtime?status=approved&staff_id=${b}&to=2026-11-30`,
  );
  expect(overtimeSummaryPath("/b", "2026-11")).toBe("/b/hr/overtime-summary?month=2026-11");
});

it("formats minutes as hours and minutes", () => {
  expect(formatMinutes(45)).toBe("45 min");
  expect(formatMinutes(120)).toBe("2 h");
  expect(formatMinutes(150)).toBe("2 h 30 min");
});

it("parses a request with its reason withheld and refuses unknown fields", () => {
  const record = {
    id: a,
    staff_id: b,
    employee_code: "SYN-N1",
    display_name: "Synthetic One",
    work_date: "2026-11-02",
    start_time: "18:00",
    end_time: "20:30",
    timezone: "Asia/Kolkata",
    starts_at: "2026-11-02T12:30:00Z",
    ends_at: "2026-11-02T15:00:00Z",
    requested_minutes: 150,
    payable_minutes: 0,
    reason: "",
    status: "pending",
    requested_by_subject: "user-filer",
    version: 1,
    created_at: "2026-10-16T00:00:00Z",
    updated_at: "2026-10-16T00:00:00Z",
  };
  expect(overtimeRequestContract.safeParse(record).success).toBe(true);
  expect(overtimeRequestContract.safeParse({ ...record, extra: 1 }).success).toBe(false);
});
