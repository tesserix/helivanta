import "../../../test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../../test/select";
import Attendance from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const nurse = "22222222-2222-4222-8222-222222222222";
const periodID = "33333333-3333-4333-8333-333333333333";
const sheet1 = "44444444-4444-4444-8444-444444444444";
const sheet2 = "55555555-5555-4555-8555-555555555555";
const stamp = "2026-10-12T00:00:00Z";
const finaliser = ["workforce.attendance.finalise"];
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
const timesheet = (id: string, number: number) => ({
  id,
  period_id: periodID,
  number,
  people: 1,
  days: 2,
  shifts: 1,
  payable_minutes: 450,
  approved_overtime_minutes: 0,
  unpaid_half_days: 0,
  statuses: [{ code: "present", count: 1 }],
  exceptions: [],
  added_days: number === 1 ? 2 : 0,
  changed_days: number === 1 ? 0 : 1,
  removed_days: number === 1 ? 0 : 1,
  policy_revision_ids: [],
  note: number === 1 ? "" : "Synthetic register arrived",
  finalised_by_subject: "synthetic-manager",
  created_at: stamp,
});
const period = {
  id: periodID,
  from: "2026-10-01",
  to: "2026-10-07",
  status: "finalised",
  version: 3,
  timesheet: timesheet(sheet2, 2),
  created_by_subject: "synthetic-manager",
  created_at: stamp,
  updated_at: stamp,
};
const reportRow = {
  id: sheet1,
  staff_id: nurse,
  employee_code: "SYN-001",
  display_name: "Synthetic Nurse",
  job_category: "nurse",
  direction: "in",
  punched_at: "2026-10-12T02:35:00Z",
  local_date: "2026-10-12",
  timezone: "Asia/Kolkata",
  source: "manual",
  recorded_by_subject: "synthetic-recorder",
  recorded_at: stamp,
  voided: true,
  voided_by_subject: "synthetic-lead",
  voided_at: stamp,
};
afterEach(() => vi.unstubAllGlobals());

function mock({ permissions = finaliser, periods = [period] as unknown[], refuse = 0 } = {}) {
  const writes: { method: string; url: string; body: unknown }[] = [];
  const reads: string[] = [];
  let refusals = refuse;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      let code = 200;
      let body: unknown = page([]);
      if (init?.method === "PUT" || init?.method === "POST") {
        writes.push({ method: init.method, url, body: JSON.parse(String(init.body)) });
        if (refusals-- > 0)
          return new Response(
            JSON.stringify({
              error: "conflict",
              message: "Some shifts in the period are not settled yet.",
              reason: "period_not_ready",
              statuses: { upcoming: 2, no_policy: 1 },
            }),
            { status: 409 },
          );
        return new Response(JSON.stringify(period), { status: 200 });
      }
      reads.push(url);
      if (url.includes("permissions")) body = { data: permissions };
      else if (url.endsWith("/hospitals"))
        body = page([
          {
            id: hospital,
            name: "Synthetic Hospital",
            access_level: permissions.length ? "manager" : "reader",
          },
        ]);
      else if (url.endsWith("/staff"))
        body = page([
          {
            id: nurse,
            hospital_id: hospital,
            employee_code: "SYN-001",
            display_name: "Synthetic Nurse",
            job_category: "nurse",
            employment_status: "active",
            created_at: stamp,
            updated_at: stamp,
          },
        ]);
      else if (url.includes("/hr/attendance-totals"))
        body = {
          date: "2026-10-12",
          people: 0,
          shifts: 0,
          payable_minutes: 0,
          approved_overtime_minutes: 0,
          unpaid_half_days: 0,
          statuses: [],
          exceptions: [],
        };
      else if (url.includes("/hr/attendance/punch-report")) body = page([reportRow]);
      else if (url.endsWith("/hr/attendance/periods")) body = page(periods);
      else if (url.endsWith(`/periods/${periodID}`)) body = period;
      else if (url.endsWith("/timesheets"))
        body = page([timesheet(sheet2, 2), timesheet(sheet1, 1)]);
      else if (url.endsWith("/history"))
        body = page([
          {
            id: sheet2,
            version: 3,
            status: "finalised",
            timesheet_id: sheet2,
            note: "Synthetic register arrived",
            actor_subject: "synthetic-manager",
            created_at: stamp,
          },
        ]);
      else if (url.includes("/people"))
        body = page([
          {
            staff_id: nurse,
            employee_code: "SYN-001",
            display_name: "Synthetic Nurse",
            days: 1,
            shifts: 1,
            payable_minutes: 450,
            approved_overtime_minutes: 0,
            unpaid_half_days: 0,
            statuses: [{ code: "present", count: 1 }],
            exceptions: [],
          },
        ]);
      else if (url.includes("/days"))
        body = page([
          {
            staff_id: nurse,
            date: "2026-10-04",
            change: "removed",
            shifts: 0,
            payable_minutes: 0,
            approved_overtime_minutes: 0,
            unpaid_half_days: 0,
          },
        ]);
      else if (url.includes("/attendance-settings") || url.endsWith("/roster-settings"))
        [code, body] = [404, { error: "not_found", message: "not found" }];
      return new Response(JSON.stringify(body), { status: code });
    }),
  );
  return { writes, reads };
}

it("finalises a period within 31 days and replays the same id after a refusal", async () => {
  const { writes } = mock({ periods: [], refuse: 1 });
  const { user } = renderWithProviders(<Attendance />);
  const panel = await screen.findByRole("region", { name: "Timesheets" });
  expect(await within(panel).findByText("No period has been finalised yet.")).toBeInTheDocument();
  await user.click(within(panel).getByRole("button", { name: "Finalise a period" }));
  const form = within(panel).getByRole("form", { name: "Finalise period" });
  await user.type(within(form).getByLabelText("First day"), "2026-09-01");
  await user.type(within(form).getByLabelText("Last day"), "2026-10-02");
  await user.click(within(form).getByRole("button", { name: "Finalise period" }));
  expect(await within(form).findByText("Keep the period within 31 days.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  const last = within(form).getByLabelText("Last day");
  await user.clear(last);
  await user.type(last, "2026-10-01");
  await user.type(within(form).getByLabelText("Note (optional)"), " Synthetic month close ");
  await user.click(within(form).getByRole("button", { name: "Finalise period" }));
  expect(
    await within(form).findByText(
      "Some shifts in the period are not settled yet. Upcoming 2, No attendance policy 1. Your input is preserved.",
    ),
  ).toBeInTheDocument();
  await user.click(within(form).getByRole("button", { name: "Finalise period" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0].method).toBe("PUT");
  expect(writes[0].url).toMatch(/\/hr\/attendance\/periods\/[0-9a-f-]{36}$/);
  expect(writes[1].url).toBe(writes[0].url);
  expect(writes[1].body).toEqual({
    from: "2026-09-01",
    to: "2026-10-01",
    note: "Synthetic month close",
  });
  expect(
    await within(panel).findByRole("region", { name: "Attendance period" }),
  ).toBeInTheDocument();
});

it("shows versions, people and marked days, and reopens only with a reason", async () => {
  const { writes, reads } = mock();
  const { user } = renderWithProviders(<Attendance />);
  const panel = await screen.findByRole("region", { name: "Timesheets" });
  await user.click(
    await within(panel).findByRole("button", { name: "Open period 2026-10-01 to 2026-10-07" }),
  );
  const detail = await within(panel).findByRole("region", { name: "Attendance period" });
  expect(
    await within(detail).findByText("Version 2 · 1 changed · 0 added · 1 removed"),
  ).toBeInTheDocument();
  expect(within(detail).getByText("Version 1 · 2 days")).toBeInTheDocument();
  const people = within(detail).getByRole("region", { name: "Version 2 people" });
  expect(await within(people).findByText("Synthetic Nurse · SYN-001")).toBeInTheDocument();
  const days = within(detail).getByRole("region", { name: "Version 2 days" });
  expect(
    await within(days).findByText("2026-10-04 · Synthetic Nurse · SYN-001 · Removed"),
  ).toBeInTheDocument();
  await chooseOption(user, within(days).getByRole("combobox", { name: "Change" }), "Removed");
  await waitFor(() =>
    expect(reads).toContainEqual(
      expect.stringContaining(`/timesheets/${sheet2}/days?change=removed`),
    ),
  );
  await user.click(within(detail).getByRole("button", { name: "Show version 1" }));
  expect(await within(detail).findByRole("region", { name: "Version 1 days" })).toBeInTheDocument();

  const steps = within(detail).getByRole("form", { name: "Change period" });
  expect(within(steps).getByRole("button", { name: "Recalculate period" })).toBeInTheDocument();
  await user.click(within(steps).getByRole("button", { name: "Reopen period" }));
  expect(await within(steps).findByText("Say why.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.type(within(steps).getByLabelText("Reason"), "Synthetic register arrived");
  await user.click(within(steps).getByRole("button", { name: "Reopen period" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "POST",
    url: expect.stringContaining(`/hr/attendance/periods/${periodID}/reopening`),
    body: { expected_version: 3, note: "Synthetic register arrived" },
  });
});

it("shows readers timesheets without finalising controls", async () => {
  mock({ permissions: [] });
  const { user } = renderWithProviders(<Attendance />);
  const panel = await screen.findByRole("region", { name: "Timesheets" });
  await user.click(await within(panel).findByRole("button", { name: /^Open period/ }));
  const detail = await within(panel).findByRole("region", { name: "Attendance period" });
  expect(
    await within(detail).findByText("Version 2 · 1 changed · 0 added · 1 removed"),
  ).toBeInTheDocument();
  expect(within(detail).queryByRole("form", { name: "Change period" })).toBeNull();
  expect(within(panel).queryByRole("button", { name: "Finalise a period" })).toBeNull();
});

it("lists every punch hospital-wide with void state and bounds the range", async () => {
  const { reads } = mock();
  const { user } = renderWithProviders(<Attendance />);
  const report = await screen.findByRole("region", { name: "Punch report" });
  const row = (await within(report).findByText("Synthetic Nurse · SYN-001")).closest("tr")!;
  expect(within(row).getByText("Nurse")).toBeInTheDocument();
  expect(within(row).getByText("08:05")).toBeInTheDocument();
  expect(within(row).getByText(/^Voided by synthetic-lead/)).toBeInTheDocument();
  await chooseOption(
    user,
    within(report).getByRole("combobox", { name: "Staff member" }),
    /SYN-001/,
  );
  await waitFor(() =>
    expect(reads).toContainEqual(
      expect.stringMatching(new RegExp(`/punch-report\\?from=.*&staff_id=${nurse}$`)),
    ),
  );
  const before = reads.length;
  const from = within(report).getByLabelText("From");
  await user.clear(from);
  await user.type(from, "2026-01-01");
  expect(await within(report).findByText("Choose at most 31 days.")).toBeInTheDocument();
  expect(reads.slice(before).filter((u) => u.includes("punch-report"))).toHaveLength(0);
});
