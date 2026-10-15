import { expect, it } from "vitest";
import {
  formatHalfDays,
  leaveConfigContract,
  leaveConfigSchema,
  leaveCorrectionSchema,
  leaveSettingsSchema,
  staffLeaveAssignmentSchema,
} from "./leave";
const id = "11111111-1111-4111-8111-111111111111";
const other = "22222222-2222-4222-8222-222222222222";
const rules = {
  paid: true,
  balance_tracked: true,
  half_day_allowed: true,
  short_leave_minutes: 120,
  clubbable: false,
};
const leaveType = {
  expected_version: 0,
  kind: "leave_type",
  code: "syn-casual",
  name: "Synthetic Casual",
  description: "",
  active: true,
  effective_on: "2026-01-01",
  reviewed: true,
  rules,
  holidays: [],
  entries: [],
};
const calendar = {
  ...leaveType,
  kind: "holiday_calendar",
  rules: undefined,
  calendar_year: 2027,
  effective_on: "2026-12-01",
  holidays: [{ holiday_date: "2027-01-26", name: "Synthetic Republic", category: "government" }],
};
const entry = {
  leave_type_revision_id: id,
  annual_half_days: 24,
  accrual: "monthly",
  carry_forward_half_days: 10,
  proration: "join_month",
  probation_eligible: false,
};
const structure = { ...leaveType, kind: "structure", rules: undefined, entries: [entry] };

it("accepts each kind and uppercases codes", () => {
  expect(leaveConfigSchema().parse(leaveType).code).toBe("SYN-CASUAL");
  expect(leaveConfigSchema().safeParse(calendar).success).toBe(true);
  expect(leaveConfigSchema().safeParse(structure).success).toBe(true);
});
it.each([
  { ...leaveType, reviewed: false },
  { ...leaveType, rules: undefined },
  { ...leaveType, rules: { ...rules, short_leave_minutes: 20 } },
  { ...leaveType, rules: { ...rules, short_leave_minutes: 495 } },
  { ...leaveType, calendar_year: 2026 },
  { ...leaveType, entries: [entry] },
  { ...leaveType, tenant_id: "injected" },
  { ...leaveType, name: "Synthetic\0Casual" },
  { ...leaveType, effective_on: "2026-02-30" },
  { ...calendar, calendar_year: undefined },
  { ...calendar, effective_on: "2027-01-02" },
  { ...calendar, holidays: [{ ...calendar.holidays[0], holiday_date: "2028-01-26" }] },
  { ...calendar, holidays: [calendar.holidays[0], calendar.holidays[0]] },
  { ...calendar, holidays: [{ ...calendar.holidays[0], category: "bank" }] },
  { ...calendar, rules },
  { ...structure, entries: [] },
  { ...structure, entries: [entry, entry] },
  { ...structure, entries: [{ ...entry, annual_half_days: 731 }] },
  { ...structure, entries: [{ ...entry, accrual: "weekly" }] },
  { ...structure, holidays: calendar.holidays },
])("rejects invalid configuration %#", (invalid) => {
  expect(leaveConfigSchema().safeParse(invalid).success).toBe(false);
});
it("rejects allowances on untracked leave types only", () => {
  const untracked = new Set([id]);
  expect(leaveConfigSchema(untracked).safeParse(structure).success).toBe(false);
  expect(
    leaveConfigSchema(untracked).safeParse({
      ...structure,
      entries: [
        { ...entry, annual_half_days: 0, carry_forward_half_days: 0 },
        { ...entry, leave_type_revision_id: other },
      ],
    }).success,
  ).toBe(true);
});
it("parses readable records with nested parts", () => {
  const record = {
    id,
    revision_id: other,
    kind: "structure",
    code: "SYN-NURSING",
    name: "Synthetic Nursing",
    description: "",
    active: true,
    effective_on: "2026-04-01",
    reviewed_by_subject: "synthetic-owner",
    version: 1,
    created_at: "2026-10-15T00:00:00Z",
    updated_at: "2026-10-15T00:00:00Z",
    holidays: [],
    entries: [
      {
        ...entry,
        leave_type_id: other,
        code: "SYN-EARNED",
        name: "Synthetic Earned",
        paid: true,
        balance_tracked: true,
      },
    ],
  };
  expect(leaveConfigContract.safeParse(record).success).toBe(true);
  expect(leaveConfigContract.safeParse({ ...record, reviewed: true }).success).toBe(false);
});
it("formats half-day allowances as days", () => {
  expect(formatHalfDays(1)).toBe("0.5 days");
  expect(formatHalfDays(2)).toBe("1 day");
  expect(formatHalfDays(24)).toBe("12 days");
  expect(formatHalfDays(-2)).toBe("-1 day");
});
it("requires review before settings or assignments are saved", () => {
  const unreviewed = leaveSettingsSchema.safeParse({ year_start_month: 4, reviewed: false });
  expect(unreviewed.error?.issues.map((i) => i.path.join("."))).toEqual(["reviewed"]);
  expect(leaveSettingsSchema.safeParse({ year_start_month: 13, reviewed: true }).success).toBe(
    false,
  );
  const assignment = {
    expected_version: 0,
    structure_revision_id: id,
    active: true,
    effective_on: "2026-04-01",
    reviewed: false,
  };
  expect(
    staffLeaveAssignmentSchema.safeParse(assignment).error?.issues.map((i) => i.path.join(".")),
  ).toEqual(["reviewed"]);
  expect(staffLeaveAssignmentSchema.safeParse({ ...assignment, reviewed: true }).success).toBe(
    true,
  );
});
it("rejects zero, oversized and unexplained corrections", () => {
  const correction = {
    leave_type_id: id,
    leave_year: 2026,
    half_days: -3,
    effective_on: "2026-05-01",
    reason: "Synthetic audit adjustment",
  };
  expect(leaveCorrectionSchema.safeParse(correction).success).toBe(true);
  const paths = (value: object) =>
    leaveCorrectionSchema
      .safeParse({ ...correction, ...value })
      .error?.issues.map((i) => i.path[0]);
  expect(paths({ half_days: 0 })).toEqual(["half_days"]);
  expect(paths({ half_days: 1461 })).toEqual(["half_days"]);
  expect(paths({ reason: "  too short  " })).toEqual(["reason"]);
});
