import "@/test/select";
import { screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "@/test/select";
import LeaveReports from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const staffID = "22222222-2222-4222-8222-222222222222";
const casual = "33333333-3333-4333-8333-333333333333";
const sick = "44444444-4444-4444-8444-444444444444";
const ward = "55555555-5555-4555-8555-555555555555";
const stamp = "2026-10-15T00:00:00Z";
const profile = { department: "Synthetic Ward", joined_on: "2025-04-01" };
const monthly = (profiles: boolean) => ({
  staff_id: staffID,
  employee_code: "SYN-001",
  display_name: "Synthetic Nurse",
  ...(profiles && profile),
  months: [
    {
      month: "2026-10",
      leave_dates: ["2026-10-01", "2026-10-03"],
      types: [
        { leave_type_id: sick, code: "SYN-SL", name: "Synthetic Sick", half_days: 1, minutes: 60 },
        {
          leave_type_id: casual,
          code: "SYN-CL",
          name: "Synthetic Casual",
          half_days: 2,
          minutes: 0,
        },
      ],
    },
  ],
});
const status = (profiles: boolean) => ({
  id: "66666666-6666-4666-8666-666666666666",
  staff_id: staffID,
  employee_code: "SYN-001",
  display_name: "Synthetic Nurse",
  ...(profiles && profile),
  leave_type_id: casual,
  code: "SYN-CL",
  name: "Synthetic Casual",
  unit: "full_day",
  start_on: "2026-10-01",
  end_on: "2026-10-03",
  half_days: 4,
  status: "pending",
  requested_by_subject: "synthetic-filer",
  created_at: stamp,
});
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());

function mock({
  permissions = [] as string[],
  rows = true,
  fail = false,
}: { permissions?: string[]; rows?: boolean; fail?: boolean } = {}) {
  const reads: string[] = [];
  const profiles = permissions.includes("workforce.enrollment.manage");
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      let code = 200;
      let body: unknown = page([]);
      if (url.includes("permissions")) body = { data: permissions };
      else if (url.endsWith("/hospitals"))
        body = page([{ id: hospital, name: "Synthetic Hospital", access_level: "manager" }]);
      else if (url.includes("/hr/leave-reports/")) {
        reads.push(url);
        const row = url.includes("/monthly?") ? monthly(profiles) : status(profiles);
        [code, body] = fail
          ? [500, { error: "internal", message: "boom" }]
          : [200, page(rows ? [row] : [])];
      } else if (url.includes("kind=department"))
        body = page([{ id: ward, kind: "department", code: "SYN-WARD", name: "Synthetic Ward" }]);
      else if (url.includes("kind=leave_type"))
        body = page([{ id: casual, kind: "leave_type", code: "SYN-CL", name: "Synthetic Casual" }]);
      return new Response(JSON.stringify(body), { status: code });
    }),
  );
  return reads;
}

async function run(user: ReturnType<typeof renderWithProviders>["user"], from: string, to: string) {
  const form = await screen.findByRole("form", { name: "Report filters" });
  for (const [label, value] of [
    ["From", from],
    ["To", to],
  ]) {
    const input = within(form).getByLabelText(label);
    await user.clear(input);
    await user.type(input, value);
  }
  await user.click(within(form).getByRole("button", { name: "Run report" }));
  return form;
}

it("shows leave by month with a column per type and no profile data for other roles", async () => {
  const reads = mock();
  const { user } = renderWithProviders(<LeaveReports />);
  expect(await screen.findByText("Choose a period and run a report.")).toBeInTheDocument();
  const form = await run(user, "2026-10-01", "2026-10-31");
  expect(within(form).queryByRole("combobox", { name: "Department" })).toBeNull();
  const table = await screen.findByRole("table");
  expect(reads).toEqual([
    `/api/v1/workforce/hospitals/${hospital}/hr/leave-reports/monthly?from=2026-10-01&to=2026-10-31`,
  ]);
  const headers = within(table)
    .getAllByRole("columnheader")
    .map((h) => h.textContent);
  expect(headers).toEqual(["Staff", "Month", "Leave dates", "SYN-CL", "SYN-SL"]);
  expect(within(table).getByText("2026-10-01, 2026-10-03")).toBeInTheDocument();
  expect(within(table).getByText("1 day")).toBeInTheDocument();
  expect(within(table).getByText("0.5 days · 60 min")).toBeInTheDocument();
});

it("lets enrollment managers filter by department and see profile columns", async () => {
  const reads = mock({ permissions: ["workforce.enrollment.manage"] });
  const { user } = renderWithProviders(<LeaveReports />);
  const form = await screen.findByRole("form", { name: "Report filters" });
  await chooseOption(
    user,
    await within(form).findByRole("combobox", { name: "Department" }),
    "Synthetic Ward",
  );
  await user.type(within(form).getByLabelText("Name or employee code"), "syn_%");
  await run(user, "2026-10-01", "2026-10-31");
  const table = await screen.findByRole("table");
  expect(reads.at(-1)).toBe(
    `/api/v1/workforce/hospitals/${hospital}/hr/leave-reports/monthly?from=2026-10-01&to=2026-10-31&q=syn_%25&department_id=${ward}`,
  );
  expect(within(table).getByRole("columnheader", { name: "Department" })).toBeInTheDocument();
  expect(within(table).getByText("Synthetic Ward")).toBeInTheDocument();
  expect(within(table).getByText("2025-04-01")).toBeInTheDocument();
});

it("runs the status report with its own filters", async () => {
  const reads = mock();
  const { user } = renderWithProviders(<LeaveReports />);
  const form = await screen.findByRole("form", { name: "Report filters" });
  await chooseOption(user, within(form).getByRole("combobox", { name: "Report" }), "Leave status");
  await chooseOption(user, within(form).getByRole("combobox", { name: "Status" }), "Pending");
  await chooseOption(user, within(form).getByRole("combobox", { name: "Leave type" }), /SYN-CL/);
  await run(user, "2026-10-01", "2026-10-31");
  const table = await screen.findByRole("table");
  expect(reads.at(-1)).toBe(
    `/api/v1/workforce/hospitals/${hospital}/hr/leave-reports/status?from=2026-10-01&to=2026-10-31&leave_type_id=${casual}&status=pending`,
  );
  const row = within(table).getAllByRole("row")[1];
  expect(within(row).getByText("2 days")).toBeInTheDocument();
  expect(within(row).getByText("synthetic-filer")).toBeInTheDocument();
  expect(within(row).getByText("—")).toBeInTheDocument();
  expect(within(row).getByText("Pending")).toBeInTheDocument();
  expect(within(table).queryByRole("columnheader", { name: "Department" })).toBeNull();
});

it("checks the period before asking the API", async () => {
  const reads = mock();
  const { user } = renderWithProviders(<LeaveReports />);
  const form = await run(user, "2026-10-31", "2026-10-01");
  expect(
    await within(form).findByText("End the period on or after its start."),
  ).toBeInTheDocument();
  await run(user, "2025-01-01", "2026-01-02");
  expect(await within(form).findByText("Keep the period within 366 days.")).toBeInTheDocument();
  expect(reads).toHaveLength(0);
});

it("explains empty and failed reports", async () => {
  mock({ rows: false });
  const { user } = renderWithProviders(<LeaveReports />);
  await run(user, "2026-03-01", "2026-03-31");
  expect(await screen.findByText("No leave matches these filters.")).toBeInTheDocument();
});

it("offers a retry when a report cannot be loaded", async () => {
  mock({ fail: true });
  const { user } = renderWithProviders(<LeaveReports />);
  await run(user, "2026-10-01", "2026-10-31");
  expect(await screen.findByText("Could not load the report.")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
});
