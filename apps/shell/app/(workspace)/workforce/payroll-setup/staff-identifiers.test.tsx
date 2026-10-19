import "@/test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "@/test/select";
import PayrollSetup from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const staffID = "22222222-2222-4222-8222-222222222222";
const current = "66666666-6666-4666-8666-666666666666";
const proposal = "77777777-7777-4777-8777-777777777777";
const stamp = "2026-10-16T00:00:00Z";
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());

// Synthetic identifiers only; none belongs to a real person.
const member = {
  id: staffID,
  display_name: "Synthetic Nurse",
  employee_code: "SYN-1",
  employment_status: "active",
};
const revision = (over: object) => ({
  id: current,
  staff_id: staffID,
  sequence: 1,
  base_version: 0,
  status: "approved",
  approved_version: 1,
  reason: "Synthetic onboarding",
  bank_account_masked: "XXXX2333",
  bank_ifsc: "SYNT0000001",
  bank_holder_name: "Synthetic Holder",
  pan_masked: "XXXXXX234Z",
  proposed_by_subject: "user-maker",
  decided_by_subject: "user-approver",
  decided_at: stamp,
  decision_note: "",
  created_at: stamp,
  ...over,
});
const identifiers = (pending: boolean) => ({
  staff_id: staffID,
  version: 1,
  current: revision({}),
  ...(pending && {
    pending: revision({
      id: proposal,
      sequence: 2,
      base_version: 1,
      status: "pending",
      approved_version: undefined,
      reason: "Synthetic bank change",
      bank_account_masked: "XXXX7666",
      uan_masked: "XXXXXXXX0001",
      decided_by_subject: undefined,
      decided_at: undefined,
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
  reveal = [200, {}] as [number, unknown],
}: {
  permissions?: string[];
  subject?: string;
  record?: unknown;
  status?: number;
  reveal?: [number, unknown];
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
      else if (url.endsWith(`/staff/${staffID}/salary-assignment`))
        [code, body] = [404, { error: "not_found", message: "x" }];
      else if (url.endsWith(`/staff/${staffID}/identifiers`))
        [code, body] =
          status !== 200
            ? [status, { error: status === 404 ? "not_found" : "internal", message: "x" }]
            : [200, record];
      if (init?.method === "PUT" || init?.method === "POST") {
        writes.push({ url, body: JSON.parse(String(init.body)) });
        [code, body] = url.endsWith("/reveal") ? reveal : [200, identifiers(false)];
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
  return within(section).findByRole("region", { name: "Bank and tax identifiers" });
};

it("shows masked values only and offers no reveal without its permission", async () => {
  mock({ permissions: ["workforce.payroll.view"], record: identifiers(false) });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  expect(
    await within(section).findByText("XXXX2333 · SYNT0000001 · Synthetic Holder"),
  ).toBeInTheDocument();
  expect(within(section).getByText("XXXXXX234Z")).toBeInTheDocument();
  expect(within(section).getByText("Not recorded")).toBeInTheDocument();
  expect(within(section).queryByRole("button", { name: /Show full/ })).toBeNull();
  expect(within(section).queryByRole("button", { name: /Propose identifier/ })).toBeNull();
});

it("says nothing is recorded and offers a retry when loading fails", async () => {
  mock({ status: 404 });
  const first = renderWithProviders(<PayrollSetup />);
  const empty = await choose(first.user);
  expect(
    await within(empty).findByText("No bank account, PAN or UAN approved for Synthetic Nurse."),
  ).toBeInTheDocument();
  first.unmount();
  mock({ status: 500 });
  const second = renderWithProviders(<PayrollSetup />);
  const failed = await choose(second.user);
  expect(
    await within(failed).findByText("Could not load this staff member's identifiers."),
  ).toBeInTheDocument();
  expect(within(failed).queryByRole("button", { name: /Propose identifiers/ })).toBeNull();
});

it("validates each field inline and sends the normalised proposal", async () => {
  const writes = mock({ status: 404 });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  await user.click(await within(section).findByRole("button", { name: "Propose identifiers" }));
  const form = within(section).getByRole("form", { name: "Propose identifiers" });
  expect(within(form).queryByRole("option", { name: "Remove" })).toBeNull();
  await user.click(within(form).getByRole("button", { name: "Propose identifiers" }));
  expect(
    await within(form).findByText("Replace or remove at least one identifier."),
  ).toBeInTheDocument();
  expect(within(form).getByText("Enter a reason.")).toBeInTheDocument();
  await chooseOption(user, within(form).getByRole("combobox", { name: "Bank account" }), "Replace");
  await chooseOption(user, within(form).getByRole("combobox", { name: "PAN" }), "Replace");
  await user.type(within(form).getByLabelText("Account number"), "1234");
  await user.type(within(form).getByLabelText("IFSC"), "SYNT1000001");
  await user.type(within(form).getByLabelText("New PAN"), "AAACZ1234Z");
  await user.type(within(form).getByLabelText("Reason"), "Synthetic onboarding");
  await user.click(within(form).getByRole("button", { name: "Propose identifiers" }));
  expect(
    await within(form).findByText("Enter the 9 to 18 digit account number."),
  ).toBeInTheDocument();
  expect(
    within(form).getByText("Enter the 11-character IFSC, such as ABCD0123456."),
  ).toBeInTheDocument();
  expect(
    within(form).getByText("Enter the account holder's name, up to 150 characters."),
  ).toBeInTheDocument();
  expect(
    within(form).getByText("Enter the individual's 10-character PAN; its fourth letter is P."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.clear(within(form).getByLabelText("Account number"));
  await user.type(within(form).getByLabelText("Account number"), "0001 1122 2333");
  await user.clear(within(form).getByLabelText("IFSC"));
  await user.type(within(form).getByLabelText("IFSC"), "synt0000001");
  await user.type(within(form).getByLabelText("Account holder name"), "Synthetic Holder");
  await user.clear(within(form).getByLabelText("New PAN"));
  await user.type(within(form).getByLabelText("New PAN"), "aaapz1234z");
  await user.click(within(form).getByRole("button", { name: "Propose identifiers" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: expect.stringMatching(new RegExp(`/hr/payroll/staff/${staffID}/identifiers$`)),
    body: {
      expected_version: 0,
      reason: "Synthetic onboarding",
      bank: {
        action: "set",
        account_number: "000111222333",
        ifsc: "SYNT0000001",
        holder_name: "Synthetic Holder",
      },
      pan: { action: "set", value: "AAAPZ1234Z" },
      uan: { action: "keep" },
    },
  });
  expect(await screen.findByText("Identifiers proposed")).toBeInTheDocument();
});

it("reveals a value only with a reason and hides it again", async () => {
  const writes = mock({
    permissions: ["workforce.payroll.view", "workforce.payroll.identifiers.reveal"],
    subject: "user-auditor",
    record: identifiers(false),
    reveal: [200, { revision_id: current, field: "pan", value: "AAAPZ1234Z" }],
  });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  expect(within(section).queryByRole("button", { name: "Show full UAN" })).toBeNull();
  await user.click(await within(section).findByRole("button", { name: "Show full PAN" }));
  const form = within(section).getByRole("form", { name: "Show full PAN" });
  await user.click(within(form).getByRole("button", { name: "Show" }));
  expect(await within(form).findByText("Enter why you need to see it.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.type(within(form).getByLabelText("Why you need to see it"), "Synthetic payout check");
  await user.click(within(form).getByRole("button", { name: "Show" }));
  expect(await within(section).findByRole("status", { name: "Full PAN" })).toHaveTextContent(
    "AAAPZ1234Z",
  );
  expect(writes[0]).toEqual({
    url: expect.stringMatching(/\/identifiers\/reveal$/),
    body: { revision_id: current, field: "pan", reason: "Synthetic payout check" },
  });
  await user.click(within(section).getByRole("button", { name: "Hide PAN" }));
  expect(within(section).queryByText("AAAPZ1234Z")).toBeNull();
  expect(within(section).getByRole("button", { name: "Show full PAN" })).toHaveFocus();
});

it("explains a failed reveal and keeps the value hidden", async () => {
  mock({
    permissions: ["workforce.payroll.view", "workforce.payroll.identifiers.reveal"],
    subject: "user-auditor",
    record: identifiers(false),
    reveal: [
      503,
      {
        error: "identifier_vault_unavailable",
        message: "Bank and tax identifiers cannot be changed or viewed right now.",
      },
    ],
  });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  await user.click(await within(section).findByRole("button", { name: "Show full Bank account" }));
  const form = within(section).getByRole("form", { name: "Show full Bank account" });
  await user.type(within(form).getByLabelText("Why you need to see it"), "Synthetic payout check");
  await user.click(within(form).getByRole("button", { name: "Show" }));
  expect(await within(form).findByRole("alert")).toHaveTextContent(
    "Bank and tax identifiers cannot be changed or viewed right now.",
  );
  expect(within(section).queryByRole("status", { name: "Full Bank account" })).toBeNull();
});

it("lets a second person check proposed values before approving", async () => {
  const writes = mock({
    permissions: [
      "workforce.payroll.view",
      "workforce.payroll.approve",
      "workforce.payroll.identifiers.reveal",
    ],
    subject: "user-approver",
    record: identifiers(true),
    reveal: [200, { revision_id: proposal, field: "uan", value: "100000000001" }],
  });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  expect(await within(section).findByText(/Change proposed by user-maker/)).toBeInTheDocument();
  await user.click(within(section).getByRole("button", { name: "Review identifiers" }));
  const decision = within(section).getByRole("region", { name: "Identifier decision" });
  await user.click(within(decision).getByRole("button", { name: "Show full UAN" }));
  const reveal = within(decision).getByRole("form", { name: "Show full UAN" });
  await user.type(within(reveal).getByLabelText("Why you need to see it"), "Synthetic check");
  await user.click(within(reveal).getByRole("button", { name: "Show" }));
  expect(await within(decision).findByRole("status", { name: "Full UAN" })).toHaveTextContent(
    "100000000001",
  );
  expect(writes[0]?.body).toEqual({
    revision_id: proposal,
    field: "uan",
    reason: "Synthetic check",
  });

  const form = within(decision).getByRole("form", { name: "Decide identifier proposal" });
  await user.click(within(form).getByRole("button", { name: "Reject identifiers" }));
  expect(await within(form).findByText("Give a reason for rejecting.")).toBeInTheDocument();
  expect(writes).toHaveLength(1);
  await user.click(within(form).getByRole("button", { name: "Approve identifiers" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]).toEqual({
    url: expect.stringMatching(/\/identifiers\/decision$/),
    body: { proposal_id: proposal, expected_version: 1, decision: "approve", note: "" },
  });
  expect(await screen.findByText("Identifiers approved")).toBeInTheDocument();
});

it("keeps the proposer to withdrawing and never offers them a reveal of their own values", async () => {
  const writes = mock({
    permissions: [
      "workforce.payroll.view",
      "workforce.payroll.configure",
      "workforce.payroll.approve",
      "workforce.payroll.identifiers.reveal",
    ],
    record: identifiers(true),
  });
  const { user } = renderWithProviders(<PayrollSetup />);
  const section = await choose(user);
  expect(await within(section).findByText(/Change proposed by you/)).toBeInTheDocument();
  expect(within(section).queryByRole("button", { name: /Propose identifier/ })).toBeNull();
  await user.click(within(section).getByRole("button", { name: "Withdraw identifiers" }));
  const decision = within(section).getByRole("region", { name: "Identifier decision" });
  expect(within(decision).queryByRole("button", { name: /Show full/ })).toBeNull();
  const form = within(decision).getByRole("form", { name: "Withdraw identifier proposal" });
  expect(within(form).queryByRole("button", { name: "Approve identifiers" })).toBeNull();
  await user.click(within(form).getByRole("button", { name: "Withdraw proposal" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: expect.stringMatching(/\/identifiers\/withdraw$/),
    body: { proposal_id: proposal, note: "" },
  });
});
