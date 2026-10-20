import "@/test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "@/test/select";
import DutyExchanges from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const nurseA = "22222222-2222-4222-8222-222222222222";
const nurseB = "33333333-3333-4333-8333-333333333333";
const exchangeID = "44444444-4444-4444-8444-444444444444";
const roster = "55555555-5555-4555-8555-555555555555";
const dutyA = "66666666-6666-4666-8666-666666666666";
const dutyB = "77777777-7777-4777-8777-777777777777";
const pastDuty = "88888888-8888-4888-8888-888888888888";
const stamp = "2026-10-16T00:00:00Z";
const exchange = (status = "pending", reason = "Synthetic family wedding") => ({
  id: exchangeID,
  kind: "swap",
  requester_staff_id: nurseA,
  requester_employee_code: "SYN-001",
  requester_display_name: "Synthetic Nurse",
  counterpart_staff_id: nurseB,
  counterpart_employee_code: "SYN-002",
  counterpart_display_name: "Synthetic Charge",
  requester_allocation_id: dutyA,
  counterpart_allocation_id: dutyB,
  duty_from: "2099-03-02",
  duty_to: "2099-03-02",
  counterpart_consent: true,
  reason,
  status,
  requested_by_subject: "synthetic-filer",
  version: status === "pending" ? 1 : 2,
  created_at: stamp,
  updated_at: stamp,
});
const duty = (id: string, staff: string, name: string, day: string, extra = {}) => ({
  id,
  roster_id: roster,
  roster_name: "Synthetic Ward Roster",
  roster_status: "frozen",
  staff_id: staff,
  employee_code: staff === nurseA ? "SYN-001" : "SYN-002",
  display_name: name,
  roster_date: day,
  shift_code: "SYN-DAY",
  shift_name: "Synthetic Day",
  start_time: "08:00",
  end_time: "16:00",
  area_code: "SYN-WARD",
  area_name: "Synthetic Ward",
  starts_at: `${day}T08:00:00Z`,
  ends_at: `${day}T16:00:00Z`,
  additional: false,
  active: true,
  ...extra,
});
const person = (id: string, code: string, name: string) => ({
  id,
  hospital_id: hospital,
  employee_code: code,
  display_name: name,
  job_category: "nurse",
  employment_status: "active",
  created_at: stamp,
  updated_at: stamp,
});
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
function mock({
  permissions = ["workforce.duty.request", "workforce.duty.approve"],
  status = "pending",
  reason = "Synthetic family wedding",
  exchanges = true,
  fail = false,
  reject = "",
}: {
  permissions?: string[];
  status?: string;
  reason?: string;
  exchanges?: boolean;
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
      else if (url.endsWith("/history"))
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
      else if (url.endsWith(`/duty-exchanges/${exchangeID}`))
        body = {
          exchange: exchange(status, reason),
          allocations: [
            duty(dutyA, nurseA, "Synthetic Nurse", "2099-03-02"),
            duty(dutyB, nurseB, "Synthetic Charge", "2099-03-02", { active: status === "pending" }),
          ],
        };
      else if (url.includes("/hr/duty-exchanges")) {
        reads.push(url);
        [code, body] = fail
          ? [500, { error: "internal", message: "boom" }]
          : [200, page(exchanges ? [exchange(status, reason)] : [])];
      } else if (url.includes(`/allocations?staff_id=${nurseA}`))
        body = page([
          duty(dutyA, nurseA, "Synthetic Nurse", "2099-03-02"),
          duty(pastDuty, nurseA, "Synthetic Nurse", "2000-03-02"),
        ]);
      else if (url.includes(`/allocations?staff_id=${nurseB}`))
        body = page([duty(dutyB, nurseB, "Synthetic Charge", "2099-03-03")]);
      else if (url.endsWith("/hr/rosters"))
        body = page([
          {
            id: roster,
            name: "Synthetic Ward Roster",
            status: "frozen",
            start_date: "2099-03-01",
            end_date: "2099-03-14",
          },
          {
            id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
            name: "Synthetic Draft Roster",
            status: "draft",
            start_date: "2099-04-01",
            end_date: "2099-04-14",
          },
        ]);
      else if (url.endsWith("/staff"))
        body = page([
          person(nurseA, "SYN-001", "Synthetic Nurse"),
          person(nurseB, "SYN-002", "Synthetic Charge"),
        ]);
      if (init?.method === "PUT" || init?.method === "POST") {
        writes.push({ method: init.method, url, body: JSON.parse(String(init.body)) });
        [code, body] = reject
          ? [409, { error: "conflict", message: reject }]
          : [200, { exchange: exchange("approved"), allocations: [] }];
      }
      return new Response(JSON.stringify(body), { status: code });
    }),
  );
  return { writes, reads };
}

it("shows an empty pending queue and lists every status on request", async () => {
  const { reads } = mock({ exchanges: false });
  const { user } = renderWithProviders(<DutyExchanges />);
  expect(await screen.findByText("No pending duty exchanges.")).toBeInTheDocument();
  expect(reads.at(-1)).toContain("/hr/duty-exchanges?status=pending");
  await chooseOption(user, screen.getByRole("combobox", { name: "Status" }), "All statuses");
  expect(
    await screen.findByText("No duty exchanges have been filed in this hospital."),
  ).toBeInTheDocument();
  expect(reads.at(-1)).toMatch(/\/hr\/duty-exchanges$/);
});

it("offers a retry when exchanges cannot be loaded", async () => {
  mock({ fail: true });
  renderWithProviders(<DutyExchanges />);
  expect(await screen.findByText("Could not load duty exchanges.")).toBeInTheDocument();
});

it("shows an exchange read-only, with duties and history, to readers", async () => {
  mock({ permissions: [], reason: "" });
  const { user } = renderWithProviders(<DutyExchanges />);
  expect(await screen.findByText("Synthetic Nurse → Synthetic Charge")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "File duty exchange" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: /Review exchange for Synthetic Nurse/ }));
  const region = await screen.findByRole("region", { name: "Duty exchange" });
  expect(
    await within(region).findByText(/Synthetic Charge · 2099-03-02 · SYN-DAY/),
  ).toBeInTheDocument();
  expect(within(region).queryByText(/Reason:/)).not.toBeInTheDocument();
  expect(await within(region).findByText(/by synthetic-filer/)).toBeInTheDocument();
  expect(within(region).queryByRole("form", { name: "Decide duty exchange" })).toBeNull();
});

it("files a swap from future duties only and replays the same id on retry", async () => {
  const { writes } = mock({ reject: "One of these duties has already started." });
  const { user } = renderWithProviders(<DutyExchanges />);
  await user.click(await screen.findByRole("button", { name: "File duty exchange" }));
  const form = screen.getByRole("form", { name: "File duty exchange" });
  await user.click(within(form).getByRole("button", { name: "File duty exchange" }));
  expect(await within(form).findByText("Choose a published roster.")).toBeInTheDocument();
  expect(within(form).getByText("Add a reason for the exchange.")).toBeInTheDocument();
  expect(
    within(form).getByText("Confirm the other person has agreed to the exchange."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  const rosterBox = within(form).getByRole("combobox", { name: "Published roster" });
  await user.click(rosterBox);
  expect(screen.queryByRole("option", { name: /Synthetic Draft Roster/ })).toBeNull();
  await user.keyboard("{Escape}");
  await chooseOption(user, rosterBox, /Synthetic Ward Roster/);
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Gives up the duty" }),
    /SYN-001/,
  );
  const given = within(form).getByRole("combobox", { name: "Duty handed over" });
  await user.click(given);
  expect(await screen.findByRole("option", { name: /2099-03-02/ })).toBeInTheDocument();
  expect(screen.queryByRole("option", { name: /2000-03-02/ })).toBeNull();
  await user.keyboard("{Escape}");
  await chooseOption(user, given, /2099-03-02/);
  await chooseOption(user, within(form).getByRole("combobox", { name: "Swaps with" }), /SYN-001/);
  await user.type(within(form).getByLabelText("Reason"), "Synthetic family wedding");
  await user.click(within(form).getByRole("checkbox"));
  await user.click(within(form).getByRole("button", { name: "File duty exchange" }));
  expect(
    await within(form).findByText("Choose someone other than the person giving up the duty."),
  ).toBeInTheDocument();
  await chooseOption(user, within(form).getByRole("combobox", { name: "Swaps with" }), /SYN-002/);
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Duty given in return" }),
    /2099-03-03/,
  );
  await user.click(within(form).getByRole("button", { name: "File duty exchange" }));
  expect(await within(form).findByRole("alert")).toHaveTextContent("already started");
  await user.click(within(form).getByRole("button", { name: "File duty exchange" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0].url).toMatch(/\/hr\/duty-exchanges\/[0-9a-f-]{36}$/);
  expect(writes[1].url).toBe(writes[0].url);
  expect(writes[0]).toMatchObject({ method: "PUT" });
  expect(writes[0].body).toEqual({
    kind: "swap",
    requester_allocation_id: dutyA,
    counterpart_staff_id: nurseB,
    counterpart_allocation_id: dutyB,
    counterpart_consent: true,
    reason: "Synthetic family wedding",
  });
});

it("lets an approver approve a pending exchange at its version", async () => {
  const { writes } = mock();
  const { user } = renderWithProviders(<DutyExchanges />);
  await user.click(
    await screen.findByRole("button", { name: /Review exchange for Synthetic Nurse/ }),
  );
  const region = await screen.findByRole("region", { name: "Duty exchange" });
  expect(await within(region).findByText("Reason: Synthetic family wedding")).toBeInTheDocument();
  await user.type(within(region).getByLabelText(/^Note/), "Both agreed");
  await user.click(within(region).getByRole("button", { name: "Approve exchange" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "POST",
    url: expect.stringContaining(`/hr/duty-exchanges/${exchangeID}/decision`),
    body: { expected_version: 1, decision: "approve", note: "Both agreed" },
  });
});

it("keeps a refused decision visible with the input preserved", async () => {
  const { writes } = mock({ reject: "This exchange changed since you opened it." });
  const { user } = renderWithProviders(<DutyExchanges />);
  await user.click(
    await screen.findByRole("button", { name: /Review exchange for Synthetic Nurse/ }),
  );
  const region = await screen.findByRole("region", { name: "Duty exchange" });
  await user.click(await within(region).findByRole("button", { name: "Reject exchange" }));
  expect(
    await within(region).findByText("Add a note explaining the decision."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.type(within(region).getByLabelText(/^Note/), "Ward short-staffed");
  await user.click(within(region).getByRole("button", { name: "Reject exchange" }));
  expect(await within(region).findByRole("alert")).toHaveTextContent("changed since you opened it");
  expect(writes[0].body).toEqual({
    decision: "reject",
    expected_version: 1,
    note: "Ward short-staffed",
  });
});

it("lets a filer without approval only withdraw a pending exchange", async () => {
  const { writes } = mock({ permissions: ["workforce.duty.request"] });
  const { user } = renderWithProviders(<DutyExchanges />);
  await user.click(
    await screen.findByRole("button", { name: /Review exchange for Synthetic Nurse/ }),
  );
  const region = await screen.findByRole("region", { name: "Duty exchange" });
  const withdraw = await within(region).findByRole("button", { name: "Withdraw exchange" });
  expect(within(region).queryByRole("button", { name: "Approve exchange" })).toBeNull();
  expect(within(region).queryByRole("button", { name: "Reject exchange" })).toBeNull();
  await user.click(withdraw);
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "POST",
    url: expect.stringContaining(`/duty-exchanges/${exchangeID}/cancellation`),
    body: { expected_version: 1, note: "" },
  });
});

it("confirms before revoking an approved exchange", async () => {
  const { writes } = mock({ status: "approved" });
  const { user } = renderWithProviders(<DutyExchanges />);
  await chooseOption(user, await screen.findByRole("combobox", { name: "Status" }), "Approved");
  await user.click(
    await screen.findByRole("button", { name: /Review exchange for Synthetic Nurse/ }),
  );
  const region = await screen.findByRole("region", { name: "Duty exchange" });
  expect(await within(region).findByText(/no longer held/)).toBeInTheDocument();
  await user.type(await within(region).findByLabelText(/^Note/), "Course moved");
  await user.click(within(region).getByRole("button", { name: "Revoke exchange" }));
  const dialog = await screen.findByRole("dialog");
  expect(writes).toHaveLength(0);
  await user.click(within(dialog).getByRole("button", { name: "Revoke exchange" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "POST",
    url: expect.stringContaining(`/duty-exchanges/${exchangeID}/revocation`),
    body: { expected_version: 2, note: "Course moved" },
  });
});
