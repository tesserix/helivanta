import "@/test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import LeaveBalances from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const staffID = "22222222-2222-4222-8222-222222222222";
const casual = "33333333-3333-4333-8333-333333333333";
const stamp = "2026-10-15T00:00:00Z";
const year = (posted: number, leaveYear = 2024) => ({
  id: "44444444-4444-4444-8444-444444444444",
  leave_year: leaveYear,
  start_on: `${leaveYear}-04-01`,
  end_on: `${leaveYear + 1}-03-31`,
  posted_months: posted,
  closed: false,
  version: 1,
  created_at: stamp,
  updated_at: stamp,
});
const balances = {
  staff_id: staffID,
  employee_code: "SYN-001",
  display_name: "Synthetic Nurse",
  structure_name: "Synthetic Structure",
  active: true,
  created_at: stamp,
  balances: [{ leave_type_id: casual, code: "SYN-CL", name: "Synthetic Casual", half_days: 6 }],
};
const entry = {
  id: "55555555-5555-4555-8555-555555555555",
  staff_id: staffID,
  leave_type_id: casual,
  leave_type_revision_id: "66666666-6666-4666-8666-666666666666",
  code: "SYN-CL",
  name: "Synthetic Casual",
  leave_year: 2024,
  kind: "accrual",
  half_days: 2,
  effective_on: "2024-04-01",
  reason: "",
  actor_subject: "synthetic-owner",
  created_at: stamp,
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());

it("closes the assignment editor when browser scrolling returns a promise", async () => {
  mock();
  const scroll = vi
    .spyOn(HTMLElement.prototype, "scrollIntoView")
    .mockImplementation(() => Promise.resolve() as never);
  try {
    const { user } = renderWithProviders(<LeaveBalances />);
    await user.click(await screen.findByRole("button", { name: "Assign leave structure" }));
    const editor = await screen.findByRole("form", { name: "Assign leave structure" });
    await user.click(within(editor).getByRole("button", { name: "Cancel changes" }));
    expect(screen.queryByRole("form", { name: "Assign leave structure" })).not.toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Leave structure assignments" }),
    ).toBeInTheDocument();
  } finally {
    scroll.mockRestore();
  }
});
function mock({
  permissions = [
    "workforce.leave.configure",
    "workforce.leave.allocate",
    "workforce.leave.correct",
  ],
  configured = true,
  posted = 3,
  leaveYear = 2024,
  reject = "",
}: {
  permissions?: string[];
  configured?: boolean;
  posted?: number;
  leaveYear?: number;
  reject?: string;
} = {}) {
  const writes: { method: string; url: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      let status = 200;
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
      else if (url.endsWith("/leave-settings"))
        [status, body] = configured
          ? [
              200,
              { year_start_month: 4, reviewed_by_subject: "synthetic-owner", created_at: stamp },
            ]
          : [404, { error: "not_found", message: "leave settings not found" }];
      else if (url.includes("/runs")) body = page([]);
      else if (url.endsWith("/leave-years"))
        body = page(configured ? [year(posted, leaveYear)] : []);
      else if (url.includes("leave-balances")) body = page([balances]);
      else if (url.includes("leave-ledger")) body = page([entry]);
      if (
        (init?.method === "PUT" || init?.method === "POST") &&
        url.includes("/workforce/hospitals/")
      ) {
        writes.push({
          method: init.method,
          url,
          body: init.body ? JSON.parse(String(init.body)) : undefined,
        });
        [status, body] = reject
          ? [409, { error: "conflict", message: reject }]
          : [200, { ...entry, id: "77777777-7777-4777-8777-777777777777" }];
      }
      return new Response(JSON.stringify(body), { status });
    }),
  );
  return writes;
}

it("sets the leave year only after review and preserves a rejected save", async () => {
  const writes = mock({ configured: false, reject: "The leave year start month is already set." });
  const { user } = renderWithProviders(<LeaveBalances />);
  await user.click(await screen.findByRole("button", { name: "Save leave year" }));
  expect(await screen.findByText("Confirm that you reviewed the change.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.click(screen.getByRole("combobox", { name: "Leave year starts in" }));
  await user.click(screen.getByRole("option", { name: "April" }));
  await user.click(screen.getByRole("checkbox", { name: /cannot change once saved/ }));
  await user.click(screen.getByRole("button", { name: "Save leave year" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("input is preserved");
  expect(writes).toEqual([
    {
      method: "PUT",
      url: expect.stringContaining(`/workforce/hospitals/${hospital}/hr/leave-settings`),
      body: { year_start_month: 4, reviewed: true },
    },
  ]);
  expect(screen.getByText("No leave year has been opened.")).toBeInTheDocument();
});

it("shows balances and the ledger read-only to readers", async () => {
  mock({ permissions: [] });
  const { user } = renderWithProviders(<LeaveBalances />);
  expect(await screen.findByText(/Leave years start on 1 April/)).toBeInTheDocument();
  expect(await screen.findByText("Synthetic Casual: 3 days")).toBeInTheDocument();
  expect(screen.getByText(/3 of 12 months posted/)).toBeInTheDocument();
  for (const name of ["Open leave year", "Post July", "Assign leave structure"])
    expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Ledger for Synthetic Nurse" }));
  const ledger = await screen.findByRole("region", { name: "Leave ledger" });
  expect(await within(ledger).findByText(/Monthly credit · \+1 day/)).toBeInTheDocument();
  expect(within(ledger).queryByRole("button", { name: "Correct balance" })).not.toBeInTheDocument();
});

it("posts the next month counted from the year start and confirms before closing", async () => {
  const writes = mock();
  const { user } = renderWithProviders(<LeaveBalances />);
  await user.click(await screen.findByRole("button", { name: "Post July" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "POST",
    url: expect.stringContaining("/hr/leave-years/2024/postings"),
    body: { month: 4 },
  });
  vi.unstubAllGlobals();
  const closes = mock({ posted: 12 });
  const { user: closer } = renderWithProviders(<LeaveBalances />);
  await closer.click(await screen.findByRole("button", { name: "Close leave year 2024" }));
  const dialog = await screen.findByRole("dialog");
  expect(closes).toHaveLength(0);
  await closer.click(within(dialog).getByRole("button", { name: "Close leave year" }));
  await waitFor(() => expect(closes).toHaveLength(1));
  expect(closes[0]).toMatchObject({
    method: "POST",
    url: expect.stringContaining("/hr/leave-years/2024/close"),
  });
});

it("offers no posting or closing before the period arrives", async () => {
  mock({ leaveYear: 2199, posted: 0 });
  renderWithProviders(<LeaveBalances />);
  expect(await screen.findByText(/0 of 12 months posted/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /^Post / })).not.toBeInTheDocument();
  vi.unstubAllGlobals();
  mock({ leaveYear: 2199, posted: 12 });
  renderWithProviders(<LeaveBalances />);
  expect(await screen.findAllByText(/12 of 12 months posted/)).not.toHaveLength(0);
  expect(screen.queryByRole("button", { name: /^Close leave year/ })).not.toBeInTheDocument();
});

it("validates a correction and replays the same entry id when a save is retried", async () => {
  const writes = mock({ reject: "This correction id was already used with different values." });
  const { user } = renderWithProviders(<LeaveBalances />);
  await user.click(await screen.findByRole("button", { name: "Ledger for Synthetic Nurse" }));
  await user.click(await screen.findByRole("button", { name: "Correct balance" }));
  const form = screen.getByRole("form", { name: "Correct balance for Synthetic Nurse" });
  await user.click(within(form).getByRole("button", { name: "Record correction" }));
  expect(await within(form).findByText("Enter a non-zero adjustment.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  const amount = within(form).getByLabelText(/Adjustment in half-days/);
  await user.clear(amount);
  await user.type(amount, "-3");
  await user.type(within(form).getByLabelText("Reason"), "Synthetic audit adjustment");
  await user.click(within(form).getByRole("button", { name: "Record correction" }));
  expect(await within(form).findByRole("alert")).toHaveTextContent("input is preserved");
  await user.click(within(form).getByRole("button", { name: "Record correction" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0].url).toMatch(
    /\/staff\/22222222-2222-4222-8222-222222222222\/leave-corrections\/[0-9a-f-]{36}$/,
  );
  expect(writes[1].url).toBe(writes[0].url);
  expect(writes[0].body).toEqual({
    leave_type_id: casual,
    leave_year: 2024,
    half_days: -3,
    effective_on: "2025-03-31",
    reason: "Synthetic audit adjustment",
  });
});
