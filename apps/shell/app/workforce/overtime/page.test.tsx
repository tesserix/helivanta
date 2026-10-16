import "../../../test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../../test/select";
import Overtime from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const nurse = "22222222-2222-4222-8222-222222222222";
const requestID = "44444444-4444-4444-8444-444444444444";
const revision = "55555555-5555-4555-8555-555555555555";
const stamp = "2026-10-16T00:00:00Z";
const settings = {
  revision_id: revision,
  daily_limit_minutes: 240,
  weekly_limit_minutes: 720,
  monthly_limit_minutes: 2400,
  filing_window_days: 7,
  effective_on: "2026-11-01",
  reviewed_by_subject: "synthetic-manager",
  version: 1,
  created_at: stamp,
  updated_at: stamp,
};
const overtime = (status = "pending", reason = "Synthetic theatre overrun") => ({
  id: requestID,
  staff_id: nurse,
  employee_code: "SYN-001",
  display_name: "Synthetic Nurse",
  work_date: "2026-11-02",
  start_time: "18:00",
  end_time: "20:30",
  timezone: "Asia/Kolkata",
  starts_at: "2026-11-02T12:30:00Z",
  ends_at: "2026-11-02T15:00:00Z",
  requested_minutes: 150,
  ...(status === "approved" || status === "revoked" ? { approved_minutes: 120 } : {}),
  payable_minutes: status === "approved" ? 120 : 0,
  reason,
  status,
  requested_by_subject: "synthetic-filer",
  version: status === "pending" ? 1 : 2,
  created_at: stamp,
  updated_at: stamp,
});
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
function mock({
  permissions = [
    "workforce.overtime.request",
    "workforce.overtime.approve",
    "workforce.overtime.configure",
  ],
  status = "pending",
  reason = "Synthetic theatre overrun",
  requests = true,
  policy = true,
  fail = false,
  reject = "",
}: {
  permissions?: string[];
  status?: string;
  reason?: string;
  requests?: boolean;
  policy?: boolean;
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
      else if (url.endsWith("/overtime-settings/history")) body = page([settings]);
      else if (url.endsWith("/overtime-settings"))
        [code, body] = policy
          ? [200, settings]
          : [404, { error: "not_found", message: "overtime settings not found" }];
      else if (url.includes("/hr/overtime-summary")) {
        reads.push(url);
        body = page(
          requests
            ? [
                {
                  staff_id: nurse,
                  employee_code: "SYN-001",
                  display_name: "Synthetic Nurse",
                  pending_minutes: 30,
                  approved_requested_minutes: 150,
                  payable_minutes: 120,
                },
              ]
            : [],
        );
      } else if (url.endsWith(`/overtime/${requestID}/history`))
        body = page([
          {
            id: "99999999-9999-4999-8999-999999999999",
            version: 1,
            status: "pending",
            note: "",
            actor_subject: "synthetic-filer",
            created_at: stamp,
          },
        ]);
      else if (url.endsWith(`/overtime/${requestID}`)) body = overtime(status, reason);
      else if (url.includes("/hr/overtime")) {
        reads.push(url);
        [code, body] = fail
          ? [500, { error: "internal", message: "boom" }]
          : [200, page(requests ? [overtime(status, reason)] : [])];
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
          : [200, url.endsWith("/overtime-settings") ? settings : overtime("approved")];
      }
      return new Response(JSON.stringify(body), { status: code });
    }),
  );
  return { writes, reads };
}

const review = async (user: ReturnType<typeof renderWithProviders>["user"]) => {
  await user.click(
    await screen.findByRole("button", { name: /Review overtime for Synthetic Nurse/ }),
  );
  return screen.findByRole("region", { name: "Overtime request" });
};

it("shows empty queues, a missing policy and every status on request", async () => {
  const { reads } = mock({ requests: false, policy: false });
  const { user } = renderWithProviders(<Overtime />);
  expect(await screen.findByText("No pending overtime.")).toBeInTheDocument();
  expect(await screen.findByText(/No overtime policy has been reviewed/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Configure overtime policy" })).toBeInTheDocument();
  expect(await screen.findByText(/No overtime was recorded in/)).toBeInTheDocument();
  expect(reads.find((r) => r.includes("/hr/overtime?"))).toContain("status=pending");
  await chooseOption(user, screen.getByRole("combobox", { name: "Status" }), "All statuses");
  expect(
    await screen.findByText("No overtime has been recorded in this hospital."),
  ).toBeInTheDocument();
  expect(reads.filter((r) => !r.includes("summary")).at(-1)).toMatch(/\/hr\/overtime$/);
});

it("offers a retry when overtime cannot be loaded", async () => {
  mock({ fail: true });
  renderWithProviders(<Overtime />);
  expect(await screen.findByText("Could not load overtime requests.")).toBeInTheDocument();
});

it("shows policy, requests and payable minutes read-only to readers", async () => {
  mock({ permissions: [], reason: "" });
  const { user } = renderWithProviders(<Overtime />);
  const policy = await screen.findByRole("region", { name: "Overtime policy" });
  expect(await within(policy).findByText("4 h")).toBeInTheDocument();
  expect(within(policy).getByText("7 days")).toBeInTheDocument();
  expect(within(policy).getByText(/Read-only access/)).toBeInTheDocument();
  expect(within(policy).queryByRole("button", { name: /overtime policy$/ })).toBeNull();
  expect(screen.queryByRole("button", { name: "Record overtime" })).not.toBeInTheDocument();
  const summary = screen.getByRole("region", { name: "Overtime by month" });
  const row = (await within(summary).findByText("Synthetic Nurse")).closest("tr")!;
  expect(within(row).getByText("30 min")).toBeInTheDocument();
  expect(within(row).getByText("2 h")).toBeInTheDocument();
  const region = await review(user);
  expect(await within(region).findByText(/2026-11-02 · 18:00–20:30/)).toBeInTheDocument();
  expect(within(region).queryByText(/Reason:/)).not.toBeInTheDocument();
  expect(await within(region).findByText(/by synthetic-filer/)).toBeInTheDocument();
  expect(within(region).queryByRole("form", { name: "Decide overtime" })).toBeNull();
});

it("refuses limits that do not nest and saves a reviewed policy", async () => {
  const { writes } = mock();
  const { user } = renderWithProviders(<Overtime />);
  await user.click(await screen.findByRole("button", { name: "Edit overtime policy" }));
  const form = screen.getByRole("form", { name: "Edit overtime policy" });
  const weekly = within(form).getByLabelText("Weekly limit (minutes)");
  await user.clear(weekly);
  await user.type(weekly, "200");
  await user.click(within(form).getByRole("button", { name: "Save overtime policy" }));
  expect(
    await within(form).findByText("Allow at least the daily limit each week."),
  ).toBeInTheDocument();
  expect(within(form).getByText("Confirm that you reviewed the policy.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.clear(weekly);
  await user.type(weekly, "600");
  await user.click(within(form).getByRole("checkbox"));
  await user.click(within(form).getByRole("button", { name: "Save overtime policy" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "PUT",
    url: expect.stringMatching(/\/hr\/overtime-settings$/),
    body: {
      expected_version: 1,
      daily_limit_minutes: 240,
      weekly_limit_minutes: 600,
      monthly_limit_minutes: 2400,
      filing_window_days: 7,
      effective_on: "2026-11-01",
      reviewed: true,
    },
  });
});

it("records overtime past midnight and replays the same id on retry", async () => {
  const { writes } = mock({ reject: "This overtime overlaps the person's rostered duty." });
  const { user } = renderWithProviders(<Overtime />);
  await user.click(await screen.findByRole("button", { name: "Record overtime" }));
  const form = screen.getByRole("form", { name: "Record overtime" });
  await user.click(within(form).getByRole("button", { name: "Record overtime" }));
  expect(await within(form).findByText("Choose a staff member.")).toBeInTheDocument();
  expect(within(form).getByText("Add a reason for the overtime.")).toBeInTheDocument();
  expect(within(form).getAllByText("Use a 24-hour time such as 18:30.")).toHaveLength(2);
  expect(writes).toHaveLength(0);
  await chooseOption(user, within(form).getByRole("combobox", { name: "Staff member" }), /SYN-001/);
  const date = within(form).getByLabelText("Work date");
  await user.clear(date);
  await user.type(date, "2026-11-02");
  await user.type(within(form).getByLabelText("Start (24-hour)"), "22:00");
  await user.type(within(form).getByLabelText("End (24-hour)"), "22:10");
  await user.type(within(form).getByLabelText("Reason"), "Synthetic theatre overrun");
  await user.click(within(form).getByRole("button", { name: "Record overtime" }));
  expect(
    await within(form).findByText("Record at least 15 minutes of overtime."),
  ).toBeInTheDocument();
  const end = within(form).getByLabelText("End (24-hour)");
  await user.clear(end);
  await user.type(end, "01:30");
  expect(within(form).getByText(/3 h 30 min, ending the next day/)).toBeInTheDocument();
  await user.click(within(form).getByRole("button", { name: "Record overtime" }));
  expect(await within(form).findByRole("alert")).toHaveTextContent("overlaps");
  await user.click(within(form).getByRole("button", { name: "Record overtime" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0].url).toMatch(/\/hr\/overtime\/[0-9a-f-]{36}$/);
  expect(writes[1].url).toBe(writes[0].url);
  expect(writes[0]).toMatchObject({ method: "PUT" });
  expect(writes[0].body).toEqual({
    staff_id: nurse,
    work_date: "2026-11-02",
    start_time: "22:00",
    end_time: "01:30",
    reason: "Synthetic theatre overrun",
  });
});

it("requires a note to approve fewer minutes and sends them at the request version", async () => {
  const { writes } = mock();
  const { user } = renderWithProviders(<Overtime />);
  const region = await review(user);
  expect(await within(region).findByText("Reason: Synthetic theatre overrun")).toBeInTheDocument();
  const minutes = within(region).getByLabelText("Minutes to approve (of 150)");
  await user.clear(minutes);
  await user.type(minutes, "151");
  await user.click(within(region).getByRole("button", { name: "Approve overtime" }));
  expect(
    await within(region).findByText("Approve no more than the 150 minutes requested."),
  ).toBeInTheDocument();
  await user.clear(minutes);
  await user.type(minutes, "120");
  await user.click(within(region).getByRole("button", { name: "Approve overtime" }));
  expect(
    await within(region).findByText("Add a note explaining why fewer minutes are approved."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.type(within(region).getByLabelText(/^Note/), "Theatre closed at 20:00");
  await user.click(within(region).getByRole("button", { name: "Approve overtime" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "POST",
    url: expect.stringContaining(`/hr/overtime/${requestID}/decision`),
    body: {
      expected_version: 1,
      decision: "approve",
      approved_minutes: 120,
      note: "Theatre closed at 20:00",
    },
  });
});

it("keeps a refused rejection visible with the input preserved", async () => {
  const { writes } = mock({ reject: "This overtime changed since you opened it." });
  const { user } = renderWithProviders(<Overtime />);
  const region = await review(user);
  await user.click(await within(region).findByRole("button", { name: "Reject overtime" }));
  expect(
    await within(region).findByText("Add a note explaining the decision."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.type(within(region).getByLabelText(/^Note/), "Not pre-agreed");
  await user.click(within(region).getByRole("button", { name: "Reject overtime" }));
  expect(await within(region).findByRole("alert")).toHaveTextContent("changed since you opened it");
  expect(writes[0].body).toEqual({
    expected_version: 1,
    decision: "reject",
    note: "Not pre-agreed",
  });
});

it("lets a filer without approval only withdraw pending overtime", async () => {
  const { writes } = mock({ permissions: ["workforce.overtime.request"] });
  const { user } = renderWithProviders(<Overtime />);
  const region = await review(user);
  const withdraw = await within(region).findByRole("button", { name: "Withdraw overtime" });
  expect(within(region).queryByRole("button", { name: "Approve overtime" })).toBeNull();
  expect(within(region).queryByLabelText(/Minutes to approve/)).toBeNull();
  await user.click(withdraw);
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "POST",
    url: expect.stringContaining(`/overtime/${requestID}/cancellation`),
    body: { expected_version: 1, note: "" },
  });
});

it("confirms before revoking approved overtime", async () => {
  const { writes } = mock({ status: "approved" });
  const { user } = renderWithProviders(<Overtime />);
  await chooseOption(user, await screen.findByRole("combobox", { name: "Status" }), "Approved");
  const region = await review(user);
  expect(await within(region).findByText(/Approved 2 h · payable 2 h/)).toBeInTheDocument();
  await user.click(await within(region).findByRole("button", { name: "Revoke overtime" }));
  expect(
    await within(region).findByText("Add a note explaining the decision."),
  ).toBeInTheDocument();
  await user.type(within(region).getByLabelText(/^Note/), "Recorded against the wrong person");
  await user.click(within(region).getByRole("button", { name: "Revoke overtime" }));
  const dialog = await screen.findByRole("dialog");
  expect(writes).toHaveLength(0);
  await user.click(within(dialog).getByRole("button", { name: "Revoke overtime" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "POST",
    url: expect.stringContaining(`/overtime/${requestID}/revocation`),
    body: { expected_version: 2, note: "Recorded against the wrong person" },
  });
});
