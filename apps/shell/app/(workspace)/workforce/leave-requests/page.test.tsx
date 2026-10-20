import "@/test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "@/test/select";
import LeaveRequests from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const staffID = "22222222-2222-4222-8222-222222222222";
const casual = "33333333-3333-4333-8333-333333333333";
const requestID = "44444444-4444-4444-8444-444444444444";
const stamp = "2026-10-15T00:00:00Z";
const request = (status = "pending", reason = "Synthetic family event") => ({
  id: requestID,
  staff_id: staffID,
  employee_code: "SYN-001",
  display_name: "Synthetic Nurse",
  leave_type_id: casual,
  leave_type_revision_id: "55555555-5555-4555-8555-555555555555",
  code: "SYN-CL",
  name: "Synthetic Casual",
  assignment_revision_id: "66666666-6666-4666-8666-666666666666",
  leave_year: 2026,
  unit: "full_day",
  start_on: "2026-11-02",
  end_on: "2026-11-04",
  half_days: 4,
  reason,
  status,
  requested_by_subject: "synthetic-filer",
  version: status === "pending" ? 1 : 2,
  created_at: stamp,
  updated_at: stamp,
});
const casualType = {
  id: casual,
  revision_id: "55555555-5555-4555-8555-555555555555",
  kind: "leave_type",
  code: "SYN-CL",
  name: "Synthetic Casual",
  description: "",
  active: true,
  effective_on: "2026-01-01",
  reviewed_by_subject: "synthetic-owner",
  version: 1,
  created_at: stamp,
  updated_at: stamp,
  holidays: [],
  entries: [],
  rules: {
    paid: true,
    balance_tracked: true,
    half_day_allowed: false,
    short_leave_minutes: 60,
    clubbable: true,
  },
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
function mock({
  permissions = ["workforce.leave.request", "workforce.leave.approve"],
  status = "pending",
  reason = "Synthetic family event",
  requests = true,
  fail = false,
  reject = "",
}: {
  permissions?: string[];
  status?: string;
  reason?: string;
  requests?: boolean;
  fail?: boolean;
  reject?: string;
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
      else if (url.includes("/hr/leave-requests")) {
        reads.push(url);
        [code, body] = fail
          ? [500, { error: "internal", message: "boom" }]
          : [200, page(requests ? [request(status, reason)] : [])];
      } else if (url.endsWith("/history"))
        body = page([
          {
            id: "77777777-7777-4777-8777-777777777777",
            version: 1,
            status: "pending",
            note: "",
            actor_subject: "synthetic-filer",
            created_at: stamp,
          },
        ]);
      else if (url.endsWith(`/leave-requests/${requestID}`))
        body = {
          request: request(status, reason),
          days: [
            { day: "2026-11-02", half_days: 2, holiday: false, calendar_revision_id: casual },
            { day: "2026-11-03", half_days: 0, holiday: true, calendar_revision_id: casual },
            { day: "2026-11-04", half_days: 2, holiday: false, calendar_revision_id: casual },
          ],
          roster_clashes: [
            {
              roster_id: "88888888-8888-4888-8888-888888888888",
              roster_name: "Synthetic Ward Roster",
              roster_date: "2026-11-02",
              starts_at: "2026-11-02T08:00:00Z",
              ends_at: "2026-11-02T16:00:00Z",
            },
          ],
        };
      else if (url.includes("kind=leave_type")) body = page([casualType]);
      else if (url.endsWith("/staff"))
        body = page([
          {
            id: staffID,
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
          : [200, { request: request("approved"), days: [], roster_clashes: [] }];
      }
      return new Response(JSON.stringify(body), { status: code });
    }),
  );
  return { writes, reads };
}

it("shows an empty pending queue and lists every status on request", async () => {
  const { reads } = mock({ requests: false });
  const { user } = renderWithProviders(<LeaveRequests />);
  expect(await screen.findByText("No pending leave requests.")).toBeInTheDocument();
  expect(reads.at(-1)).toContain("/hr/leave-requests?status=pending");
  await chooseOption(user, screen.getByRole("combobox", { name: "Status" }), "All statuses");
  expect(
    await screen.findByText("No leave requests have been filed in this hospital."),
  ).toBeInTheDocument();
  expect(reads.at(-1)).toMatch(/\/hr\/leave-requests$/);
});

it("offers a retry when requests cannot be loaded", async () => {
  mock({ fail: true });
  renderWithProviders(<LeaveRequests />);
  expect(await screen.findByText("Could not load leave requests.")).toBeInTheDocument();
});

it("shows a request read-only, with clashes and history, to readers", async () => {
  mock({ permissions: [], reason: "" });
  const { user } = renderWithProviders(<LeaveRequests />);
  expect(await screen.findByText(/2026-11-02 to 2026-11-04 · 2 days/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "File leave request" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: /Review leave for Synthetic Nurse/ }));
  const region = await screen.findByRole("region", { name: "Leave request" });
  expect(await within(region).findByText("2026-11-03 · holiday, not counted")).toBeInTheDocument();
  expect(within(region).getByText(/Synthetic Ward Roster · 2026-11-02/)).toBeInTheDocument();
  expect(within(region).queryByText(/Reason:/)).not.toBeInTheDocument();
  expect(await within(region).findByText(/by synthetic-filer/)).toBeInTheDocument();
  expect(within(region).queryByRole("form", { name: "Decide leave request" })).toBeNull();
});

it("files a request with only the unit's fields and replays the same id on retry", async () => {
  const { writes } = mock({ reject: "This request id was already used with different values." });
  const { user } = renderWithProviders(<LeaveRequests />);
  await user.click(await screen.findByRole("button", { name: "File leave request" }));
  const form = screen.getByRole("form", { name: "File leave request" });
  await user.click(within(form).getByRole("button", { name: "File leave request" }));
  expect(await within(form).findByText("Choose a leave type.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await chooseOption(user, within(form).getByRole("combobox", { name: "Staff member" }), /SYN-001/);
  await chooseOption(user, within(form).getByRole("combobox", { name: "Leave type" }), /SYN-CL/);
  await chooseOption(user, within(form).getByRole("combobox", { name: "Taken as" }), "Half day");
  await user.click(within(form).getByRole("button", { name: "File leave request" }));
  expect(
    await within(form).findByText("This leave type cannot be taken as a half day."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await chooseOption(user, within(form).getByRole("combobox", { name: "Taken as" }), "Short leave");
  const minutes = within(form).getByLabelText("Minutes");
  await user.clear(minutes);
  await user.type(minutes, "45");
  const date = within(form).getByLabelText("Date");
  await user.clear(date);
  await user.type(date, "2026-11-09");
  await user.click(within(form).getByRole("button", { name: "File leave request" }));
  expect(await within(form).findByRole("alert")).toHaveTextContent("input is preserved");
  await user.click(within(form).getByRole("button", { name: "File leave request" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0].url).toMatch(
    /\/staff\/22222222-2222-4222-8222-222222222222\/leave-requests\/[0-9a-f-]{36}$/,
  );
  expect(writes[1].url).toBe(writes[0].url);
  expect(writes[0]).toMatchObject({ method: "PUT" });
  expect(writes[0].body).toEqual({
    leave_type_id: casual,
    unit: "short_leave",
    start_on: "2026-11-09",
    end_on: "2026-11-09",
    minutes: 45,
    reason: "",
  });
});

it("lets an approver approve a pending request at its version", async () => {
  const { writes } = mock();
  const { user } = renderWithProviders(<LeaveRequests />);
  await user.click(await screen.findByRole("button", { name: /Review leave for Synthetic Nurse/ }));
  const region = await screen.findByRole("region", { name: "Leave request" });
  expect(await within(region).findByText("Reason: Synthetic family event")).toBeInTheDocument();
  await user.type(within(region).getByLabelText(/^Note/), "Cover arranged");
  await user.click(within(region).getByRole("button", { name: "Approve leave" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "POST",
    url: expect.stringContaining(`/staff/${staffID}/leave-requests/${requestID}/decision`),
    body: { expected_version: 1, decision: "approve", note: "Cover arranged" },
  });
});

it("keeps a refused decision visible with the input preserved", async () => {
  const { writes } = mock({ reject: "This leave request changed since you opened it." });
  const { user } = renderWithProviders(<LeaveRequests />);
  await user.click(await screen.findByRole("button", { name: /Review leave for Synthetic Nurse/ }));
  const region = await screen.findByRole("region", { name: "Leave request" });
  await user.click(await within(region).findByRole("button", { name: "Reject leave" }));
  expect(
    await within(region).findByText("Add a note explaining the decision."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.type(within(region).getByLabelText(/^Note/), "Ward short-staffed");
  await user.click(within(region).getByRole("button", { name: "Reject leave" }));
  expect(await within(region).findByRole("alert")).toHaveTextContent("changed since you opened it");
  expect(writes[0].body).toEqual({
    decision: "reject",
    expected_version: 1,
    note: "Ward short-staffed",
  });
});

it("lets a filer without approval only withdraw a pending request", async () => {
  const { writes } = mock({ permissions: ["workforce.leave.request"] });
  const { user } = renderWithProviders(<LeaveRequests />);
  await user.click(await screen.findByRole("button", { name: /Review leave for Synthetic Nurse/ }));
  const region = await screen.findByRole("region", { name: "Leave request" });
  const withdraw = await within(region).findByRole("button", { name: "Withdraw request" });
  expect(within(region).queryByRole("button", { name: "Approve leave" })).toBeNull();
  expect(within(region).queryByRole("button", { name: "Reject leave" })).toBeNull();
  await user.click(withdraw);
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "POST",
    url: expect.stringContaining(`/leave-requests/${requestID}/cancellation`),
    body: { expected_version: 1, note: "" },
  });
});

it("confirms before cancelling approved leave", async () => {
  const { writes } = mock({ status: "approved" });
  const { user } = renderWithProviders(<LeaveRequests />);
  await chooseOption(user, await screen.findByRole("combobox", { name: "Status" }), "Approved");
  await user.click(await screen.findByRole("button", { name: /Review leave for Synthetic Nurse/ }));
  const region = await screen.findByRole("region", { name: "Leave request" });
  await user.type(await within(region).findByLabelText(/^Note/), "Recalled for duty");
  await user.click(within(region).getByRole("button", { name: "Cancel approved leave" }));
  const dialog = await screen.findByRole("dialog");
  expect(writes).toHaveLength(0);
  await user.click(within(dialog).getByRole("button", { name: "Cancel approved leave" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "POST",
    url: expect.stringContaining(`/leave-requests/${requestID}/revocation`),
    body: { expected_version: 2, note: "Recalled for duty" },
  });
});
