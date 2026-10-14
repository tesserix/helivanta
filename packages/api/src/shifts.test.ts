import { expect, it } from "vitest";
import { shiftDuration, shiftSchema } from "./shifts";
const shift = {
  expected_version: 0,
  shift_type: "night",
  code: "syn-night",
  name: "Synthetic Night",
  description: "",
  active: true,
  start_time: "22:00",
  end_time: "06:00",
  break_minutes: 30,
  grace_minutes: 0,
  full_day_ot_minutes: 0,
  half_day_ot_minutes: 0,
  locum: false,
};
it("validates an overnight shift with explicitly configured policy inputs", () => {
  expect(shiftDuration("22:00", "06:00")).toBe(480);
  expect(shiftSchema.parse(shift).code).toBe("SYN-NIGHT");
});
it.each([
  { end_time: "22:00" },
  { start_time: "25:00" },
  { break_minutes: 480 },
  { half_day_ot_minutes: 1 },
  { grace_minutes: -1 },
  { tenant_id: "injected" },
  { name: "Synthetic\0Invalid" },
])("rejects invalid time, policy or scope injection %j", (invalid) => {
  expect(shiftSchema.safeParse({ ...shift, ...invalid }).success).toBe(false);
});
