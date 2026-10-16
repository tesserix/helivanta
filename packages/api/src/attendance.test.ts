import { expect, it } from "vitest";
import {
  ATTENDANCE_EXCEPTIONS,
  ATTENDANCE_STATUSES,
  attendanceDayContract,
  attendancePunchContract,
  attendancePunchFormSchema,
  attendancePunchInputContract,
  attendancePunchesPath,
  attendanceSettingsInputContract,
  attendanceSettingsSchema,
  attendanceSheetPath,
  attendanceTotalsContract,
  attendanceTotalsPath,
  attendanceVoidFormSchema,
  staffAttendancePath,
  toAttendancePunch,
  toAttendanceVoid,
  zonedClock,
  zonedInstant,
} from "./attendance";

const a = "11111111-1111-4111-8111-111111111111";
const settings = {
  expected_version: 0,
  early_arrival_minutes: 60,
  late_departure_minutes: 120,
  early_departure_grace_minutes: 10,
  short_duty_minutes: 30,
  duplicate_window_minutes: 5,
  backdate_days: 7,
  effective_on: "2026-11-01",
  reviewed: true,
};
const punch = {
  staff_id: a,
  direction: "in" as const,
  date: "2026-10-12",
  time: "08:05",
  reason: " Synthetic register entry ",
};
const issues = (
  schema: {
    safeParse: (v: unknown) => {
      error?: { issues: { path: (string | number)[]; message: string }[] };
    };
  },
  value: object,
) => schema.safeParse(value).error?.issues.map((i) => [i.path.join("."), i.message]);

it("accepts a reviewed policy and sends it as the contract body", () => {
  expect(attendanceSettingsInputContract.parse(attendanceSettingsSchema.parse(settings))).toEqual(
    settings,
  );
});

it("refuses an unreviewed policy and allowances outside their limits", () => {
  expect(issues(attendanceSettingsSchema, { ...settings, reviewed: false })).toEqual([
    ["reviewed", "Confirm that you reviewed the policy."],
  ]);
  expect(issues(attendanceSettingsSchema, { ...settings, duplicate_window_minutes: 0 })).toEqual([
    ["duplicate_window_minutes", "Use at least 1 for the duplicate window."],
  ]);
  expect(issues(attendanceSettingsSchema, { ...settings, backdate_days: 93 })).toEqual([
    ["backdate_days", "Keep the back-dating window within 92."],
  ]);
  expect(issues(attendanceSettingsSchema, { ...settings, late_departure_minutes: 1.5 })).toEqual([
    ["late_departure_minutes", "Enter the late departure allowance as a whole number."],
  ]);
});

it("resolves hospital wall-clock time to an instant", () => {
  expect(zonedInstant("2026-10-12", "08:05", "Asia/Kolkata")).toBe("2026-10-12T02:35:00Z");
  expect(zonedInstant("2026-10-12", "00:10", "Asia/Kolkata")).toBe("2026-10-11T18:40:00Z");
  expect(zonedInstant("2026-07-01", "09:00", "America/New_York")).toBe("2026-07-01T13:00:00Z");
  expect(zonedInstant("2026-12-01", "09:00", "America/New_York")).toBe("2026-12-01T14:00:00Z");
  expect(zonedInstant("2026-03-08", "02:30", "America/New_York")).toBeNull();
  expect(zonedInstant("2026-11-01", "01:30", "America/New_York")).toBe("2026-11-01T05:30:00Z");
  expect(zonedClock("2026-10-12T02:35:00Z", "Asia/Kolkata")).toBe("08:05");
});

it("builds the punch body from the form and the hospital timezone", () => {
  const form = attendancePunchFormSchema.parse(punch);
  const body = toAttendancePunch(form, "Asia/Kolkata");
  expect(body).toEqual({
    staff_id: a,
    direction: "in",
    punched_at: "2026-10-12T02:35:00Z",
    reason: "Synthetic register entry",
  });
  expect(attendancePunchInputContract.parse(body)).toEqual(body);
  expect(
    toAttendancePunch({ ...form, date: "2026-03-08", time: "02:30" }, "America/New_York"),
  ).toBeNull();
  expect(toAttendanceVoid(attendanceVoidFormSchema.parse({ reason: " Wrong person " }))).toEqual({
    reason: "Wrong person",
  });
});

it("refuses a punch form without a reason, a direction or a valid time", () => {
  expect(issues(attendancePunchFormSchema, { ...punch, reason: "  " })).toEqual([
    ["reason", "Say why this punch is entered by hand."],
  ]);
  expect(issues(attendancePunchFormSchema, { ...punch, time: "24:00" })).toEqual([
    ["time", "Use a 24-hour time such as 08:05."],
  ]);
  expect(issues(attendancePunchFormSchema, { ...punch, direction: "" })).toEqual([
    ["direction", "Choose in or out."],
  ]);
  expect(issues(attendancePunchFormSchema, { ...punch, staff_id: "" })).toEqual([
    ["staff_id", "Choose a staff member."],
  ]);
  expect(issues(attendanceVoidFormSchema, { reason: "" })).toEqual([
    ["reason", "Say why this punch is being voided."],
  ]);
});

it("builds encoded paths", () => {
  expect(attendanceSheetPath("/h", "2026-10-12")).toBe("/h/hr/attendance?date=2026-10-12");
  expect(attendanceTotalsPath("/h", "2026-10-12")).toBe("/h/hr/attendance-totals?date=2026-10-12");
  expect(attendancePunchesPath("/h", a, "2026-10-01", "2026-10-12")).toBe(
    `/h/hr/attendance/punches?staff_id=${a}&from=2026-10-01&to=2026-10-12`,
  );
  expect(staffAttendancePath("/h", "a/b", "2026-10-01", "2026-10-12")).toBe(
    "/h/staff/a%2Fb/attendance?from=2026-10-01&to=2026-10-12",
  );
});

it("parses server rows and labels every status and exception", () => {
  const shift = {
    allocation_id: a,
    roster_id: a,
    shift_revision_id: a,
    shift_code: "DAY",
    shift_name: "Day",
    status: "present",
    starts_at: "2026-10-12T02:30:00Z",
    ends_at: "2026-10-12T10:30:00Z",
    break_minutes: 30,
    grace_minutes: 0,
    late_minutes: 5,
    early_departure_minutes: 0,
    worked_minutes: 475,
    expected_minutes: 450,
    payable_minutes: 445,
    unpaid_half_days: 0,
    exceptions: ["late"],
    punch_ids: [a],
    ignored_punch_ids: [],
  };
  const day = {
    staff_id: a,
    employee_code: "SYN-1",
    display_name: "Synthetic",
    date: "2026-10-12",
    timezone: "Asia/Kolkata",
    leave: "none",
    short_leave_minutes: 0,
    approved_overtime_minutes: 0,
    payable_minutes: 445,
    shifts: [shift],
    stray_punch_ids: [],
    exceptions: ["late"],
  };
  expect(attendanceDayContract.parse(day).shifts[0]?.payable_minutes).toBe(445);
  expect(
    attendanceDayContract.safeParse({ ...day, shifts: [{ ...shift, status: "odd" }] }).success,
  ).toBe(false);
  expect(
    attendanceTotalsContract.parse({
      date: "2026-10-12",
      people: 1,
      shifts: 1,
      payable_minutes: 445,
      approved_overtime_minutes: 0,
      unpaid_half_days: 0,
      statuses: [{ code: "present", count: 1 }],
      exceptions: [{ code: "late", count: 1 }],
    }).people,
  ).toBe(1);
  expect(
    attendancePunchContract.safeParse({
      id: a,
      staff_id: a,
      employee_code: "SYN-1",
      display_name: "Synthetic",
      direction: "in",
      punched_at: "2026-10-12T02:35:00Z",
      local_date: "2026-10-12",
      timezone: "Asia/Kolkata",
      source: "manual",
      policy_revision_id: a,
      reason: "",
      recorded_by_subject: "user-1",
      created_at: "2026-10-12T02:36:00Z",
      voided: false,
    }).success,
  ).toBe(true);
  expect(Object.keys(ATTENDANCE_STATUSES)).toHaveLength(9);
  expect(Object.keys(ATTENDANCE_EXCEPTIONS)).toHaveLength(8);
});
