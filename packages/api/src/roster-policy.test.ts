import { expect, it } from "vitest";
import { rosterSettingsSchema, staffRosterAssignmentSchema } from "./roster-config";
const settings = {
  expected_version: 0,
  timezone: "Asia/Kolkata",
  week_start: 0,
  auto_population: "fixed_shift",
  effective_on: "2026-10-15",
  reviewed: true,
};
it("accepts reviewed settings in a canonical IANA timezone", () => {
  expect(rosterSettingsSchema.safeParse(settings).success).toBe(true);
  expect(rosterSettingsSchema.safeParse({ ...settings, timezone: "UTC" }).success).toBe(true);
});
it.each([
  { timezone: "Mars/Olympus" },
  { timezone: "Local" },
  { timezone: "asia/kolkata" },
  { timezone: "posix/Asia/Kolkata" },
  { week_start: 7 },
  { auto_population: "everyone" },
  { effective_on: "2026-02-30" },
  { reviewed: false },
  { tenant_id: "injected" },
])("rejects invalid roster settings %j", (invalid) => {
  expect(rosterSettingsSchema.safeParse({ ...settings, ...invalid }).success).toBe(false);
});
it("requires a reviewed assignment that pins a real template revision", () => {
  const assignment = {
    expected_version: 0,
    weekly_template_revision_id: "11111111-1111-4111-8111-111111111111",
    fixed: true,
    active: true,
    effective_on: "2026-10-15",
    reviewed: true,
  };
  expect(staffRosterAssignmentSchema.safeParse(assignment).success).toBe(true);
  expect(staffRosterAssignmentSchema.safeParse({ ...assignment, reviewed: false }).success).toBe(
    false,
  );
  expect(
    staffRosterAssignmentSchema.safeParse({
      ...assignment,
      weekly_template_revision_id: "00000000-0000-0000-0000-000000000000",
    }).success,
  ).toBe(false);
});
