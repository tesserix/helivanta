import "@/test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "@/test/select";
import LeaveSetup from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const earnedID = "22222222-2222-4222-8222-222222222222";
const earnedRevision = "33333333-3333-4333-8333-333333333333";
const unpaidID = "44444444-4444-4444-8444-444444444444";
const unpaidRevision = "55555555-5555-4555-8555-555555555555";
const record = {
  description: "",
  active: true,
  effective_on: "2026-01-01",
  reviewed_by_subject: "synthetic-owner",
  version: 1,
  created_at: "2026-10-15T00:00:00Z",
  updated_at: "2026-10-15T00:00:00Z",
  holidays: [],
  entries: [],
};
const rules = { paid: true, half_day_allowed: true, short_leave_minutes: 0, clubbable: false };
const earned = {
  ...record,
  id: earnedID,
  revision_id: earnedRevision,
  kind: "leave_type",
  code: "SYN-EARNED",
  name: "Synthetic Earned",
  rules: { ...rules, balance_tracked: true },
};
const unpaid = {
  ...earned,
  id: unpaidID,
  revision_id: unpaidRevision,
  code: "SYN-UNPAID",
  name: "Synthetic Unpaid",
  rules: { ...rules, paid: false, balance_tracked: false },
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
function mock({ allowed = true, status = 409 } = {}) {
  const writes: { url: string; body: Record<string, unknown> }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      let code = 200;
      let body: unknown = page([]);
      if (url.includes("permissions"))
        body = { data: allowed ? ["workforce.leave.configure"] : [] };
      else if (url.endsWith("/hospitals"))
        body = page([{ id: hospital, name: "Synthetic Hospital", access_level: "manager" }]);
      else if (url.includes("/history"))
        body = page([{ ...earned, id: earnedRevision, config_id: earnedID }]);
      else if (url.includes("kind=leave_type")) body = page([earned, unpaid]);
      if (init?.method === "PUT") {
        writes.push({ url, body: JSON.parse(String(init.body)) });
        code = status;
        body =
          status === 200
            ? { ...earned, ...JSON.parse(String(init.body)) }
            : { message: "Configuration changed. Reload before saving." };
      }
      return new Response(JSON.stringify(body), { status: code });
    }),
  );
  return writes;
}

it("requires review, keeps input on conflict and retries with the same ID", async () => {
  const writes = mock();
  const { user } = renderWithProviders(<LeaveSetup />);
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Earned" }));
  expect(screen.getByLabelText("Code")).toBeDisabled();
  expect(screen.getByRole("checkbox", { name: "Keep a balance for this leave" })).toBeChecked();
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  expect(
    await screen.findByText("Confirm that you reviewed the configuration."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.clear(screen.getByLabelText("Short leave limit (minutes, 0 for none)"));
  await user.type(screen.getByLabelText("Short leave limit (minutes, 0 for none)"), "20");
  await user.click(
    screen.getByRole("checkbox", { name: "I reviewed the effective date and leave rules" }),
  );
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  expect(await screen.findByText("Use steps of 15 minutes.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.clear(screen.getByLabelText("Short leave limit (minutes, 0 for none)"));
  await user.type(screen.getByLabelText("Short leave limit (minutes, 0 for none)"), "120");
  await user.clear(screen.getByLabelText("Name"));
  await user.paste("Synthetic Earned Revised");
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("changed");
  expect(screen.getByLabelText("Name")).toHaveValue("Synthetic Earned Revised");
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(writes[0]?.url).toContain(`/hr/leave-configs/${earnedID}`);
  expect(writes[0]?.body).toMatchObject({
    expected_version: 1,
    name: "Synthetic Earned Revised",
    rules: { short_leave_minutes: 120, balance_tracked: true },
  });
  expect(writes[0]?.body).not.toHaveProperty("calendar_year");
});

it("confirms before retiring a configuration", async () => {
  const writes = mock({ status: 200 });
  const { user } = renderWithProviders(<LeaveSetup />);
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Earned" }));
  await user.click(screen.getByRole("checkbox", { name: "Available for new leave setup" }));
  await user.click(
    screen.getByRole("checkbox", { name: "I reviewed the effective date and leave rules" }),
  );
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  const dialog = await screen.findByRole("dialog");
  expect(dialog).toHaveTextContent("Retire Synthetic Earned?");
  expect(writes).toHaveLength(0);
  await user.click(within(dialog).getByRole("button", { name: "Retire" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]?.body).toMatchObject({ active: false });
});

it("builds structures in half days and gives untracked types no allowance", async () => {
  const writes = mock({ status: 200 });
  const { user } = renderWithProviders(<LeaveSetup />);
  await chooseOption(
    user,
    await screen.findByRole("combobox", { name: "Configuration" }),
    "Leave structures",
  );
  await user.click(await screen.findByRole("button", { name: "Add leave structure" }));
  await user.paste("Synthetic Nursing");
  await user.type(screen.getByLabelText("Code"), "syn-nursing");
  await chooseOption(
    user,
    screen.getByRole("combobox", { name: "Add a leave type" }),
    /SYN-EARNED/,
  );
  await chooseOption(
    user,
    screen.getByRole("combobox", { name: "Add a leave type" }),
    /SYN-UNPAID/,
  );
  const earnedGroup = screen.getByRole("group", { name: "Synthetic Earned" });
  await user.clear(within(earnedGroup).getByLabelText("Days per year"));
  await user.type(within(earnedGroup).getByLabelText("Days per year"), "12.5");
  await chooseOption(
    user,
    within(earnedGroup).getByRole("combobox", { name: "Credit" }),
    /monthly/,
  );
  const unpaidGroup = screen.getByRole("group", { name: "Synthetic Unpaid" });
  expect(unpaidGroup).toHaveTextContent("no allowance");
  expect(within(unpaidGroup).queryByLabelText("Days per year")).not.toBeInTheDocument();
  await user.click(
    screen.getByRole("checkbox", { name: "I reviewed the effective date and leave rules" }),
  );
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]?.body).toMatchObject({
    kind: "structure",
    code: "SYN-NURSING",
    entries: [
      { leave_type_revision_id: earnedRevision, annual_half_days: 25, accrual: "monthly" },
      { leave_type_revision_id: unpaidRevision, annual_half_days: 0, carry_forward_half_days: 0 },
    ],
  });
  expect(writes[0]?.body).not.toHaveProperty("rules");
});

it("validates holidays inside the calendar year", async () => {
  const writes = mock({ status: 200 });
  const { user } = renderWithProviders(<LeaveSetup />);
  await chooseOption(
    user,
    await screen.findByRole("combobox", { name: "Configuration" }),
    "Holiday calendars",
  );
  await user.click(await screen.findByRole("button", { name: "Add holiday calendar" }));
  await user.paste("Synthetic 2027");
  await user.type(screen.getByLabelText("Code"), "SYN-2027");
  await user.clear(screen.getByLabelText("Calendar year"));
  await user.type(screen.getByLabelText("Calendar year"), "2027");
  await user.clear(screen.getByLabelText("Effective date"));
  await user.type(screen.getByLabelText("Effective date"), "2026-12-01");
  await user.click(screen.getByRole("button", { name: "Add holiday" }));
  const day = screen.getByRole("group", { name: "Holiday 1" });
  await user.type(within(day).getByLabelText("Date"), "2028-01-26");
  await user.type(within(day).getByLabelText("Holiday name"), "Synthetic Republic");
  await user.click(
    screen.getByRole("checkbox", { name: "I reviewed the effective date and leave rules" }),
  );
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  expect(await screen.findByText("Choose a date in 2027.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.clear(within(day).getByLabelText("Date"));
  await user.type(within(day).getByLabelText("Date"), "2027-01-26");
  await chooseOption(user, within(day).getByRole("combobox", { name: "Category" }), "Government");
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]?.body).toMatchObject({
    kind: "holiday_calendar",
    calendar_year: 2027,
    holidays: [{ holiday_date: "2027-01-26", name: "Synthetic Republic", category: "government" }],
  });
});

it("reads history and hides management controls from readers", async () => {
  mock({ allowed: false });
  const { user } = renderWithProviders(<LeaveSetup />);
  expect(await screen.findByText("Synthetic Earned")).toBeInTheDocument();
  expect(screen.getByText("No balance kept")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add leave type" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Edit Synthetic Earned" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "History for Synthetic Earned" }));
  await waitFor(() =>
    expect(screen.getByRole("region", { name: "Leave configuration history" })).toHaveTextContent(
      "Revision 1",
    ),
  );
});
