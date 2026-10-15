import { expect, it } from "vitest";
import {
  addDays,
  rosterChangeSchema,
  rosterDates,
  rosterSchema,
  rosterStatusSchema,
  weekdayOf,
} from "./roster-config";

const roster = {
  name: "October",
  start_date: "2026-10-12",
  end_date: "2026-10-25",
  reviewed: true,
};
it("accepts one to six whole weeks", () => {
  expect(rosterSchema.safeParse(roster).success).toBe(true);
  expect(rosterSchema.safeParse({ ...roster, end_date: "2026-11-22" }).success).toBe(true);
});
it.each([
  { end_date: "2026-10-24" },
  { end_date: "2026-11-29" },
  { end_date: "2026-10-11" },
  { start_date: "2026-02-30" },
  { name: " " },
  { reviewed: false },
  { status: "frozen" },
])("rejects invalid rosters %j", (invalid) => {
  expect(rosterSchema.safeParse({ ...roster, ...invalid }).success).toBe(false);
});

const allocation = (staff: string, area: string) => ({
  id: crypto.randomUUID(),
  staff_id: staff,
  roster_date: "2026-10-12",
  shift_revision_id: "33333333-3333-4333-8333-333333333333",
  area_revision_id: area,
});
const a = "11111111-1111-4111-8111-111111111111";
const b = "22222222-2222-4222-8222-222222222222";
const change = {
  change_id: "44444444-4444-4444-8444-444444444444",
  expected_version: 1,
  mode: "roster" as const,
  add: [allocation(a, a)],
  remove: [] as string[],
  reason: "",
  reviewed: true,
};
it("mirrors the API's per-mode rules", () => {
  expect(rosterChangeSchema.safeParse(change).success).toBe(true);
  expect(
    rosterChangeSchema.safeParse({ ...change, add: [allocation(a, a), allocation(a, b)] }).success,
  ).toBe(false);
  expect(
    rosterChangeSchema.safeParse({ ...change, add: [allocation(a, a), allocation(b, a)] }).success,
  ).toBe(true);
  expect(
    rosterChangeSchema.safeParse({
      ...change,
      mode: "employee",
      add: [allocation(a, a), allocation(b, a)],
    }).success,
  ).toBe(false);
  expect(rosterChangeSchema.safeParse({ ...change, mode: "additional", remove: [b] }).success).toBe(
    false,
  );
  expect(rosterChangeSchema.safeParse({ ...change, add: [] }).success).toBe(false);
  expect(rosterChangeSchema.safeParse({ ...change, reviewed: false }).success).toBe(false);
  expect(rosterChangeSchema.safeParse({ ...change, expected_version: 0 }).success).toBe(false);
});
it("requires a reason only where history needs one", () => {
  const status = { change_id: change.change_id, expected_version: 2, reason: "", reviewed: true };
  expect(rosterStatusSchema(false).safeParse(status).success).toBe(true);
  expect(rosterStatusSchema(true).safeParse(status).success).toBe(false);
  expect(rosterStatusSchema(true).safeParse({ ...status, reason: "Correct cover" }).success).toBe(
    true,
  );
  expect(rosterStatusSchema(false).safeParse({ ...status, reviewed: false }).success).toBe(false);
});
it("walks calendar dates independent of the browser timezone", () => {
  expect(weekdayOf("2026-10-12")).toBe(0);
  expect(weekdayOf("2026-10-18")).toBe(6);
  expect(addDays("2026-03-07", 2)).toBe("2026-03-09");
  expect(rosterDates("2026-10-12", "2026-10-18")).toHaveLength(7);
  expect(rosterDates("2026-10-12", "2027-10-18")).toHaveLength(42);
});
