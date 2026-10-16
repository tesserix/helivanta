import "../../../test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../../test/select";
import PayrollSetup from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const staffID = "22222222-2222-4222-8222-222222222222";
const structureID = "33333333-3333-4333-8333-333333333333";
const basicID = "44444444-4444-4444-8444-444444444444";
const pfID = "55555555-5555-4555-8555-555555555555";
const current = "66666666-6666-4666-8666-666666666666";
const proposal = "77777777-7777-4777-8777-777777777777";
const electionID = "88888888-8888-4888-8888-888888888888";
const stamp = "2026-10-16T00:00:00Z";
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());

// Synthetic values only: these prove behaviour, never real pay or rates.
const member = {
  id: staffID,
  display_name: "Synthetic Nurse",
  employee_code: "SYN-1",
  employment_status: "active",
};
const structure = {
  id: structureID,
  revision_id: current,
  code: "SYN-NURSE",
  name: "Synthetic Nurse Pay",
  description: "",
  active: true,
  lines: [
    {
      position: 1,
      head_id: basicID,
      code: "BASIC",
      name: "Basic",
      kind: "earning",
      basis: "fixed",
      amount_paise: 2000000,
    },
    { position: 2, head_id: pfID, code: "PF", name: "PF", kind: "deduction", basis: "percent" },
  ],
  effective_on: "2026-04-01",
  reviewed_by_subject: "user-maker",
  version: 1,
  created_at: stamp,
  updated_at: stamp,
};
const revision = (over: object) => ({
  id: current,
  staff_id: staffID,
  structure_id: structureID,
  sequence: 1,
  base_version: 0,
  status: "approved",
  approved_version: 1,
  effective_on: "2026-04-01",
  reason: "Synthetic appointment",
  amounts: [{ head_id: basicID, amount_paise: 2500000 }],
  proposed_by_subject: "user-maker",
  decided_by_subject: "user-approver",
  decided_at: stamp,
  decision_note: "",
  created_at: stamp,
  ...over,
});
const assignment = (pending: boolean) => ({
  staff_id: staffID,
  version: 1,
  effective_on: "2026-04-01",
  current: revision({}),
  ...(pending && {
    pending: revision({
      id: proposal,
      sequence: 2,
      base_version: 1,
      status: "pending",
      approved_version: undefined,
      effective_on: "2026-10-01",
      reason: "Synthetic increment",
      amounts: [{ head_id: basicID, amount_paise: 2700000 }],
      decided_by_subject: undefined,
      decided_at: undefined,
    }),
  }),
  created_at: stamp,
  updated_at: stamp,
});
const election = {
  id: electionID,
  staff_id: staffID,
  tax_year: "2026-27",
  regime: "new",
  intimated_on: "2026-04-03",
  version: 1,
  created_at: stamp,
  updated_at: stamp,
};

function mock({
  permissions = ["workforce.payroll.view", "workforce.payroll.configure"],
  subject = "user-maker",
  pay = null as unknown,
  payStatus = 200,
  elections = [] as unknown[],
  previewBody = {} as unknown,
}: {
  permissions?: string[];
  subject?: string;
  pay?: unknown;
  payStatus?: number;
  elections?: unknown[];
  previewBody?: unknown;
} = {}) {
  const writes: { url: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      let code = 200;
      let body: unknown = page([]);
      if (url.includes("permissions"))
        body = { data: permissions, subject, tenant_id: "99999999-9999-4999-8999-999999999999" };
      else if (url.endsWith("/hospitals"))
        body = page([{ id: hospital, name: "Synthetic Hospital", access_level: "manager" }]);
      else if (url.endsWith(`/hospitals/${hospital}/staff`)) body = page([member]);
      else if (url.endsWith("/hr/payroll/salary-structures")) body = page([structure]);
      else if (url.endsWith(`/staff/${staffID}/salary-assignment`))
        [code, body] =
          payStatus !== 200
            ? [payStatus, { error: payStatus === 404 ? "not_found" : "internal", message: "x" }]
            : [200, pay];
      else if (url.endsWith(`/staff/${staffID}/tax-regimes`)) body = page(elections);
      if (init?.method === "PUT" || init?.method === "POST") {
        writes.push({ url, body: JSON.parse(String(init.body)) });
        [code, body] = [
          200,
          url.endsWith("/salary-preview")
            ? previewBody
            : url.includes("/tax-regimes/")
              ? election
              : assignment(false),
        ];
      }
      return new Response(JSON.stringify(body), { status: code });
    }),
  );
  return writes;
}

const choose = async (user: Awaited<ReturnType<typeof renderWithProviders>>["user"]) => {
  const section = await screen.findByRole("region", { name: "Staff pay" });
  await chooseOption(
    user,
    await within(section).findByRole("combobox", { name: "Staff member" }),
    "Synthetic Nurse · SYN-1",
  );
  return section;
};

it("says nothing is paid until approved and offers readers no changes", async () => {
  mock({ permissions: ["workforce.payroll.view"], payStatus: 404 });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  expect(
    await within(section).findByText(/Payroll will not pay Synthetic Nurse until an assignment/),
  ).toBeInTheDocument();
  expect(within(section).getByText("No regime recorded for any tax year.")).toBeInTheDocument();
  expect(within(section).queryByRole("button", { name: /Propose pay/ })).toBeNull();
  expect(within(section).queryByRole("button", { name: "Record election" })).toBeNull();
});

it("offers a retry when a staff member's pay cannot be loaded", async () => {
  mock({ payStatus: 500 });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  expect(
    await within(section).findByText("Could not load this staff member's pay."),
  ).toBeInTheDocument();
  expect(within(section).getByRole("button", { name: "Try again" })).toBeInTheDocument();
});

it("refuses a proposal without a structure or reason and sends only overridden amounts", async () => {
  const writes = mock({ payStatus: 404 });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  await user.click(await within(section).findByRole("button", { name: "Propose pay" }));
  const form = within(section).getByRole("form", { name: "Propose pay" });
  await user.click(within(form).getByRole("button", { name: "Propose pay" }));
  expect(await within(form).findByText("Choose a structure.")).toBeInTheDocument();
  expect(within(form).getByText("Enter a reason.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Salary structure" }),
    "Synthetic Nurse Pay · SYN-NURSE",
  );
  expect(within(form).queryByLabelText("PF (₹ monthly)")).toBeNull();
  await user.type(within(form).getByLabelText("Basic (₹ monthly)"), "12.345");
  await user.type(within(form).getByLabelText("Reason"), "Synthetic appointment");
  await user.click(within(form).getByRole("button", { name: "Propose pay" }));
  expect(
    await within(form).findByText("Enter rupees with up to two decimals, or leave blank."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.clear(within(form).getByLabelText("Basic (₹ monthly)"));
  await user.type(within(form).getByLabelText("Basic (₹ monthly)"), "25000");
  await user.click(within(form).getByRole("button", { name: "Propose pay" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: expect.stringMatching(new RegExp(`/hr/payroll/staff/${staffID}/salary-assignment$`)),
    body: {
      expected_version: 0,
      structure_id: structureID,
      effective_on: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/),
      reason: "Synthetic appointment",
      amounts: [{ head_id: basicID, amount_paise: 2500000 }],
    },
  });
  expect(await screen.findByText("Pay proposed")).toBeInTheDocument();
});

it("lets a second person approve and requires a note to reject", async () => {
  const writes = mock({
    permissions: ["workforce.payroll.view", "workforce.payroll.approve"],
    subject: "user-approver",
    pay: assignment(true),
  });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  expect(
    await within(section).findByText("Synthetic Nurse Pay · Basic ₹25,000.00"),
  ).toBeInTheDocument();
  expect(within(section).getByText(/proposed by user-maker/)).toBeInTheDocument();
  expect(within(section).queryByRole("button", { name: /Propose pay/ })).toBeNull();
  await user.click(within(section).getByRole("button", { name: "Review proposal" }));
  const form = within(section).getByRole("form", { name: "Decide pay proposal" });
  await user.click(within(form).getByRole("button", { name: "Reject pay" }));
  expect(await within(form).findByText("Give a reason for rejecting.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.click(within(form).getByRole("button", { name: "Approve pay" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: expect.stringMatching(/\/salary-assignment\/decision$/),
    body: { proposal_id: proposal, expected_version: 1, decision: "approve", note: "" },
  });
  expect(await screen.findByText("Pay approved")).toBeInTheDocument();
});

it("keeps the proposer to withdrawing their own proposal", async () => {
  const writes = mock({
    permissions: [
      "workforce.payroll.view",
      "workforce.payroll.configure",
      "workforce.payroll.approve",
    ],
    pay: assignment(true),
  });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  expect(await within(section).findByText(/proposed by you/)).toBeInTheDocument();
  await user.click(within(section).getByRole("button", { name: "Withdraw" }));
  const form = within(section).getByRole("form", { name: "Withdraw pay proposal" });
  expect(within(form).queryByRole("button", { name: "Approve pay" })).toBeNull();
  await user.click(within(form).getByRole("button", { name: "Withdraw proposal" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: expect.stringMatching(/\/salary-assignment\/withdraw$/),
    body: { proposal_id: proposal, note: "" },
  });
});

it("records a regime only once chosen and needs a reason to change one", async () => {
  const writes = mock({ pay: assignment(false), elections: [election] });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  const regimes = await within(section).findByRole("region", { name: "Income-tax regime" });
  expect(await within(regimes).findByText("2026-27 · New regime")).toBeInTheDocument();

  await user.click(within(regimes).getByRole("button", { name: "Record election" }));
  const record = within(regimes).getByRole("form", { name: "Record election" });
  await user.click(within(record).getByRole("button", { name: "Record election" }));
  expect(
    await within(record).findByText("Choose the regime the employee intimated."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.click(within(record).getByRole("button", { name: "Cancel" }));

  await user.click(within(regimes).getByRole("button", { name: "Change 2026-27 election" }));
  const change = within(regimes).getByRole("form", { name: "Change 2026-27 election" });
  expect(within(change).getByLabelText("Tax year")).toBeDisabled();
  await chooseOption(user, within(change).getByRole("combobox", { name: "Regime" }), "Old regime");
  await user.click(within(change).getByRole("button", { name: "Change election" }));
  expect(
    await within(change).findByText("Give a reason for changing the recorded regime."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.type(within(change).getByLabelText("Reason for the change"), "Synthetic revision");
  await user.click(within(change).getByRole("button", { name: "Change election" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: expect.stringMatching(new RegExp(`/staff/${staffID}/tax-regimes/2026-27$`)),
    body: {
      expected_version: 1,
      regime: "old",
      intimated_on: "2026-04-03",
      note: "Synthetic revision",
    },
  });
  expect(await screen.findByText("Election changed")).toBeInTheDocument();
});

it("previews pay and reports an unrecorded regime instead of assuming one", async () => {
  const writes = mock({
    pay: assignment(false),
    previewBody: {
      tax_year: "2026-27",
      tax_regime_status: "regime_not_elected",
      salary: {
        on: "2026-10-01",
        lines: [
          {
            head_id: basicID,
            payslip_label: "Basic",
            kind: "earning",
            amount_paise: 2500000,
            shown: true,
          },
        ],
        totals: { gross_paise: 2500000 },
      },
      results: [],
      employee_paise: 0,
      employer_paise: 0,
      net_after_statutory_paise: 2500000,
    },
  });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  await user.click(await within(section).findByRole("button", { name: "Preview pay" }));
  const region = within(section).getByRole("region", { name: "Pay preview" });
  await user.type(within(region).getByLabelText("Payable days"), "28");
  await user.click(within(region).getByRole("button", { name: "Compute pay" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: expect.stringMatching(new RegExp(`/staff/${staffID}/salary-preview$`)),
    body: { on: expect.any(String), payable_half_days: 56, disability: false },
  });
  expect(await within(region).findByText(/no regime recorded/)).toBeInTheDocument();
  expect(within(region).getByRole("cell", { name: "Basic" })).toBeInTheDocument();
});
