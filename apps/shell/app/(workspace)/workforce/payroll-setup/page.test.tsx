import "@/test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "@/test/select";
import PayrollSetup from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const basic = "22222222-2222-4222-8222-222222222222";
const hra = "33333333-3333-4333-8333-333333333333";
const pf = "44444444-4444-4444-8444-444444444444";
const structureID = "55555555-5555-4555-8555-555555555555";
const grade = "66666666-6666-4666-8666-666666666666";
const revision = "77777777-7777-4777-8777-777777777777";
const stamp = "2026-10-16T00:00:00Z";
const today = new Date().toISOString().slice(0, 10);
const allMonths = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12];
const head = (over: object) => ({
  revision_id: revision,
  kind: "earning",
  active: true,
  basis: "fixed",
  base_head_ids: [],
  proration: "calendar_days",
  minimum_payable_half_days: 0,
  months: allMonths,
  rounding_mode: "nearest",
  rounding_unit_paise: 100,
  in_gross: true,
  in_ctc: true,
  arrears_eligible: true,
  taxability: "taxable",
  wage_class: "wages",
  payslip_order: 10,
  show_when_zero: false,
  effective_on: "2026-11-01",
  reviewed_by_subject: "synthetic-manager",
  version: 2,
  created_at: stamp,
  updated_at: stamp,
  ...over,
});
const heads = [
  head({ id: basic, code: "SYN-BASIC", name: "Synthetic Basic", payslip_label: "Basic" }),
  head({
    id: hra,
    code: "SYN-HRA",
    name: "Synthetic HRA",
    payslip_label: "HRA",
    basis: "percent",
    rate_ppm: 400000,
    base_head_ids: [basic],
    proration: "none",
    taxability: "exempt",
    tax_note: "Synthetic exemption basis",
    wage_class: "excluded",
  }),
  head({
    id: pf,
    kind: "deduction",
    code: "SYN-PF",
    name: "Synthetic PF",
    payslip_label: "PF",
    basis: "percent",
    rate_ppm: 120000,
    base_head_ids: [basic],
    proration: "none",
    max_amount_paise: 180000,
    in_gross: false,
    in_ctc: false,
    taxability: "not_applicable",
    wage_class: "outside",
  }),
];
const structure = {
  id: structureID,
  revision_id: revision,
  code: "SYN-NURSE",
  name: "Synthetic Nurse Pay",
  description: "",
  active: true,
  grade_id: grade,
  lines: [
    {
      position: 1,
      head_id: basic,
      code: "SYN-BASIC",
      name: "Synthetic Basic",
      kind: "earning",
      basis: "fixed",
      amount_paise: 3000000,
    },
    {
      position: 2,
      head_id: hra,
      code: "SYN-HRA",
      name: "Synthetic HRA",
      kind: "earning",
      basis: "percent",
    },
  ],
  effective_on: "2026-11-01",
  reviewed_by_subject: "synthetic-manager",
  version: 3,
  created_at: stamp,
  updated_at: stamp,
};
const line = (
  head_id: string,
  payslip_label: string,
  kind: string,
  amount_paise: number,
  shown = true,
) => ({
  head_id,
  revision_id: revision,
  payslip_label,
  payslip_order: 10,
  kind,
  amount_paise,
  applicable: amount_paise > 0,
  shown,
});
const preview = {
  structure_id: structureID,
  structure_revision_id: revision,
  period_days: 28,
  payable_half_days: 56,
  lines: [
    line(basic, "Basic", "earning", 3000000),
    line(hra, "HRA", "earning", 1200000),
    line(pf, "PF", "deduction", 180000),
    line(grade, "Synthetic hidden line", "earning", 0, false),
  ],
  totals: {
    gross_paise: 4200000,
    deductions_paise: 180000,
    employer_paise: 0,
    net_paise: 4020000,
    ctc_paise: 4200000,
    negative_net: false,
  },
  wage_test: {
    wages_paise: 3000000,
    excluded_paise: 1200000,
    remuneration_paise: 4200000,
    add_back_paise: 0,
    statutory_wages_paise: 3000000,
    excluded_share_ppm: 285714,
  },
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());

function mock({
  permissions = ["workforce.payroll.view", "workforce.payroll.configure"],
  empty = false,
  fail = false,
  reject = "",
  access = "manager",
}: {
  permissions?: string[];
  empty?: boolean;
  fail?: boolean;
  reject?: string;
  access?: string;
} = {}) {
  const writes: { method: string; url: string; body: unknown }[] = [];
  const reads: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      let code = 200;
      let body: unknown = page([]);
      const kind = /[?&]kind=([a-z_]+)/.exec(url)?.[1];
      if (url.includes("permissions")) body = { data: permissions };
      else if (url.endsWith("/hospitals"))
        body = page([
          {
            id: hospital,
            name: "Synthetic Hospital",
            access_level: access,
          },
        ]);
      else if (url.endsWith("/hr/masters"))
        body = page([
          {
            id: grade,
            revision_id: revision,
            kind: "grade",
            code: "SYN-G1",
            name: "Synthetic Grade A",
            description: "",
            active: true,
            version: 1,
            created_at: stamp,
            updated_at: stamp,
          },
        ]);
      else if (url.endsWith(`/salary-heads/${basic}/history`))
        body = page([
          heads[0],
          { ...heads[0], revision_id: grade, version: 1, effective_on: "2026-10-01" },
        ]);
      else if (url.includes("/hr/payroll/salary-heads")) {
        reads.push(url);
        [code, body] = fail
          ? [500, { error: "internal", message: "boom" }]
          : [200, page(empty ? [] : heads.filter((h) => !kind || h.kind === kind))];
      } else if (url.endsWith(`/salary-structures/${structureID}/history`))
        body = page([structure]);
      else if (url.endsWith("/hr/payroll/salary-structures")) body = page(empty ? [] : [structure]);
      if (init?.method === "PUT" || init?.method === "POST") {
        writes.push({ method: init.method, url, body: JSON.parse(String(init.body)) });
        [code, body] = reject
          ? [409, { error: "conflict", message: reject }]
          : [
              200,
              url.endsWith("/preview")
                ? preview
                : url.includes("structures")
                  ? structure
                  : heads[0],
            ];
      }
      return new Response(JSON.stringify(body), { status: code });
    }),
  );
  return { writes, reads };
}

const headsPanel = () => screen.findByRole("region", { name: "Salary heads" });
const structuresPanel = () => screen.findByRole("region", { name: "Salary structures" });

it("explains an empty setup and filters heads by kind", async () => {
  const { reads } = mock({ empty: true });
  const { user } = renderWithProviders(<PayrollSetup />);
  expect(
    await screen.findByText(/No salary heads are defined for this hospital/),
  ).toBeInTheDocument();
  expect(await screen.findByText(/Add salary heads first/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Add salary structure" })).toBeDisabled();
  await chooseOption(
    user,
    within(await headsPanel()).getByRole("combobox", { name: "Kind" }),
    "Deduction",
  );
  expect(await screen.findByText("No deduction heads.")).toBeInTheDocument();
  expect(reads.at(-1)).toMatch(/\/hr\/payroll\/salary-heads\?kind=deduction$/);
});

it("offers a retry when heads cannot be loaded", async () => {
  mock({ fail: true });
  renderWithProviders(<PayrollSetup />);
  expect(await screen.findByText("Could not load salary heads.")).toBeInTheDocument();
});

it("shows heads, structures and history read-only to readers", async () => {
  mock({ permissions: ["workforce.payroll.view"], access: "reader" });
  const { user } = renderWithProviders(<PayrollSetup />);
  const panel = await headsPanel();
  expect(
    await within(panel).findByText("40% of Synthetic Basic", { exact: false }),
  ).toBeInTheDocument();
  expect(within(panel).getByText(/cap ₹1,800\.00/)).toBeInTheDocument();
  expect(within(panel).getByText(/Read-only access/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add salary head" })).toBeNull();
  expect(screen.queryByRole("button", { name: /^Edit / })).toBeNull();
  const structures = await structuresPanel();
  expect(
    await within(structures).findByText(/Synthetic Basic ₹30,000\.00 · Synthetic HRA/),
  ).toBeInTheDocument();
  expect(await within(structures).findByText(/Synthetic Grade A · v3/)).toBeInTheDocument();
  await user.click(within(panel).getByRole("button", { name: "History of Synthetic Basic" }));
  const history = await screen.findByRole("region", { name: "Synthetic Basic history" });
  expect(await within(history).findByText("v1 · effective 2026-10-01")).toBeInTheDocument();
  expect(within(history).getByText("v2 · effective 2026-11-01")).toBeInTheDocument();
});

it("keeps the setup read-only where the hospital grants no manager access", async () => {
  mock({ access: "reader" });
  renderWithProviders(<PayrollSetup />);
  expect(await within(await headsPanel()).findByText(/Read-only access/)).toBeInTheDocument();
  expect(await screen.findByText(/Synthetic Nurse Pay · SYN-NURSE/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add salary head" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Add salary structure" })).toBeNull();
  expect(screen.queryByRole("button", { name: /^Edit / })).toBeNull();
});

it("refuses an incomplete head and saves a percentage of a reviewed base", async () => {
  const { writes } = mock();
  const { user } = renderWithProviders(<PayrollSetup />);
  await user.click(await screen.findByRole("button", { name: "Add salary head" }));
  const form = screen.getByRole("form", { name: "Add salary head" });
  await user.click(within(form).getByRole("button", { name: "Save salary head" }));
  expect(await within(form).findByText("Enter a name.")).toBeInTheDocument();
  expect(within(form).getByText("Enter a payslip label.")).toBeInTheDocument();
  expect(
    within(form).getByText("Use up to 40 letters, numbers, underscores or hyphens."),
  ).toBeInTheDocument();
  expect(within(form).getByText("Confirm that you reviewed the head.")).toBeInTheDocument();
  expect(within(form).getByLabelText("Proration")).toBeInTheDocument();
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Basis" }),
    "Percentage of other heads",
  );
  expect(within(form).queryByLabelText("Proration")).toBeNull();
  await user.type(within(form).getByLabelText("Code"), "syn-da");
  await user.type(within(form).getByLabelText("Name"), "Synthetic DA");
  await user.type(within(form).getByLabelText("Payslip label"), "DA");
  await user.click(within(form).getByRole("button", { name: "Save salary head" }));
  expect(await within(form).findByText("Choose at least one base.")).toBeInTheDocument();
  expect(within(form).getByText(/Enter a percentage above 0/)).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.type(within(form).getByLabelText("Rate (%)"), "10.5");
  expect(within(form).queryByRole("checkbox", { name: "Synthetic PF · SYN-PF" })).toBeNull();
  await user.click(within(form).getByRole("checkbox", { name: "Synthetic Basic · SYN-BASIC" }));
  await user.click(within(form).getByRole("checkbox", { name: "Jun" }));
  await user.click(within(form).getByRole("checkbox", { name: /I reviewed this head/ }));
  await user.click(within(form).getByRole("button", { name: "Save salary head" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "PUT",
    url: expect.stringMatching(
      /\/hospitals\/1{8}-[^/]+\/hr\/payroll\/salary-heads\/[0-9a-f-]{36}$/,
    ),
    body: {
      expected_version: 0,
      kind: "earning",
      code: "SYN-DA",
      name: "Synthetic DA",
      active: true,
      basis: "percent",
      rate_ppm: 105000,
      base_head_ids: [basic],
      proration: "none",
      minimum_payable_half_days: 0,
      months: [1, 2, 3, 4, 5, 7, 8, 9, 10, 11, 12],
      rounding_mode: "nearest",
      rounding_unit_paise: 100,
      in_gross: true,
      in_ctc: true,
      arrears_eligible: true,
      taxability: "taxable",
      wage_class: "wages",
      payslip_label: "DA",
      payslip_order: 10,
      show_when_zero: false,
      effective_on: today,
      reviewed: true,
    },
  });
  expect(await screen.findByText("Salary head saved")).toBeInTheDocument();
});

it("saves a deduction outside gross, tax and wages whatever was chosen as an earning", async () => {
  const { writes } = mock();
  const { user } = renderWithProviders(<PayrollSetup />);
  await user.click(await screen.findByRole("button", { name: "Add salary head" }));
  const form = screen.getByRole("form", { name: "Add salary head" });
  await chooseOption(user, within(form).getByRole("combobox", { name: "Income tax" }), "Exempt");
  expect(within(form).getByLabelText("Exemption basis")).toBeInTheDocument();
  await chooseOption(user, within(form).getByRole("combobox", { name: "Kind" }), "Deduction");
  expect(within(form).queryByRole("combobox", { name: "Income tax" })).toBeNull();
  expect(within(form).getByText("Outside remuneration")).toBeInTheDocument();
  expect(within(form).getByRole("checkbox", { name: "Counts toward gross" })).toBeDisabled();
  expect(within(form).getByRole("checkbox", { name: "Counts toward CTC" })).not.toBeChecked();
  await user.type(within(form).getByLabelText("Code"), "SYN-LWF");
  await user.type(within(form).getByLabelText("Name"), "Synthetic welfare fund");
  await user.type(within(form).getByLabelText("Payslip label"), "LWF");
  await user.click(within(form).getByRole("checkbox", { name: /I reviewed this head/ }));
  await user.click(within(form).getByRole("button", { name: "Save salary head" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]!.body).toMatchObject({
    kind: "deduction",
    basis: "fixed",
    proration: "calendar_days",
    in_gross: false,
    in_ctc: false,
    taxability: "not_applicable",
    wage_class: "outside",
  });
  expect(writes[0]!.body).not.toHaveProperty("tax_note");
});

it("locks kind and code on edit and keeps input after a conflict", async () => {
  const { writes } = mock({ reject: "The salary head changed since you loaded it." });
  const { user } = renderWithProviders(<PayrollSetup />);
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Basic" }));
  const form = screen.getByRole("form", { name: "Edit Synthetic Basic" });
  expect(within(form).getByLabelText("Code")).toBeDisabled();
  expect(within(form).getByLabelText("Code")).toHaveValue("SYN-BASIC");
  expect(within(form).getByRole("combobox", { name: "Kind" })).toBeDisabled();
  const name = within(form).getByLabelText("Name");
  await user.clear(name);
  await user.type(name, "Synthetic Basic Pay");
  await user.click(within(form).getByRole("checkbox", { name: /I reviewed this head/ }));
  await user.click(within(form).getByRole("button", { name: "Save salary head" }));
  expect(
    await within(form).findByText(
      "The salary head changed since you loaded it. Cancel and reload to see the current head. Your input is preserved.",
    ),
  ).toBeInTheDocument();
  expect(writes[0]).toMatchObject({
    url: expect.stringMatching(new RegExp(`/salary-heads/${basic}$`)),
    body: {
      expected_version: 2,
      code: "SYN-BASIC",
      name: "Synthetic Basic Pay",
      proration: "calendar_days",
    },
  });
  expect(name).toHaveValue("Synthetic Basic Pay");
});

it("asks for amounts on fixed heads only and saves a scoped structure", async () => {
  const { writes } = mock();
  const { user } = renderWithProviders(<PayrollSetup />);
  const panel = await structuresPanel();
  await waitFor(() =>
    expect(within(panel).getByRole("button", { name: "Add salary structure" })).toBeEnabled(),
  );
  await user.click(within(panel).getByRole("button", { name: "Add salary structure" }));
  const form = screen.getByRole("form", { name: "Add salary structure" });
  await user.type(within(form).getByLabelText("Code"), "syn-staff");
  await user.type(within(form).getByLabelText("Name"), "Synthetic Staff Pay");
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Grade" }),
    "Synthetic Grade A",
  );
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Head 1" }),
    "Synthetic Basic · Earning",
  );
  await user.click(within(form).getByRole("button", { name: "Add head" }));
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Head 2" }),
    "Synthetic HRA · Earning",
  );
  expect(within(form).getByText("Computed from its bases")).toBeInTheDocument();
  expect(within(form).getAllByLabelText("Monthly amount (₹)")).toHaveLength(1);
  await user.click(within(form).getByRole("button", { name: "Save salary structure" }));
  expect(await within(form).findByText(/Enter rupees with up to 2 decimals/)).toBeInTheDocument();
  expect(within(form).getByText("Confirm that you reviewed the structure.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.type(within(form).getByLabelText("Monthly amount (₹)"), "30,000.50");
  await user.click(within(form).getByRole("checkbox", { name: /I reviewed the heads/ }));
  await user.click(within(form).getByRole("button", { name: "Save salary structure" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "PUT",
    url: expect.stringMatching(/\/hr\/payroll\/salary-structures\/[0-9a-f-]{36}$/),
    body: {
      expected_version: 0,
      code: "SYN-STAFF",
      name: "Synthetic Staff Pay",
      description: "",
      active: true,
      grade_id: grade,
      lines: [{ head_id: basic, amount_paise: 3000050 }, { head_id: hra }],
      effective_on: today,
      reviewed: true,
    },
  });
});

it("bounds preview days by the month and shows totals and the wage test", async () => {
  const { writes } = mock();
  const { user } = renderWithProviders(<PayrollSetup />);
  await user.click(await screen.findByRole("button", { name: "Preview Synthetic Nurse Pay" }));
  const region = screen.getByRole("region", { name: "Synthetic Nurse Pay preview" });
  const on = within(region).getByLabelText("Month containing");
  await user.clear(on);
  await user.type(on, "2026-02-10");
  await user.type(within(region).getByLabelText("Payable days"), "28.5");
  await user.click(within(region).getByRole("button", { name: "Compute preview" }));
  expect(
    await within(region).findByText("Use no more than the 28 days in that month."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  const days = within(region).getByLabelText("Payable days");
  await user.clear(days);
  await user.type(days, "28");
  await user.click(within(region).getByRole("button", { name: "Compute preview" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    method: "POST",
    url: expect.stringMatching(new RegExp(`/salary-structures/${structureID}/preview$`)),
    body: { on: "2026-02-10", payable_half_days: 56 },
  });
  const row = (await within(region).findByText("PF")).closest("tr")!;
  expect(within(row).getByText("₹1,800.00")).toBeInTheDocument();
  expect(within(region).queryByText("Synthetic hidden line")).toBeNull();
  expect(within(region).getByText("₹40,200.00")).toBeInTheDocument();
  expect(within(region).getByText(/28\.5714% of ₹42,000\.00/)).toBeInTheDocument();
  expect(within(region).queryByText(/net pay is negative/)).toBeNull();
});
