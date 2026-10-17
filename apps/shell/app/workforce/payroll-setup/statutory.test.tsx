import "../../../test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../../test/select";
import PayrollSetup from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const ruleID = "22222222-2222-4222-8222-222222222222";
const current = "33333333-3333-4333-8333-333333333333";
const proposal = "44444444-4444-4444-8444-444444444444";
const structureID = "55555555-5555-4555-8555-555555555555";
const stamp = "2026-10-16T00:00:00Z";
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
// Filling a whole rule through the dialog takes many interactions, which shared CI runners run about three times slower.
const longForm = { timeout: 15_000 };

// Synthetic values only: these prove behaviour, never statutory rates.
const pfParams = {
  wage_basis: "statutory_wages",
  contribution_basis: "capped",
  wage_ceiling_paise: 1000000,
  employee_rate_ppm: 100000,
  employer_rate_ppm: 100000,
  eps_rate_ppm: 60000,
  eps_wage_ceiling_paise: 1000000,
  edli_rate_ppm: 4000,
  edli_wage_ceiling_paise: 1000000,
  admin_rate_ppm: 3000,
  admin_minimum_paise: 0,
  rounding_mode: "nearest",
  rounding_unit_paise: 1,
};
const revision = (over: object) => ({
  id: current,
  rule_id: ruleID,
  kind: "pf",
  sequence: 1,
  base_version: 0,
  status: "approved",
  approved_version: 1,
  name: "Synthetic PF",
  covered: true,
  effective_on: "2026-04-01",
  citation: "Synthetic notification 1",
  reference: "",
  pf: pfParams,
  proposed_by_subject: "user-maker",
  decided_by_subject: "user-approver",
  decided_at: stamp,
  decision_note: "",
  created_at: stamp,
  ...over,
});
const pendingRevision = revision({
  id: proposal,
  sequence: 2,
  base_version: 1,
  status: "pending",
  approved_version: undefined,
  effective_on: "2026-10-01",
  citation: "Synthetic notification 2",
  pf: { ...pfParams, employee_rate_ppm: 110000 },
  decided_by_subject: undefined,
  decided_at: undefined,
});
const rule = (pending: boolean) => ({
  id: ruleID,
  kind: "pf",
  version: 1,
  effective_on: "2026-04-01",
  current: revision({}),
  ...(pending && { pending: pendingRevision }),
  created_at: stamp,
  updated_at: stamp,
});
const structure = {
  id: structureID,
  revision_id: current,
  code: "SYN-NURSE",
  name: "Synthetic Nurse Pay",
  description: "",
  active: true,
  lines: [],
  effective_on: "2026-04-01",
  reviewed_by_subject: "user-maker",
  version: 1,
  created_at: stamp,
  updated_at: stamp,
};
const preview = {
  salary: {},
  results: [
    {
      kind: "pf",
      status: "applied",
      rule_id: ruleID,
      revision_id: current,
      wage_basis: "statutory_wages",
      wages_paise: 1200000,
      base_paise: 1000000,
      components: [
        { component: "pf_employee", party: "employee", amount_paise: 100000 },
        { component: "pf_employer_epf", party: "employer", amount_paise: 40000 },
        { component: "pf_employer_eps", party: "employer", amount_paise: 60000 },
        { component: "pf_edli", party: "employer", amount_paise: 4000 },
        { component: "pf_admin", party: "employer", amount_paise: 3000 },
      ],
    },
    { kind: "esi", status: "not_configured", wages_paise: 1200000, base_paise: 0, components: [] },
  ],
  employee_paise: 100000,
  employer_paise: 107000,
  net_after_statutory_paise: 1100000,
};

function mock({
  permissions = ["workforce.payroll.view", "workforce.payroll.configure"],
  subject = "user-maker",
  rules = [] as unknown[],
  fail = false,
  reject = "",
  previewBody = preview,
  incomeTaxBody = {},
}: {
  permissions?: string[];
  subject?: string;
  rules?: unknown[];
  fail?: boolean;
  reject?: string;
  previewBody?: unknown;
  incomeTaxBody?: unknown;
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
      else if (url.endsWith(`/statutory-rules/${ruleID}/history`))
        body = page([pendingRevision, revision({})]);
      else if (url.endsWith("/hr/payroll/statutory-rules"))
        [code, body] = fail ? [500, { error: "internal", message: "boom" }] : [200, page(rules)];
      else if (url.endsWith("/hr/payroll/salary-structures")) body = page([structure]);
      if (init?.method === "PUT" || init?.method === "POST") {
        writes.push({ url, body: JSON.parse(String(init.body)) });
        [code, body] = reject
          ? [409, { error: "conflict", message: reject }]
          : [
              200,
              url.endsWith("/statutory-preview")
                ? previewBody
                : url.endsWith("/income-tax-preview")
                  ? incomeTaxBody
                  : rule(false),
            ];
      }
      return new Response(JSON.stringify(body), { status: code });
    }),
  );
  return writes;
}

const panel = () => screen.findByRole("region", { name: "Statutory contributions" });

it("explains unconfigured rules and offers no proposals to readers", async () => {
  mock({ permissions: ["workforce.payroll.view"] });
  renderWithProviders(<PayrollSetup />);
  const section = await panel();
  expect(
    await within(section).findByText(/Payroll will not compute Provident Fund until a rule/),
  ).toBeInTheDocument();
  expect(
    within(section).getByText(/Payroll will not compute Employees' State Insurance/),
  ).toBeInTheDocument();
  expect(within(section).queryByRole("button", { name: /Propose/ })).toBeNull();
});

it("offers a retry when statutory rules cannot be loaded", async () => {
  mock({ fail: true });
  renderWithProviders(<PayrollSetup />);
  const section = await panel();
  expect(await within(section).findByText("Could not load statutory rules.")).toBeInTheDocument();
  expect(within(section).getByRole("button", { name: "Try again" })).toBeInTheDocument();
});

it("refuses a blank PF proposal and submits only the cited values", longForm, async () => {
  const writes = mock();
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await panel();
  await user.click(
    await within(section).findByRole("button", { name: "Propose Provident Fund change" }),
  );
  const form = within(section).getByRole("form", { name: "Propose Provident Fund rule" });
  await user.click(within(form).getByRole("button", { name: "Submit for approval" }));
  expect(
    await within(form).findByText("State whether the hospital is covered."),
  ).toBeInTheDocument();
  expect(within(form).queryByText("Provident Fund parameters")).toBeNull();
  await chooseOption(user, within(form).getByRole("combobox", { name: "Coverage" }), "Covered");
  await user.click(within(form).getByLabelText("Name"));
  await user.paste("Synthetic PF");
  await user.click(within(form).getByLabelText("Notification or section relied on"));
  await user.paste("Synthetic notification 1");
  await user.click(within(form).getByRole("button", { name: "Submit for approval" }));
  expect(await within(form).findAllByText("Choose an option.")).toHaveLength(4);
  expect(within(form).getAllByText(/Enter a percentage from 0 to 100/)).toHaveLength(5);
  expect(writes).toHaveLength(0);
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Wages" }),
    "Labour Code wages",
  );
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Contributions on" }),
    "Capped at the wage ceiling",
  );
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Rounding" }),
    "Nearest (half up)",
  );
  await chooseOption(user, within(form).getByRole("combobox", { name: "Round to" }), "1 paisa");
  for (const [label, value] of [
    ["Wage ceiling (₹)", "10000"],
    ["Employee share (%)", "10"],
    ["Employer share (%)", "10"],
    ["Of which pension (%)", "12"],
    ["Pension wage ceiling (₹)", "10000"],
    ["EDLI (%)", "0.4"],
    ["EDLI wage ceiling (₹)", "10000"],
    ["Admin charges (%)", "0.3"],
    ["Admin minimum per establishment (₹)", "0"],
  ]) {
    await user.click(within(form).getByLabelText(label));
    await user.paste(value);
  }
  await user.click(within(form).getByRole("button", { name: "Submit for approval" }));
  expect(
    await within(form).findByText(
      "The pension share comes out of the employer share, so it cannot exceed it.",
    ),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.clear(within(form).getByLabelText("Of which pension (%)"));
  await user.paste("6");
  await user.click(within(form).getByRole("button", { name: "Submit for approval" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: expect.stringMatching(/\/hr\/payroll\/statutory-rules\/[0-9a-f-]{36}$/),
    body: {
      expected_version: 0,
      kind: "pf",
      name: "Synthetic PF",
      covered: true,
      effective_on: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/),
      citation: "Synthetic notification 1",
      reference: "",
      pf: pfParams,
    },
  });
  expect(await screen.findByText("Proposal submitted")).toBeInTheDocument();
});

it("lets a second person approve and requires a note to reject", async () => {
  const writes = mock({
    permissions: ["workforce.payroll.view", "workforce.payroll.approve"],
    subject: "user-approver",
    rules: [rule(true)],
  });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await panel();
  expect(await within(section).findByText(/proposed by user-maker/)).toBeInTheDocument();
  expect(within(section).queryByRole("button", { name: /Propose Provident Fund/ })).toBeNull();
  await user.click(within(section).getByRole("button", { name: "Review Provident Fund proposal" }));
  const form = within(section).getByRole("form", { name: "Decide proposal" });
  expect(within(form).queryByRole("button", { name: "Withdraw proposal" })).toBeNull();
  await user.click(within(form).getByRole("button", { name: "Reject proposal" }));
  expect(await within(form).findByText("Give a reason for rejecting.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.click(within(form).getByRole("button", { name: "Approve proposal" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: expect.stringMatching(new RegExp(`/statutory-rules/${ruleID}/decision$`)),
    body: { proposal_id: proposal, expected_version: 1, decision: "approve", note: "" },
  });
  expect(await screen.findByText("Proposal approved")).toBeInTheDocument();
});

it("keeps a proposer to withdrawing and shows a refused decision", async () => {
  const writes = mock({
    permissions: [
      "workforce.payroll.view",
      "workforce.payroll.configure",
      "workforce.payroll.approve",
    ],
    rules: [rule(true)],
    reject: "This proposal has already been decided.",
  });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await panel();
  expect(await within(section).findByText(/proposed by you/)).toBeInTheDocument();
  expect(within(section).queryByRole("button", { name: /Propose Provident Fund/ })).toBeNull();
  await user.click(
    within(section).getByRole("button", { name: "Withdraw Provident Fund proposal" }),
  );
  const form = within(section).getByRole("form", { name: "Withdraw proposal" });
  expect(within(form).queryByRole("button", { name: "Approve proposal" })).toBeNull();
  expect(within(form).queryByRole("button", { name: "Reject proposal" })).toBeNull();
  await user.type(
    within(form).getByLabelText("Reason for withdrawing (optional)"),
    " Synthetic typo ",
  );
  await user.click(within(form).getByRole("button", { name: "Withdraw proposal" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: expect.stringMatching(new RegExp(`/statutory-rules/${ruleID}/withdraw$`)),
    body: { proposal_id: proposal, note: "Synthetic typo" },
  });
  expect(
    await within(form).findByText(/This proposal has already been decided/),
  ).toBeInTheDocument();
});

it("lists every revision with its decision in history", async () => {
  mock({ rules: [rule(true)] });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await panel();
  await user.click(
    await within(section).findByRole("button", { name: "History of Provident Fund" }),
  );
  const history = await within(section).findByRole("region", { name: "Provident Fund history" });
  const items = await within(history).findAllByRole("listitem");
  expect(items).toHaveLength(2);
  expect(items[0]).toHaveTextContent("Awaiting approval · effective 2026-10-01");
  expect(items[1]).toHaveTextContent("Approved · v1 · effective 2026-04-01");
  expect(items[1]).toHaveTextContent("decided by user-approver");
});

it("previews contributions for a structure without storing anything", async () => {
  const writes = mock({ rules: [rule(false)] });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await panel();
  await user.click(await within(section).findByRole("button", { name: "Preview contributions" }));
  const form = within(section).getByRole("form", { name: "Statutory preview inputs" });
  await user.click(within(form).getByRole("button", { name: "Compute contributions" }));
  expect(await within(form).findByText("Choose a structure.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Salary structure" }),
    "Synthetic Nurse Pay · SYN-NURSE",
  );
  await user.type(within(form).getByLabelText("Payable days"), "30");
  await user.click(within(form).getByRole("checkbox", { name: /person with disability/ }));
  await user.click(within(form).getByRole("button", { name: "Compute contributions" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: expect.stringMatching(/\/hr\/payroll\/statutory-preview$/),
    body: {
      structure_id: structureID,
      on: expect.any(String),
      payable_half_days: 60,
      disability: true,
    },
  });
  expect(writes[0]?.body).not.toHaveProperty("gender");
  const region = within(section).getByRole("region", { name: "Statutory preview" });
  expect(await within(region).findByText("Provident Fund · Applied")).toBeInTheDocument();
  expect(
    within(region).getByText("Employees' State Insurance · Not configured"),
  ).toBeInTheDocument();
  expect(within(region).getByRole("cell", { name: "Pension (EPS)" })).toBeInTheDocument();
  expect(within(region).getByText("Net after contributions")).toBeInTheDocument();
});

const fill = async (
  user: Awaited<ReturnType<typeof renderWithProviders>>["user"],
  scope: HTMLElement | ReturnType<typeof within>,
  fields: [string, string][],
) => {
  const q = scope instanceof HTMLElement ? within(scope) : scope;
  for (const [label, value] of fields) {
    await user.click(q.getByLabelText(label));
    await user.paste(value);
  }
};
const openProposal = async (kind: string) => {
  const writes = mock();
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await panel();
  await user.click(await within(section).findByRole("button", { name: `Propose ${kind} change` }));
  const form = within(section).getByRole("form", { name: `Propose ${kind} rule` });
  await chooseOption(user, within(form).getByRole("combobox", { name: "Coverage" }), "Covered");
  await fill(user, form, [
    ["Name", `Synthetic ${kind}`],
    ["Notification or section relied on", "Synthetic notification"],
  ]);
  return { writes, user, form };
};

it("builds a professional tax table and refuses one with a gender left out", longForm, async () => {
  const { writes, user, form } = await openProposal("Professional tax");
  await user.click(within(form).getByRole("button", { name: "Submit for approval" }));
  expect(
    await within(form).findByText("Enter the state code, for example IN-MH."),
  ).toBeInTheDocument();
  expect(within(form).getByText("Choose its months.")).toBeInTheDocument();
  expect(within(form).getByText("Choose who it applies to.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await fill(user, form, [["State code (for example IN-MH)", "in-zz"]]);
  await chooseOption(user, within(form).getByRole("combobox", { name: "Wages" }), "Gross pay");
  const first = within(within(form).getByRole("group", { name: "Slab 1" }));
  await fill(user, first, [
    ["Monthly wages from (₹)", "0"],
    ["Up to, excluding (₹, blank for no limit)", "100"],
    ["Tax per month (₹)", "0"],
  ]);
  const firstMonths = within(first.getByRole("group", { name: "Months slab 1 applies in" }));
  await user.click(firstMonths.getByRole("checkbox", { name: "Feb" }));
  const firstWho = within(first.getByRole("group", { name: "Slab 1 applies to" }));
  await user.click(firstWho.getByRole("checkbox", { name: "Women" }));
  await user.click(firstWho.getByRole("checkbox", { name: "Men" }));
  await user.click(firstWho.getByRole("checkbox", { name: "Other genders" }));
  await user.click(within(form).getByRole("button", { name: "Submit for approval" }));
  expect(
    await within(form).findByText(
      "Feb, women: the table needs a last slab from ₹100.00 with no upper limit.",
    ),
  ).toBeInTheDocument();
  await user.click(within(form).getByRole("button", { name: "Add slab" }));
  const second = within(within(form).getByRole("group", { name: "Slab 2" }));
  await fill(user, second, [
    ["Monthly wages from (₹)", "100"],
    ["Tax per month (₹)", "3"],
  ]);
  await user.click(
    within(second.getByRole("group", { name: "Months slab 2 applies in" })).getByRole("checkbox", {
      name: "Feb",
    }),
  );
  const secondWho = within(second.getByRole("group", { name: "Slab 2 applies to" }));
  await user.click(secondWho.getByRole("checkbox", { name: "Women" }));
  await user.click(secondWho.getByRole("checkbox", { name: "Men" }));
  await user.click(within(form).getByRole("button", { name: "Submit for approval" }));
  expect(
    await within(form).findByText(
      "Feb, other genders: the table needs a last slab from ₹100.00 with no upper limit.",
    ),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.click(secondWho.getByRole("checkbox", { name: "Other genders" }));
  expect(
    within(form).getByText("Highest yearly tax: women ₹3.00 · men ₹3.00 · other genders ₹3.00"),
  ).toBeInTheDocument();
  await user.click(within(form).getByRole("button", { name: "Submit for approval" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]?.body).toEqual({
    expected_version: 0,
    kind: "pt",
    name: "Synthetic Professional tax",
    covered: true,
    effective_on: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/),
    citation: "Synthetic notification",
    reference: "",
    pt: {
      state_code: "IN-ZZ",
      wage_basis: "gross",
      slabs: [
        {
          months: [2],
          genders: ["female", "male", "other"],
          from_paise: 0,
          to_paise: 10000,
          amount_paise: 0,
        },
        { months: [2], genders: ["female", "male", "other"], from_paise: 10000, amount_paise: 300 },
      ],
    },
  });
});

it("removes a professional tax slab but always keeps one", async () => {
  const { user, form } = await openProposal("Professional tax");
  expect(within(form).getByRole("button", { name: "Remove slab 1" })).toBeDisabled();
  await user.click(within(form).getByRole("button", { name: "Add slab" }));
  await fill(user, within(within(form).getByRole("group", { name: "Slab 2" })), [
    ["Tax per month (₹)", "7"],
  ]);
  await user.click(within(form).getByRole("button", { name: "Remove slab 1" }));
  expect(within(form).queryByRole("group", { name: "Slab 2" })).toBeNull();
  expect(
    within(within(form).getByRole("group", { name: "Slab 1" })).getByLabelText("Tax per month (₹)"),
  ).toHaveValue("7");
});

it("submits labour welfare fund slabs with optional caps", longForm, async () => {
  const { writes, user, form } = await openProposal("Labour welfare fund");
  await user.click(within(form).getByRole("button", { name: "Submit for approval" }));
  expect(
    await within(form).findByText("Choose the months LWF is deducted in."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await fill(user, form, [["State code (for example IN-MH)", "IN-ZZ"]]);
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Wages" }),
    "Labour Code wages",
  );
  await chooseOption(user, within(form).getByRole("combobox", { name: "Rounding" }), "Always up");
  await chooseOption(user, within(form).getByRole("combobox", { name: "Round to" }), "₹1");
  const months = within(within(form).getByRole("group", { name: "Deducted in" }));
  await user.click(months.getByRole("checkbox", { name: "Dec" }));
  await user.click(months.getByRole("checkbox", { name: "Jun" }));
  await fill(user, within(within(form).getByRole("group", { name: "Slab 1" })), [
    ["Wages from (₹)", "0"],
    ["Employee fixed (₹)", "0"],
    ["Employee rate (%)", "0.2"],
    ["Employee cap (₹, optional)", "5"],
    ["Employer fixed (₹)", "10"],
    ["Employer rate (%)", "0"],
  ]);
  await user.click(within(form).getByRole("button", { name: "Submit for approval" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]?.body).toMatchObject({
    kind: "lwf",
    lwf: {
      state_code: "IN-ZZ",
      wage_basis: "statutory_wages",
      deduction_months: [6, 12],
      rounding_mode: "up",
      rounding_unit_paise: 100,
      slabs: [
        {
          from_paise: 0,
          employee_fixed_paise: 0,
          employee_rate_ppm: 2000,
          employee_cap_paise: 500,
          employer_fixed_paise: 1000,
          employer_rate_ppm: 0,
        },
      ],
    },
  });
  expect(writes[0]?.body).not.toHaveProperty("pt");
});

it("explains a state levy recorded as not covered", async () => {
  const { user, form } = await openProposal("Professional tax");
  await chooseOption(user, within(form).getByRole("combobox", { name: "Coverage" }), "Not covered");
  expect(within(form).getByText(/assessed half-yearly or yearly/)).toBeInTheDocument();
  expect(within(form).queryByText("Professional tax parameters")).toBeNull();
});

it("sends a chosen gender and labels state levy statuses", async () => {
  const writes = mock({
    rules: [rule(false)],
    previewBody: {
      ...preview,
      results: [
        {
          kind: "pt",
          status: "gender_required",
          wages_paise: 1200000,
          base_paise: 1200000,
          components: [],
        },
        {
          kind: "lwf",
          status: "not_due",
          wages_paise: 1200000,
          base_paise: 1200000,
          components: [],
        },
      ],
      employee_paise: 0,
      employer_paise: 0,
    },
  });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await panel();
  await user.click(await within(section).findByRole("button", { name: "Preview contributions" }));
  const form = within(section).getByRole("form", { name: "Statutory preview inputs" });
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Salary structure" }),
    "Synthetic Nurse Pay · SYN-NURSE",
  );
  await user.type(within(form).getByLabelText("Payable days"), "30");
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Gender (professional tax)" }),
    "Women",
  );
  await user.click(within(form).getByRole("button", { name: "Compute contributions" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]?.body).toMatchObject({ gender: "female" });
  const region = within(section).getByRole("region", { name: "Statutory preview" });
  expect(
    await within(region).findByText("Professional tax · Needs the employee's gender"),
  ).toBeInTheDocument();
  expect(within(region).getByText(/differs by gender/)).toBeInTheDocument();
  expect(within(region).getByText("Labour welfare fund · Not due this month")).toBeInTheDocument();
});

it(
  "builds an income-tax rule for one tax year with age bands, rebate and surcharge",
  longForm,
  async () => {
    const { writes, user, form } = await openProposal("Income tax (new regime)");
    await user.click(within(form).getByRole("button", { name: "Submit for approval" }));
    expect(
      await within(form).findByText("Enter the tax year as YYYY-YY, for example 2026-27."),
    ).toBeInTheDocument();
    expect(writes).toHaveLength(0);
    const effective = (within(form).getByLabelText("Effective date") as HTMLInputElement).value;
    await user.clear(within(form).getByLabelText("Effective date"));
    await user.type(within(form).getByLabelText("Effective date"), "2027-04-01");
    await fill(user, form, [
      ["Tax year (for example 2026-27)", "2026-27"],
      ["Standard deduction (₹)", "10000"],
      ["Health and education cess (%)", "5"],
    ]);
    for (const [name, option] of [
      ["Round taxable income", "Nearest (half up)"],
      ["Taxable income to", "₹10"],
      ["Round tax payable", "Always up"],
      ["Tax payable to", "₹10"],
      ["Rebate", "Rebate with marginal relief"],
    ]) {
      await chooseOption(user, within(form).getByRole("combobox", { name }), option);
    }
    const base = within(within(form).getByRole("group", { name: "Age band 1" }));
    await fill(user, base, [
      ["Applies from age", "0"],
      ["Slab 1 taxable income from (₹)", "0"],
      ["Up to (₹, blank for no limit)", "100000"],
      ["Rate (%)", "0"],
    ]);
    await user.click(base.getByRole("button", { name: "Add slab" }));
    await fill(user, base, [["Slab 2 taxable income from (₹)", "150000"]]);
    await user.click(base.getAllByLabelText("Rate (%)")[1] as HTMLElement);
    await user.paste("10");
    await user.click(within(form).getByRole("button", { name: "Add age band" }));
    await fill(user, within(within(form).getByRole("group", { name: "Age band 2" })), [
      ["Applies from age", "60"],
      ["Slab 1 taxable income from (₹)", "0"],
      ["Rate (%)", "0"],
    ]);
    await fill(user, form, [
      ["Taxable income up to (₹)", "150000"],
      ["Rebate at most (₹)", "10000"],
    ]);
    await user.click(within(form).getByRole("button", { name: "Add surcharge band" }));
    await fill(user, form, [
      ["Band 1 on taxable income above (₹)", "5000000"],
      ["Rate on tax (%)", "10"],
    ]);
    await user.click(within(form).getByRole("button", { name: "Submit for approval" }));
    expect(
      await within(form).findByText(/The effective date falls in tax year 2027-28/),
    ).toBeInTheDocument();
    expect(
      within(form).getByText(
        "The table leaves income from ₹1,00,000.00 to ₹1,50,000.00 without a slab.",
      ),
    ).toBeInTheDocument();
    expect(writes).toHaveLength(0);
    await user.clear(within(form).getByLabelText("Effective date"));
    await user.type(within(form).getByLabelText("Effective date"), "2027-03-31");
    await user.clear(base.getByLabelText("Slab 2 taxable income from (₹)"));
    await user.type(base.getByLabelText("Slab 2 taxable income from (₹)"), "100000");
    await user.click(within(form).getByRole("button", { name: "Submit for approval" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(effective).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(writes[0]?.body).toMatchObject({
      kind: "income_tax_new",
      covered: true,
      effective_on: "2027-03-31",
      income_tax: {
        tax_year: "2026-27",
        standard_deduction_paise: 1_000_000,
        taxable_rounding_mode: "nearest",
        taxable_rounding_unit_paise: 1000,
        tax_rounding_mode: "up",
        tax_rounding_unit_paise: 1000,
        age_bands: [
          {
            min_age: 0,
            slabs: [
              { from_paise: 0, to_paise: 10_000_000, rate_ppm: 0 },
              { from_paise: 10_000_000, rate_ppm: 100_000 },
            ],
          },
          { min_age: 60, slabs: [{ from_paise: 0, rate_ppm: 0 }] },
        ],
        rebate: { income_limit_paise: 15_000_000, max_paise: 1_000_000, marginal_relief: true },
        surcharge_bands: [{ above_paise: 500_000_000, rate_ppm: 100_000 }],
        cess_rate_ppm: 50_000,
      },
    });
    expect(writes[0]?.body).not.toHaveProperty("pt");
  },
);

it("hides rebate amounts without a rebate and explains an uncovered regime", async () => {
  const { user, form } = await openProposal("Income tax (old regime)");
  expect(within(form).queryByLabelText("Taxable income up to (₹)")).toBeNull();
  await chooseOption(user, within(form).getByRole("combobox", { name: "Rebate" }), "No rebate");
  expect(within(form).queryByLabelText("Rebate at most (₹)")).toBeNull();
  await chooseOption(user, within(form).getByRole("combobox", { name: "Coverage" }), "Not covered");
  expect(within(form).getByText(/will not deduct tax under this regime/)).toBeInTheDocument();
  expect(within(form).queryByText("Income-tax parameters")).toBeNull();
});

it("previews each regime's annual tax and explains what is missing", async () => {
  const result = (kind: string, status: string, over: object = {}) => ({
    kind,
    status,
    income_paise: 120_000_000,
    standard_deduction_paise: 0,
    taxable_paise: 0,
    slab_tax_paise: 0,
    rebate_paise: 0,
    surcharge_paise: 0,
    cess_paise: 0,
    total_paise: 0,
    ...over,
  });
  const writes = mock({
    rules: [rule(false)],
    incomeTaxBody: {
      on: "2026-10-01",
      results: [
        result("income_tax_new", "applied", {
          tax_year: "2026-27",
          age_band_min_age: 0,
          standard_deduction_paise: 1_000_000,
          taxable_paise: 119_000_000,
          slab_tax_paise: 9_900_000,
          rebate_paise: 400_000,
          surcharge_paise: 0,
          cess_paise: 380_000,
          total_paise: 9_880_000,
        }),
        result("income_tax_old", "tax_year_missing", { tax_year: "2025-26" }),
      ],
    },
  });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await panel();
  await user.click(await within(section).findByRole("button", { name: "Preview income tax" }));
  const form = within(section).getByRole("form", { name: "Income-tax preview inputs" });
  await fill(user, form, [["Age (optional)", "121"]]);
  await user.click(within(form).getByRole("button", { name: "Compute tax" }));
  expect(
    await within(form).findByText("Enter the annual salary in rupees, with up to 2 decimals."),
  ).toBeInTheDocument();
  expect(
    within(form).getByText("Enter an age from 0 to 120, or leave it blank."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.clear(within(form).getByLabelText("Age (optional)"));
  await user.clear(within(form).getByLabelText("Tax year containing"));
  await user.type(within(form).getByLabelText("Tax year containing"), "2026-10-01");
  await fill(user, form, [["Annual salary (₹)", "12,00,000"]]);
  await user.click(within(form).getByRole("button", { name: "Compute tax" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: expect.stringMatching(/\/hr\/payroll\/income-tax-preview$/),
    body: { on: "2026-10-01", annual_income_paise: 120_000_000 },
  });
  const region = within(section).getByRole("region", { name: "Income-tax preview" });
  expect(await within(region).findByText("Income tax (new regime) · Applied")).toBeInTheDocument();
  const table = within(region).getByRole("table", {
    name: "Income tax (new regime) for tax year 2026-27",
  });
  expect(within(table).getByRole("row", { name: "Rebate − ₹4,000.00" })).toBeInTheDocument();
  expect(
    within(table).getByRole("row", { name: "Tax for the year ₹98,800.00" }),
  ).toBeInTheDocument();
  expect(
    within(region).getByText("Income tax (old regime) · No rule for this tax year"),
  ).toBeInTheDocument();
  expect(within(region).getByText(/The rule in force covers tax year 2025-26/)).toBeInTheDocument();
  expect(within(section).queryByRole("region", { name: "Statutory preview" })).toBeNull();
});

it("asks for an age when a regime's slabs differ by age", async () => {
  const writes = mock({
    rules: [rule(false)],
    incomeTaxBody: {
      on: "2026-10-01",
      results: [
        {
          kind: "income_tax_new",
          status: "age_required",
          income_paise: 100,
          standard_deduction_paise: 0,
          taxable_paise: 0,
          slab_tax_paise: 0,
          rebate_paise: 0,
          surcharge_paise: 0,
          cess_paise: 0,
          total_paise: 0,
        },
      ],
    },
  });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await panel();
  await user.click(await within(section).findByRole("button", { name: "Preview income tax" }));
  const form = within(section).getByRole("form", { name: "Income-tax preview inputs" });
  await fill(user, form, [
    ["Annual salary (₹)", "1"],
    ["Age (optional)", "61"],
  ]);
  await user.click(within(form).getByRole("button", { name: "Compute tax" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]?.body).toMatchObject({ annual_income_paise: 100, age: 61 });
  const region = within(section).getByRole("region", { name: "Income-tax preview" });
  expect(
    await within(region).findByText("Income tax (new regime) · Needs the employee's age"),
  ).toBeInTheDocument();
  expect(within(region).getByText(/Enter the age and compute again/)).toBeInTheDocument();
  expect(within(region).queryByRole("table")).toBeNull();
});
