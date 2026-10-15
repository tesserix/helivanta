import { expect, it } from "vitest";
import {
  dutyExchangeContract,
  dutyExchangeFormSchema,
  dutyExchangeInputContract,
  dutyExchangesPath,
  toDutyExchangeInput,
} from "./duty";
const a = "11111111-1111-4111-8111-111111111111";
const b = "22222222-2222-4222-8222-222222222222";
const c = "33333333-3333-4333-8333-333333333333";
const d = "44444444-4444-4444-8444-444444444444";
const form = {
  kind: "cover" as const,
  roster_id: c,
  requester_staff_id: a,
  requester_allocation_id: d,
  counterpart_staff_id: b,
  counterpart_allocation_id: "",
  counterpart_consent: true,
  reason: " Synthetic course ",
};
const issues = (value: object) =>
  dutyExchangeFormSchema.safeParse(value).error?.issues.map((i) => [i.path.join("."), i.message]);

it("turns a cover into the filing body without a counterpart duty", () => {
  const input = toDutyExchangeInput(dutyExchangeFormSchema.parse(form));
  expect(input).toEqual({
    kind: "cover",
    requester_allocation_id: d,
    counterpart_staff_id: b,
    counterpart_consent: true,
    reason: "Synthetic course",
  });
  expect(dutyExchangeInputContract.parse(input)).toEqual(input);
});

it("sends the counterpart duty only for swaps", () => {
  const input = toDutyExchangeInput(
    dutyExchangeFormSchema.parse({ ...form, kind: "swap", counterpart_allocation_id: c }),
  );
  expect(input.counterpart_allocation_id).toBe(c);
  expect(dutyExchangeInputContract.parse(input)).toEqual(input);
});

it("refuses an exchange with oneself, a swap without a return duty and missing consent", () => {
  expect(issues({ ...form, counterpart_staff_id: a })).toEqual([
    ["counterpart_staff_id", "Choose someone other than the person giving up the duty."],
  ]);
  expect(issues({ ...form, kind: "swap" })).toEqual([
    ["counterpart_allocation_id", "Choose the duty given in return."],
  ]);
  expect(issues({ ...form, counterpart_consent: false })).toEqual([
    ["counterpart_consent", "Confirm the other person has agreed to the exchange."],
  ]);
  expect(issues({ ...form, reason: "   " })).toEqual([
    ["reason", "Add a reason for the exchange."],
  ]);
  expect(issues({ ...form, reason: "x".repeat(501) })).toEqual([
    ["reason", "Keep the reason within 500 characters."],
  ]);
  expect(issues({ ...form, requester_allocation_id: "" })).toEqual([
    ["requester_allocation_id", "Choose the duty to hand over."],
  ]);
});

it("builds list paths with only the filters that are set", () => {
  expect(dutyExchangesPath("/b", {})).toBe("/b/hr/duty-exchanges");
  expect(dutyExchangesPath("/b", { status: "all", staff_id: "" })).toBe("/b/hr/duty-exchanges");
  expect(dutyExchangesPath("/b", { status: "pending", staff_id: a, from: "2026-01-01" })).toBe(
    `/b/hr/duty-exchanges?status=pending&staff_id=${a}&from=2026-01-01`,
  );
});

it("parses an exchange with its reason withheld", () => {
  const exchange = {
    id: a,
    kind: "cover",
    requester_staff_id: a,
    requester_employee_code: "SYN-N1",
    requester_display_name: "Synthetic One",
    counterpart_staff_id: b,
    counterpart_employee_code: "SYN-N2",
    counterpart_display_name: "Synthetic Two",
    requester_allocation_id: c,
    duty_from: "2027-03-01",
    duty_to: "2027-03-01",
    counterpart_consent: true,
    reason: "",
    status: "pending",
    requested_by_subject: "user-filer",
    version: 1,
    created_at: "2026-10-16T00:00:00Z",
    updated_at: "2026-10-16T00:00:00Z",
  };
  expect(dutyExchangeContract.safeParse(exchange).success).toBe(true);
  expect(dutyExchangeContract.safeParse({ ...exchange, extra: 1 }).success).toBe(false);
});
