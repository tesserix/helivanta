import { expect, it } from "vitest";
import { rosterConfigContract, rosterConfigSchema } from "./roster-config";
const id = "11111111-1111-4111-8111-111111111111";
const base = {
  expected_version: 0,
  kind: "area",
  code: "syn-area",
  name: "Synthetic Area",
  description: "",
  active: true,
  effective_on: "2026-10-14",
  reviewed: true,
  shift_revision_ids: [id],
  days: [],
};
it("validates generated area fields and requires an explicit configuration review", () => {
  expect(rosterConfigSchema.parse(base).code).toBe("SYN-AREA");
  expect(rosterConfigSchema.safeParse({ ...base, reviewed: false }).success).toBe(false);
});
it.each([
  { effective_on: "2026-02-30" },
  { effective_on: "0000-01-01" },
  { tenant_id: "injected" },
  { shift_revision_ids: [id, id] },
  { shift_revision_ids: [] },
  { name: "Synthetic\0Invalid" },
])("rejects invalid configuration %j", (invalid) => {
  expect(rosterConfigSchema.safeParse({ ...base, ...invalid }).success).toBe(false);
});
it("checks complete weekly days, off-day assignments and duplicate shifts", () => {
  const days = Array.from({ length: 7 }, (_, weekday) => ({
    weekday,
    weekly_off: weekday > 0,
    allocations: weekday === 0 ? [{ shift_revision_id: id, area_revision_id: id }] : [],
  }));
  const template = { ...base, kind: "weekly_template", shift_revision_ids: [], days };
  expect(rosterConfigSchema.safeParse(template).success).toBe(true);
  expect(rosterConfigSchema.safeParse({ ...template, days: days.slice(0, 6) }).success).toBe(false);
  expect(
    rosterConfigSchema.safeParse({
      ...template,
      days: days.map((d) => ({ ...d, weekly_off: true })),
    }).success,
  ).toBe(false);
  expect(
    rosterConfigSchema.safeParse({
      ...template,
      days: days.map((d) =>
        d.weekday === 0 ? { ...d, allocations: [...d.allocations, ...d.allocations] } : d,
      ),
    }).success,
  ).toBe(false);
});
it("accepts generated readable records with omitted head config ID", () => {
  expect(
    rosterConfigContract.safeParse({
      ...base,
      id,
      revision_id: id,
      reviewed_by_subject: "synthetic-owner",
      version: 1,
      created_at: "2026-10-14T00:00:00Z",
      updated_at: "2026-10-14T00:00:00Z",
      shifts: [],
      expected_version: undefined,
      reviewed: undefined,
      shift_revision_ids: undefined,
    }).success,
  ).toBe(false);
  const { expected_version, reviewed, shift_revision_ids, ...record } = base;
  void expected_version;
  void reviewed;
  void shift_revision_ids;
  expect(
    rosterConfigContract.safeParse({
      ...record,
      id,
      revision_id: id,
      reviewed_by_subject: "synthetic-owner",
      version: 1,
      created_at: "2026-10-14T00:00:00Z",
      updated_at: "2026-10-14T00:00:00Z",
      shifts: [],
    }).success,
  ).toBe(true);
});
