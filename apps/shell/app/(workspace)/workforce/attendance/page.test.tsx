import "@/test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "@/test/select";
import Attendance from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const nurse = "22222222-2222-4222-8222-222222222222";
const punchID = "44444444-4444-4444-8444-444444444444";
const revision = "55555555-5555-4555-8555-555555555555";
const allocation = "66666666-6666-4666-8666-666666666666";
const stamp = "2026-10-12T00:00:00Z";
const settings = {
  revision_id: revision,
  early_arrival_minutes: 60,
  late_departure_minutes: 120,
  early_departure_grace_minutes: 10,
  short_duty_minutes: 30,
  duplicate_window_minutes: 5,
  backdate_days: 7,
  effective_on: "2026-10-01",
  reviewed_by_subject: "synthetic-manager",
  version: 1,
  created_at: stamp,
  updated_at: stamp,
};
const rosterSettings = (timezone: string) => ({
  revision_id: revision,
  timezone,
  week_start: 1,
  auto_population: "disabled",
  effective_on: "2026-10-01",
  reviewed_by_subject: "synthetic-manager",
  version: 1,
  created_at: stamp,
  updated_at: stamp,
});
const day = {
  staff_id: nurse,
  employee_code: "SYN-001",
  display_name: "Synthetic Nurse",
  date: "2026-10-12",
  timezone: "Asia/Kolkata",
  leave: "short",
  short_leave_minutes: 60,
  approved_overtime_minutes: 90,
  payable_minutes: 445,
  shifts: [
    {
      allocation_id: allocation,
      roster_id: revision,
      shift_revision_id: revision,
      shift_code: "DAY",
      shift_name: "Day",
      status: "present",
      starts_at: "2026-10-12T02:30:00Z",
      ends_at: "2026-10-12T10:30:00Z",
      break_minutes: 30,
      grace_minutes: 0,
      policy_revision_id: revision,
      first_in: "2026-10-12T02:35:00Z",
      last_out: "2026-10-12T10:30:00Z",
      late_minutes: 5,
      early_departure_minutes: 0,
      worked_minutes: 445,
      expected_minutes: 450,
      payable_minutes: 445,
      unpaid_half_days: 0,
      exceptions: ["late"],
      punch_ids: [punchID],
      ignored_punch_ids: [],
    },
  ],
  stray_punch_ids: [punchID],
  exceptions: ["late", "stray_punch"],
};
const totals = {
  date: "2026-10-12",
  people: 1,
  shifts: 1,
  payable_minutes: 445,
  approved_overtime_minutes: 90,
  unpaid_half_days: 0,
  statuses: [{ code: "present", count: 1 }],
  exceptions: [{ code: "late", count: 1 }],
};
const punch = (voided = false, reason = "Synthetic register entry") => ({
  id: punchID,
  staff_id: nurse,
  employee_code: "SYN-001",
  display_name: "Synthetic Nurse",
  direction: "in",
  punched_at: "2026-10-12T02:35:00Z",
  local_date: "2026-10-12",
  timezone: "Asia/Kolkata",
  source: "manual",
  policy_revision_id: revision,
  reason,
  recorded_by_subject: "synthetic-recorder",
  created_at: stamp,
  voided,
  ...(voided
    ? { void_reason: "Wrong person", voided_by_subject: "synthetic-lead", voided_at: stamp }
    : {}),
});
const correctionID = "77777777-7777-4777-8777-777777777777";
const correction = {
  id: correctionID,
  staff_id: nurse,
  employee_code: "SYN-001",
  display_name: "Synthetic Nurse",
  kind: "replace",
  target_punch_id: punchID,
  target_direction: "in",
  target_punched_at: "2026-10-12T04:00:00Z",
  direction: "in",
  punched_at: "2026-10-12T02:30:00Z",
  local_date: "2026-10-12",
  timezone: "Asia/Kolkata",
  reason: "Synthetic clock running fast",
  status: "pending",
  requested_by_subject: "synthetic-filer",
  version: 1,
  created_at: stamp,
  updated_at: stamp,
};
const corrector = ["workforce.attendance.correct"];
const approver = ["workforce.attendance.approve"];
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
function mock({
  permissions = ["workforce.attendance.record", "workforce.attendance.configure"],
  policy = true,
  timezone = "Asia/Kolkata" as string | null,
  people = true,
  voided = false,
  fail = false,
  reject = "",
  corrections = [] as unknown[],
}: {
  permissions?: string[];
  policy?: boolean;
  timezone?: string | null;
  people?: boolean;
  voided?: boolean;
  fail?: boolean;
  reject?: string;
  corrections?: unknown[];
} = {}) {
  const writes: { method: string; url: string; body: unknown }[] = [];
  const reads: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      let code = 200;
      let body: unknown = page([]);
      if (url.includes("permissions")) body = { data: permissions };
      else if (url.endsWith("/hospitals"))
        body = page([
          {
            id: hospital,
            name: "Synthetic Hospital",
            access_level: permissions.length ? "manager" : "reader",
          },
        ]);
      else if (url.endsWith("/attendance-settings/history")) body = page([settings]);
      else if (url.endsWith("/attendance-settings"))
        [code, body] = policy
          ? [200, settings]
          : [404, { error: "not_found", message: "attendance settings not found" }];
      else if (url.endsWith("/roster-settings"))
        [code, body] = timezone
          ? [200, rosterSettings(timezone)]
          : [404, { error: "not_found", message: "roster settings not found" }];
      else if (url.includes("/hr/attendance-totals"))
        body = people ? totals : { ...totals, people: 0, shifts: 0, statuses: [], exceptions: [] };
      else if (url.includes("/hr/attendance/corrections")) {
        reads.push(url);
        body = url.endsWith("/history")
          ? page([
              {
                id: revision,
                version: 1,
                status: "pending",
                note: "",
                actor_subject: "synthetic-filer",
                created_at: stamp,
              },
            ])
          : url.endsWith(correctionID)
            ? correction
            : page(corrections);
      } else if (url.includes("/hr/attendance/punches")) {
        reads.push(url);
        body = page([punch(voided, permissions.length ? "Synthetic register entry" : "")]);
      } else if (url.includes("/hr/attendance?")) {
        reads.push(url);
        [code, body] = fail
          ? [500, { error: "internal", message: "boom" }]
          : [200, page(people ? [day] : [])];
      } else if (url.endsWith("/staff"))
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
      if (init?.method === "PUT" || init?.method === "POST") {
        writes.push({ method: init.method, url, body: JSON.parse(String(init.body)) });
        [code, body] = reject
          ? [409, { error: "conflict", message: reject }]
          : [
              200,
              url.endsWith("/attendance-settings")
                ? settings
                : url.includes("/corrections")
                  ? correction
                  : punch(init.method === "POST"),
            ];
      }
      return new Response(JSON.stringify(body), { status: code });
    }),
  );
  return { writes, reads };
}

const showDate = async (user: ReturnType<typeof renderWithProviders>["user"], date: string) => {
  const input = await screen.findByLabelText("Date");
  await user.clear(input);
  await user.type(input, date);
};

it("shows a missing policy and an empty day read-only to readers", async () => {
  mock({ permissions: [], policy: false, people: false });
  const { user } = renderWithProviders(<Attendance />);
  expect(await screen.findByText(/No attendance policy has been reviewed/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Configure attendance policy" })).toBeNull();
  await showDate(user, "2026-10-12");
  expect(
    await screen.findByText("Nobody is rostered, on leave or punched on 2026-10-12."),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Record punch" })).toBeNull();
});

it("shows the day's shifts in hospital time with totals and exceptions", async () => {
  const { reads } = mock();
  const { user } = renderWithProviders(<Attendance />);
  await showDate(user, "2026-10-12");
  const sheet = screen.getByRole("region", { name: "Attendance by day" });
  const row = (await within(sheet).findByText("Synthetic Nurse")).closest("tr")!;
  expect(within(row).getByText("Day 08:00–16:00")).toBeInTheDocument();
  expect(
    within(row).getByText(/In 08:05 · Out 16:00 · worked 7 h 25 min of 7 h 30 min/),
  ).toBeInTheDocument();
  expect(within(row).getByText(/5 min late/)).toBeInTheDocument();
  expect(within(row).getByText("Short leave · 1 h")).toBeInTheDocument();
  expect(within(row).getByText("1 punch outside any shift")).toBeInTheDocument();
  expect(within(row).getByText("Late, Punch outside any shift")).toBeInTheDocument();
  expect(within(row).getByText("+ 1 h 30 min approved overtime")).toBeInTheDocument();
  const day = within(sheet).getByRole("region", { name: "Day totals" });
  expect(within(day).getByText("Present 1")).toBeInTheDocument();
  expect(within(day).getByText("Late 1")).toBeInTheDocument();
  expect(reads.at(-1)).toMatch(/\/hr\/attendance\?date=2026-10-12$/);
});

it("offers a retry when attendance cannot be loaded", async () => {
  mock({ fail: true });
  renderWithProviders(<Attendance />);
  expect(await screen.findByText("Could not load attendance.")).toBeInTheDocument();
});

it("refuses allowances outside their limits and saves a reviewed policy", async () => {
  const { writes } = mock();
  const { user } = renderWithProviders(<Attendance />);
  await user.click(await screen.findByRole("button", { name: "Edit attendance policy" }));
  const form = screen.getByRole("form", { name: "Edit attendance policy" });
  const window = within(form).getByLabelText("Duplicate punch window (minutes)");
  await user.clear(window);
  await user.type(window, "0");
  await user.click(within(form).getByRole("button", { name: "Save attendance policy" }));
  expect(
    await within(form).findByText("Use at least 1 for the duplicate window."),
  ).toBeInTheDocument();
  expect(within(form).getByText("Confirm that you reviewed the policy.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.clear(window);
  await user.type(window, "3");
  await user.click(within(form).getByRole("checkbox"));
  await user.click(within(form).getByRole("button", { name: "Save attendance policy" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "PUT",
    url: expect.stringMatching(/\/hr\/attendance-settings$/),
    body: {
      expected_version: 1,
      early_arrival_minutes: 60,
      late_departure_minutes: 120,
      early_departure_grace_minutes: 10,
      short_duty_minutes: 30,
      duplicate_window_minutes: 3,
      backdate_days: 7,
      effective_on: "2026-10-01",
      reviewed: true,
    },
  });
});

it("records a punch in hospital time and replays the same id on retry", async () => {
  const { writes } = mock({ reject: "A punch in this direction was recorded moments ago." });
  const { user } = renderWithProviders(<Attendance />);
  await showDate(user, "2026-10-12");
  await user.click(await screen.findByRole("button", { name: "Record punch" }));
  const form = screen.getByRole("form", { name: "Record punch" });
  expect(within(form).getByText(/hospital's timezone, Asia\/Kolkata/)).toBeInTheDocument();
  await user.click(within(form).getByRole("button", { name: "Record punch" }));
  expect(await within(form).findByText("Choose a staff member.")).toBeInTheDocument();
  expect(within(form).getByText("Use a 24-hour time such as 08:05.")).toBeInTheDocument();
  expect(within(form).getByText("Say why this punch is entered by hand.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await chooseOption(user, within(form).getByRole("combobox", { name: "Staff member" }), /SYN-001/);
  await chooseOption(user, within(form).getByRole("combobox", { name: "Direction" }), "Out");
  await user.type(within(form).getByLabelText("Time (24-hour)"), "00:10");
  await user.type(within(form).getByLabelText("Reason"), " Biometric reader offline ");
  await user.click(within(form).getByRole("button", { name: "Record punch" }));
  expect(await within(form).findByRole("alert")).toHaveTextContent("moments ago");
  await user.click(within(form).getByRole("button", { name: "Record punch" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0].url).toMatch(/\/hr\/attendance\/punches\/[0-9a-f-]{36}$/);
  expect(writes[1].url).toBe(writes[0].url);
  expect(writes[0]).toEqual({
    method: "PUT",
    url: writes[0].url,
    body: {
      staff_id: nurse,
      direction: "out",
      punched_at: "2026-10-11T18:40:00Z",
      reason: "Biometric reader offline",
    },
  });
});

it("refuses a wall-clock time skipped by a clock change", async () => {
  const { writes } = mock({ timezone: "America/New_York" });
  const { user } = renderWithProviders(<Attendance />);
  await showDate(user, "2026-03-08");
  await user.click(await screen.findByRole("button", { name: "Record punch" }));
  const form = screen.getByRole("form", { name: "Record punch" });
  await chooseOption(user, within(form).getByRole("combobox", { name: "Staff member" }), /SYN-001/);
  await user.type(within(form).getByLabelText("Time (24-hour)"), "02:30");
  await user.type(within(form).getByLabelText("Reason"), "Synthetic register entry");
  await user.click(within(form).getByRole("button", { name: "Record punch" }));
  expect(
    await within(form).findByText(
      "This time does not exist in America/New_York because the clocks changed.",
    ),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
});

it("withholds punch entry until the roster sets a timezone", async () => {
  mock({ timezone: null });
  renderWithProviders(<Attendance />);
  expect(
    await screen.findByText(/Punches can be recorded once the hospital's roster settings/),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Record punch" })).toBeNull();
});

it("lists a person's punches within a bounded range", async () => {
  const { reads } = mock({ voided: true });
  const { user } = renderWithProviders(<Attendance />);
  const ledger = await screen.findByRole("region", { name: "Punch ledger" });
  expect(
    within(ledger).getByText("Choose a staff member to see their punches."),
  ).toBeInTheDocument();
  await chooseOption(
    user,
    within(ledger).getByRole("combobox", { name: "Staff member" }),
    /SYN-001/,
  );
  const from = within(ledger).getByLabelText("From");
  await user.clear(from);
  await user.type(from, "2026-08-01");
  const to = within(ledger).getByLabelText("To");
  await user.clear(to);
  await user.type(to, "2026-10-12");
  expect(await within(ledger).findByText("Choose at most 42 days.")).toBeInTheDocument();
  await user.clear(from);
  await user.type(from, "2026-10-01");
  expect(await within(ledger).findByText("2026-10-12 · 08:05 · In · Voided")).toBeInTheDocument();
  expect(within(ledger).getByText(/voided by synthetic-lead: Wrong person/)).toBeInTheDocument();
  expect(within(ledger).queryByRole("button", { name: /^Void in punch/ })).toBeNull();
  expect(reads.at(-1)).toContain(`staff_id=${nurse}&from=2026-10-01&to=2026-10-12`);
});

it("hides reasons and void actions from readers", async () => {
  mock({ permissions: [] });
  const { user } = renderWithProviders(<Attendance />);
  const ledger = await screen.findByRole("region", { name: "Punch ledger" });
  await chooseOption(
    user,
    within(ledger).getByRole("combobox", { name: "Staff member" }),
    /SYN-001/,
  );
  const from = within(ledger).getByLabelText("From");
  await user.clear(from);
  await user.type(from, "2026-10-01");
  const to = within(ledger).getByLabelText("To");
  await user.clear(to);
  await user.type(to, "2026-10-12");
  expect(await within(ledger).findByText("Recorded by synthetic-recorder")).toBeInTheDocument();
  expect(within(ledger).queryByRole("button", { name: /^Void/ })).toBeNull();
});

it("requires a reason and confirmation before voiding a punch", async () => {
  const { writes } = mock();
  const { user } = renderWithProviders(<Attendance />);
  const ledger = await screen.findByRole("region", { name: "Punch ledger" });
  await chooseOption(
    user,
    within(ledger).getByRole("combobox", { name: "Staff member" }),
    /SYN-001/,
  );
  const from = within(ledger).getByLabelText("From");
  await user.clear(from);
  await user.type(from, "2026-10-01");
  const to = within(ledger).getByLabelText("To");
  await user.clear(to);
  await user.type(to, "2026-10-12");
  expect(await within(ledger).findByText(/Synthetic register entry/)).toBeInTheDocument();
  await user.click(
    within(ledger).getByRole("button", { name: "Void in punch on 2026-10-12 at 08:05" }),
  );
  const form = within(ledger).getByRole("form", { name: "Void punch" });
  await user.click(within(form).getByRole("button", { name: "Void punch" }));
  expect(await within(form).findByText("Say why this punch is being voided.")).toBeInTheDocument();
  await user.type(within(form).getByLabelText("Reason"), "  Recorded against the wrong person ");
  await user.click(within(form).getByRole("button", { name: "Void punch" }));
  const dialog = await screen.findByRole("dialog");
  expect(writes).toHaveLength(0);
  await user.click(within(dialog).getByRole("button", { name: "Void punch" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "POST",
    url: expect.stringContaining(`/hr/attendance/punches/${punchID}/void`),
    body: { reason: "Recorded against the wrong person" },
  });
});

const openLedger = async (user: ReturnType<typeof renderWithProviders>["user"]) => {
  const ledger = await screen.findByRole("region", { name: "Punch ledger" });
  await chooseOption(
    user,
    within(ledger).getByRole("combobox", { name: "Staff member" }),
    /SYN-001/,
  );
  const from = within(ledger).getByLabelText("From");
  await user.clear(from);
  await user.type(from, "2026-10-01");
  const to = within(ledger).getByLabelText("To");
  await user.clear(to);
  await user.type(to, "2026-10-12");
  return ledger;
};

it("files a retime from the ledger in hospital time and replays the same id on retry", async () => {
  const { writes } = mock({
    permissions: corrector,
    reject: "a pending correction already targets this punch",
  });
  const { user } = renderWithProviders(<Attendance />);
  const ledger = await openLedger(user);
  expect(within(ledger).queryByRole("button", { name: /^Void in punch/ })).toBeNull();
  await user.click(
    await within(ledger).findByRole("button", {
      name: "Request correction to in punch on 2026-10-12 at 08:05",
    }),
  );
  const form = within(ledger).getByRole("form", { name: "Request correction" });
  expect(within(form).getByLabelText("Time (24-hour)")).toHaveValue("08:05");
  await user.click(within(form).getByRole("button", { name: "Request correction" }));
  expect(await within(form).findByText("Say why this correction is needed.")).toBeInTheDocument();
  const time = within(form).getByLabelText("Time (24-hour)");
  await user.clear(time);
  await user.type(time, "08:00");
  await user.type(within(form).getByLabelText("Reason"), " Synthetic clock running fast ");
  await user.click(within(form).getByRole("button", { name: "Request correction" }));
  expect(
    await within(form).findByText(/a pending correction already targets this punch/),
  ).toBeInTheDocument();
  await user.click(within(form).getByRole("button", { name: "Request correction" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual({
    method: "PUT",
    url: expect.stringMatching(/\/hr\/attendance\/corrections\/[0-9a-f-]{36}$/),
    body: {
      staff_id: nurse,
      kind: "replace",
      target_punch_id: punchID,
      direction: "in",
      punched_at: "2026-10-12T02:30:00Z",
      reason: "Synthetic clock running fast",
    },
  });
  expect(writes[1].url).toBe(writes[0].url);
});

it("sends only the target when a punch is voided by correction", async () => {
  const { writes } = mock({ permissions: corrector });
  const { user } = renderWithProviders(<Attendance />);
  const ledger = await openLedger(user);
  await user.click(
    await within(ledger).findByRole("button", { name: /^Request correction to in punch/ }),
  );
  const form = within(ledger).getByRole("form", { name: "Request correction" });
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Correction" }),
    "Void punch",
  );
  expect(within(form).queryByLabelText("Time (24-hour)")).toBeNull();
  await user.type(within(form).getByLabelText("Reason"), "Synthetic wrong person");
  await user.click(within(form).getByRole("button", { name: "Request correction" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0].body).toEqual({
    staff_id: nurse,
    kind: "void",
    target_punch_id: punchID,
    reason: "Synthetic wrong person",
  });
});

it("adds a missing punch from the corrections board", async () => {
  const { writes } = mock({ permissions: corrector });
  const { user } = renderWithProviders(<Attendance />);
  const board = await screen.findByRole("region", { name: "Attendance corrections" });
  expect(await within(board).findByText("No pending corrections.")).toBeInTheDocument();
  await user.click(await within(board).findByRole("button", { name: "Add missing punch" }));
  const form = within(board).getByRole("form", { name: "Request correction" });
  await user.click(within(form).getByRole("button", { name: "Request correction" }));
  expect(await within(form).findByText("Choose a staff member.")).toBeInTheDocument();
  expect(within(form).getByText("Use a 24-hour time such as 08:05.")).toBeInTheDocument();
  await chooseOption(user, within(form).getByRole("combobox", { name: "Staff member" }), /SYN-001/);
  await chooseOption(user, within(form).getByRole("combobox", { name: "Direction" }), "Out");
  const date = within(form).getByLabelText("Date");
  await user.clear(date);
  await user.type(date, "2026-10-12");
  await user.type(within(form).getByLabelText("Time (24-hour)"), "16:00");
  await user.type(within(form).getByLabelText("Reason"), "Synthetic missed out punch");
  await user.click(within(form).getByRole("button", { name: "Request correction" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0].body).toEqual({
    staff_id: nurse,
    kind: "add",
    direction: "out",
    punched_at: "2026-10-12T10:30:00Z",
    reason: "Synthetic missed out punch",
  });
});

it("rejects only with a note and approves a retime only after confirmation", async () => {
  const { writes } = mock({ permissions: approver, corrections: [correction] });
  const { user } = renderWithProviders(<Attendance />);
  const board = await screen.findByRole("region", { name: "Attendance corrections" });
  expect(
    await within(board).findByText("2026-10-12 · Retime punch · In at 09:30 → In at 08:00"),
  ).toBeInTheDocument();
  expect(within(board).queryByRole("button", { name: "Add missing punch" })).toBeNull();
  await user.click(
    within(board).getByRole("button", {
      name: "Review correction for Synthetic Nurse on 2026-10-12",
    }),
  );
  const review = await within(board).findByRole("region", { name: "Correction request" });
  expect(
    await within(review).findByText("Reason: Synthetic clock running fast"),
  ).toBeInTheDocument();
  expect(within(review).queryByRole("button", { name: "Cancel correction" })).toBeNull();
  await user.click(within(review).getByRole("button", { name: "Reject correction" }));
  expect(
    await within(review).findByText("Add a note explaining the rejection."),
  ).toBeInTheDocument();
  await user.click(within(review).getByRole("button", { name: "Approve correction" }));
  const dialog = await screen.findByRole("dialog");
  expect(writes).toHaveLength(0);
  await user.click(within(dialog).getByRole("button", { name: "Approve correction" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "POST",
    url: expect.stringContaining(`/hr/attendance/corrections/${correctionID}/decision`),
    body: { expected_version: 1, decision: "approve", note: "" },
  });
});

it("lets a filer cancel a pending correction", async () => {
  const { writes } = mock({ permissions: corrector, corrections: [correction] });
  const { user } = renderWithProviders(<Attendance />);
  const board = await screen.findByRole("region", { name: "Attendance corrections" });
  await user.click(
    await within(board).findByRole("button", { name: /^Review correction for Synthetic Nurse/ }),
  );
  const review = await within(board).findByRole("region", { name: "Correction request" });
  expect(within(review).queryByRole("button", { name: "Approve correction" })).toBeNull();
  await user.click(await within(review).findByRole("button", { name: "Cancel correction" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "POST",
    url: expect.stringContaining(`/hr/attendance/corrections/${correctionID}/cancellation`),
    body: { expected_version: 1, note: "" },
  });
});

it("shows readers the corrections board without actions", async () => {
  mock({ permissions: [], corrections: [correction] });
  const { user } = renderWithProviders(<Attendance />);
  const board = await screen.findByRole("region", { name: "Attendance corrections" });
  await user.click(
    await within(board).findByRole("button", { name: /^Review correction for Synthetic Nurse/ }),
  );
  const review = await within(board).findByRole("region", { name: "Correction request" });
  expect(await within(review).findByText("Version 1")).toBeInTheDocument();
  expect(within(review).queryByRole("form", { name: "Decide correction" })).toBeNull();
  expect(within(board).queryByRole("button", { name: "Add missing punch" })).toBeNull();
});
