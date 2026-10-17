import "../../../test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { taxYearsAround } from "@helivanta/api";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../../test/select";
import PayrollSetup from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const staffID = "22222222-2222-4222-8222-222222222222";
const current = "66666666-6666-4666-8666-666666666666";
const proposal = "77777777-7777-4777-8777-777777777777";
const stamp = "2026-10-16T00:00:00Z";
const [, thisYear, nextYear] = taxYearsAround(new Date().toISOString().slice(0, 10));
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());

// Synthetic names and PANs only; none belongs to a real landlord or lender.
const member = {
  id: staffID,
  display_name: "Synthetic Nurse",
  employee_code: "SYN-1",
  employment_status: "active",
};
const revision = (over: object) => ({
  id: current,
  staff_id: staffID,
  tax_year: thisYear,
  sequence: 1,
  base_version: 0,
  status: "approved",
  approved_version: 1,
  reason: "Synthetic Form 124",
  hra_metro: true,
  claims: [{ code: "SAVINGS", amount_paise: 15000000 }],
  parties: [
    {
      role: "landlord",
      position: 1,
      name: "Synthetic Landlord",
      address: "1 Synthetic Lane",
      rent_paise: 9000000,
      pan_masked: "XXXXXX234A",
    },
  ],
  proposed_by_subject: "user-maker",
  decided_by_subject: "user-approver",
  decided_at: stamp,
  decision_note: "Synthetic receipts checked",
  created_at: stamp,
  ...over,
});
const declaration = (pending: boolean) => ({
  staff_id: staffID,
  tax_year: thisYear,
  version: 1,
  current: revision({}),
  ...(pending && {
    pending: revision({
      id: proposal,
      sequence: 2,
      base_version: 1,
      status: "pending",
      approved_version: undefined,
      reason: "Synthetic loan added",
      home_loan_interest_paise: 20000000,
      parties: [
        ...revision({}).parties,
        {
          role: "lender",
          position: 1,
          name: "Synthetic Bank",
          address: "2 Synthetic Road",
          pan_masked: "XXXXXX012C",
        },
      ],
      decided_by_subject: undefined,
      decided_at: undefined,
      decision_note: "",
    }),
  }),
  created_at: stamp,
  updated_at: stamp,
});

function mock({
  permissions = ["workforce.payroll.view", "workforce.payroll.configure"],
  subject = "user-maker",
  record = null as unknown,
  status = 200,
  write = [200, declaration(false)] as [number, unknown],
}: {
  permissions?: string[];
  subject?: string;
  record?: unknown;
  status?: number;
  write?: [number, unknown];
} = {}) {
  const writes: { url: string; body: unknown }[] = [];
  const reads: string[] = [];
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
      else if (
        url.endsWith(`/staff/${staffID}/salary-assignment`) ||
        url.endsWith(`/staff/${staffID}/identifiers`)
      )
        [code, body] = [404, { error: "not_found", message: "x" }];
      else if (url.endsWith(`/staff/${staffID}/tax-declarations/${nextYear}`)) {
        reads.push(url);
        [code, body] = [404, { error: "not_found", message: "x" }];
      } else if (url.endsWith(`/staff/${staffID}/tax-declarations/${thisYear}`)) {
        reads.push(url);
        [code, body] =
          status !== 200
            ? [status, { error: status === 404 ? "not_found" : "internal", message: "x" }]
            : [200, record];
      }
      if (init?.method === "PUT" || init?.method === "POST") {
        writes.push({ url, body: JSON.parse(String(init.body)) });
        [code, body] = write;
      }
      return new Response(JSON.stringify(body), { status: code });
    }),
  );
  return { writes, reads };
}

const choose = async (user: Awaited<ReturnType<typeof renderWithProviders>>["user"]) => {
  const section = await screen.findByRole("region", { name: "Staff pay" });
  await chooseOption(
    user,
    await within(section).findByRole("combobox", { name: "Staff member" }),
    "Synthetic Nurse · SYN-1",
  );
  return within(section).findByRole("region", { name: "Tax declarations" });
};

it("shows the masked declaration and offers no reveal or change without permission", async () => {
  mock({ permissions: ["workforce.payroll.view"], record: declaration(false) });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  expect(await within(section).findByText("SAVINGS ₹1,50,000.00")).toBeInTheDocument();
  expect(within(section).getByText("Claimed · metro city")).toBeInTheDocument();
  expect(within(section).getByText("Not claimed")).toBeInTheDocument();
  const party = within(section).getByRole("list", { name: "Landlords and lenders" });
  expect(within(party).getByText("Synthetic Landlord")).toBeInTheDocument();
  expect(within(party).getByText("Rent paid ₹90,000.00")).toBeInTheDocument();
  expect(within(party).getByText("PAN XXXXXX234A")).toBeInTheDocument();
  expect(within(section).queryByRole("button", { name: /Show full/ })).toBeNull();
  expect(within(section).queryByRole("button", { name: /Propose declaration/ })).toBeNull();
});

it("says nothing is declared, switches year, and offers a retry when loading fails", async () => {
  const { reads } = mock({ status: 404 });
  const first = renderWithProviders(<PayrollSetup />);
  const empty = await choose(first.user);
  expect(
    await within(empty).findByText(`No declaration approved for Synthetic Nurse in ${thisYear}.`),
  ).toBeInTheDocument();
  await chooseOption(
    first.user,
    within(empty).getByRole("combobox", { name: "Tax year" }),
    nextYear!,
  );
  expect(
    await within(empty).findByText(`No declaration approved for Synthetic Nurse in ${nextYear}.`),
  ).toBeInTheDocument();
  expect(reads.some((u) => u.endsWith(`/tax-declarations/${nextYear}`))).toBe(true);
  first.unmount();
  mock({ status: 500 });
  const second = renderWithProviders(<PayrollSetup />);
  const failed = await choose(second.user);
  expect(
    await within(failed).findByText("Could not load this staff member's tax declarations."),
  ).toBeInTheDocument();
  expect(within(failed).queryByRole("button", { name: /Propose declaration/ })).toBeNull();
});

it("validates every row inline and sends the normalised proposal", async () => {
  const { writes } = mock({ status: 404 });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  await user.click(await within(section).findByRole("button", { name: "Propose declaration" }));
  const form = within(section).getByRole("form", { name: "Propose declaration" });
  await user.click(within(form).getByRole("button", { name: "Add claim" }));
  await user.type(within(form).getByLabelText("Claim 1 code"), "A B");
  await user.click(within(form).getByRole("checkbox", { name: "Claim house rent allowance" }));
  await user.click(within(form).getByRole("checkbox", { name: "Claim home loan interest" }));
  await user.click(within(form).getByRole("button", { name: "Propose declaration" }));
  expect(await within(form).findByText("Enter a reason.")).toBeInTheDocument();
  expect(
    within(form).getByText("Use up to 40 letters, numbers, underscores or hyphens."),
  ).toBeInTheDocument();
  expect(within(form).getByText("Enter an amount up to ₹1,000 crore.")).toBeInTheDocument();
  expect(within(form).getAllByText("Enter the name, up to 150 characters.")).toHaveLength(2);
  expect(
    within(form).getByText("Enter the rent paid this year, up to ₹1 crore."),
  ).toBeInTheDocument();
  expect(
    within(form).getByText("Enter the interest payable this year, up to ₹1 crore."),
  ).toBeInTheDocument();
  expect(
    within(form).getByText("Enter the 10-character PAN, such as ABCDE1234F."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);

  await user.clear(within(form).getByLabelText("Claim 1 code"));
  await user.type(within(form).getByLabelText("Claim 1 code"), " savings ");
  await user.type(within(form).getByLabelText("Claim 1 amount (₹)"), "1,50,000");
  const landlord = within(form).getByRole("group", { name: "Landlord 1" });
  await user.type(within(landlord).getByLabelText("Name"), "Synthetic Landlord");
  await user.type(within(landlord).getByLabelText("Address"), "1 Synthetic Lane");
  await user.type(within(landlord).getByLabelText("Rent paid this year (₹)"), "90000");
  await user.type(within(landlord).getByLabelText("Landlord 1 PAN"), "synfa1234a");
  await user.click(within(form).getByRole("checkbox", { name: "Rented home is in a metro city" }));
  await user.type(within(form).getByLabelText("Interest payable this year (₹)"), "2,00,000");
  const lender = within(form).getByRole("group", { name: "Lender 1" });
  expect(within(lender).queryByRole("option", { name: "Keep the PAN on record" })).toBeNull();
  await user.type(within(lender).getByLabelText("Name"), "Synthetic Bank");
  await user.type(within(lender).getByLabelText("Address"), "2 Synthetic Road");
  await user.type(within(form).getByLabelText("Reason"), "Synthetic Form 124");
  await user.click(within(form).getByRole("button", { name: "Propose declaration" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: expect.stringMatching(
      new RegExp(`/hr/payroll/staff/${staffID}/tax-declarations/${thisYear}$`),
    ),
    body: {
      expected_version: 0,
      reason: "Synthetic Form 124",
      claims: [{ code: "SAVINGS", amount_paise: 15000000 }],
      hra: {
        metro: true,
        landlords: [
          {
            name: "Synthetic Landlord",
            address: "1 Synthetic Lane",
            rent_paise: 9000000,
            pan: { action: "set", value: "SYNFA1234A" },
          },
        ],
      },
      home_loan: {
        interest_paise: 20000000,
        lenders: [{ name: "Synthetic Bank", address: "2 Synthetic Road", pan: { action: "none" } }],
      },
    },
  });
  expect(await screen.findByText("Declaration proposed")).toBeInTheDocument();
});

it("starts a change from the declaration in force and explains a missing landlord PAN", async () => {
  const { writes } = mock({
    record: declaration(false),
    write: [
      400,
      {
        error: "landlord_pan_required",
        message: "Give every landlord's PAN: rent paid crosses the threshold in force.",
      },
    ],
  });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  await user.click(
    await within(section).findByRole("button", { name: "Propose declaration change" }),
  );
  const form = within(section).getByRole("form", { name: "Propose declaration change" });
  expect(within(form).getByLabelText("Claim 1 code")).toHaveValue("SAVINGS");
  const landlord = within(form).getByRole("group", { name: "Landlord 1" });
  expect(within(landlord).getByLabelText("Rent paid this year (₹)")).toHaveValue("90000");
  expect(within(landlord).getByRole("combobox", { name: "PAN" })).toHaveTextContent(
    "Keep the PAN on record",
  );
  await chooseOption(user, within(landlord).getByRole("combobox", { name: "PAN" }), "No PAN");
  await user.type(within(form).getByLabelText("Reason"), "Synthetic PAN removed");
  await user.click(within(form).getByRole("button", { name: "Propose declaration" }));
  expect(await within(form).findByRole("alert")).toHaveTextContent(
    "Give every landlord's PAN: rent paid crosses the threshold in force.",
  );
  expect(writes[0]?.body).toMatchObject({
    expected_version: 1,
    hra: { landlords: [{ pan: { action: "none" } }] },
  });
});

it("reveals a party PAN only with a reason and hides it again", async () => {
  const { writes } = mock({
    permissions: ["workforce.payroll.view", "workforce.payroll.identifiers.reveal"],
    subject: "user-auditor",
    record: declaration(false),
    write: [200, { revision_id: current, role: "landlord", position: 1, value: "SYNFA1234A" }],
  });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  await user.click(
    await within(section).findByRole("button", { name: "Show full Landlord 1 PAN" }),
  );
  const form = within(section).getByRole("form", { name: "Show full Landlord 1 PAN" });
  await user.click(within(form).getByRole("button", { name: "Show" }));
  expect(await within(form).findByText("Enter why you need to see it.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.type(within(form).getByLabelText("Why you need to see it"), "Synthetic Form 16 check");
  await user.click(within(form).getByRole("button", { name: "Show" }));
  expect(
    await within(section).findByRole("status", { name: "Full Landlord 1 PAN" }),
  ).toHaveTextContent("SYNFA1234A");
  expect(writes[0]).toEqual({
    url: expect.stringMatching(new RegExp(`/tax-declarations/${thisYear}/reveal$`)),
    body: {
      revision_id: current,
      role: "landlord",
      position: 1,
      reason: "Synthetic Form 16 check",
    },
  });
  await user.click(within(section).getByRole("button", { name: "Hide Landlord 1 PAN" }));
  expect(within(section).queryByText("SYNFA1234A")).toBeNull();
  expect(within(section).getByRole("button", { name: "Show full Landlord 1 PAN" })).toHaveFocus();
});

it("lets a second person check proposed PANs, needs a note to reject, and approves", async () => {
  const { writes } = mock({
    permissions: [
      "workforce.payroll.view",
      "workforce.payroll.approve",
      "workforce.payroll.identifiers.reveal",
    ],
    subject: "user-approver",
    record: declaration(true),
  });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  expect(await within(section).findByText(/Change proposed by user-maker/)).toBeInTheDocument();
  await user.click(within(section).getByRole("button", { name: "Review declaration" }));
  const decision = within(section).getByRole("region", { name: "Declaration decision" });
  expect(within(decision).getByText("₹2,00,000.00")).toBeInTheDocument();
  expect(
    within(decision).getByRole("button", { name: "Show full Lender 1 PAN" }),
  ).toBeInTheDocument();
  const form = within(decision).getByRole("form", { name: "Decide declaration proposal" });
  await user.click(within(form).getByRole("button", { name: "Reject declaration" }));
  expect(await within(form).findByText("Give a reason for rejecting.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.type(
    within(form).getByLabelText(/Evidence checked/),
    "Synthetic loan certificate seen",
  );
  await user.click(within(form).getByRole("button", { name: "Approve declaration" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: expect.stringMatching(new RegExp(`/tax-declarations/${thisYear}/decision$`)),
    body: {
      proposal_id: proposal,
      expected_version: 1,
      decision: "approve",
      note: "Synthetic loan certificate seen",
    },
  });
  expect(await screen.findByText("Declaration approved")).toBeInTheDocument();
});

it("keeps the proposer to withdrawing and never offers them a reveal of their own values", async () => {
  const { writes } = mock({
    permissions: [
      "workforce.payroll.view",
      "workforce.payroll.configure",
      "workforce.payroll.approve",
      "workforce.payroll.identifiers.reveal",
    ],
    record: declaration(true),
  });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  expect(await within(section).findByText(/Change proposed by you/)).toBeInTheDocument();
  expect(within(section).queryByRole("button", { name: /Propose declaration/ })).toBeNull();
  await user.click(within(section).getByRole("button", { name: "Withdraw declaration" }));
  const decision = within(section).getByRole("region", { name: "Declaration decision" });
  expect(within(decision).queryByRole("button", { name: /Show full/ })).toBeNull();
  const form = within(decision).getByRole("form", { name: "Withdraw declaration proposal" });
  expect(within(form).queryByRole("button", { name: "Approve declaration" })).toBeNull();
  await user.click(within(form).getByRole("button", { name: "Withdraw proposal" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: expect.stringMatching(new RegExp(`/tax-declarations/${thisYear}/withdraw$`)),
    body: { proposal_id: proposal, note: "" },
  });
});
