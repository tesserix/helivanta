import { useState } from "react";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { useApiPagedQuery } from "@helivanta/api";
import type { AdmissionAccount } from "@helivanta/api";
import { chooseOption } from "../../test/select";
import { CashDesk } from "./cash-desk";
import { CashPayments } from "./cash-payments";

const hospital = {
  id: "0199e000-0000-7000-8000-00000000a001",
  name: "TEST General",
  access_level: "manager" as const,
};
const admission = "0199e000-0000-7000-8000-0000000000a9";
const counter = {
  id: "0199e000-0000-7000-8000-0000000000c1",
  code: "IPD-1",
  name: "IPD desk",
  modes: [] as string[],
};
const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const page = (data: unknown[]) => json(200, { data, page: { next_cursor: null, has_more: false } });
const shift = (over: Record<string, unknown> = {}) => ({
  id: "0199e000-0000-7000-8000-0000000000d1",
  hospital_id: hospital.id,
  counter_id: counter.id,
  counter_code: "IPD-1",
  cashier: "user-clerk",
  currency: "INR",
  opening_float_minor: 200000,
  opened_at: "2026-10-30T03:00:00Z",
  status: "open",
  expected_minor: null,
  declared_minor: null,
  variance_minor: null,
  closed_at: null,
  authorised_by: null,
  authorisation_reason: null,
  authorised_at: null,
  declared_counts: null,
  picked_up_minor: 0,
  counter_modes: [] as string[],
  ...over,
});
const pending = shift({
  status: "pending_authorisation",
  expected_minor: 500000,
  declared_minor: 490000,
  variance_minor: -10000,
  closed_at: "2026-10-30T11:00:00Z",
});

afterEach(() => vi.unstubAllGlobals());

const denominations = [
  { key: "note:50000", kind: "note", value_minor: 50000 },
  { key: "note:10000", kind: "note", value_minor: 10000 },
  { key: "coin:50", kind: "coin", value_minor: 50 },
];
const other = shift({
  id: "0199e000-0000-7000-8000-0000000000d2",
  cashier: "user-clerk2",
  counter_code: "OPD-1",
});
const pickup = {
  id: "0199e000-0000-7000-8000-0000000000e1",
  hospital_id: hospital.id,
  shift_id: other.id,
  counter_id: counter.id,
  kind: "pickup",
  amount_minor: 100000,
  currency: "INR",
  counts: { "note:50000": 2 },
  corrects_id: null,
  reason: null,
  recorded_by: "user-admin",
  recorded_at: "2026-10-30T05:00:00Z",
};

function server(opts: {
  permissions: string[];
  mine?: unknown;
  shifts?: unknown[];
  open?: unknown[];
  pickups?: unknown[];
  incoming?: unknown[];
  disputed?: unknown[];
  put?: unknown;
  counters?: unknown[];
  receipts?: unknown[];
}) {
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (url.includes("/iam/me/permissions"))
      return json(200, { subject: "user-clerk", data: opts.permissions });
    if (url.endsWith(`/billing/hospitals/${hospital.id}/access`))
      return json(200, { granted: true });
    if (init.method === "PUT") return json(200, opts.put ?? {});
    if (url.endsWith("/cash-shifts/mine"))
      return opts.mine
        ? json(200, opts.mine)
        : json(404, { error: "no_open_shift", message: "none" });
    if (url.includes("/cash-shifts?status=pending_authorisation")) return page(opts.shifts ?? []);
    if (url.includes("/cash-shifts?status=open")) return page(opts.open ?? []);
    if (url.includes("/pickups")) return page(opts.pickups ?? []);
    if (url.includes("/cash-handovers/incoming")) return page(opts.incoming ?? []);
    if (url.includes("/cash-handovers/disputed")) return page(opts.disputed ?? []);
    if (url.endsWith("/cash-denominations")) return json(200, denominations);
    if (url.includes("/cash-shifts")) return page([]);
    if (url.endsWith("/refund-policy"))
      return json(200, {
        hospital_id: hospital.id,
        threshold_minor: 1000000,
        currency: "INR",
        created_by: null,
        created_at: null,
      });
    if (url.includes("/counters")) return page(opts.counters ?? [counter]);
    if (url.includes("/receipts")) return page(opts.receipts ?? []);
    return page([]);
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
const handover = (over: Record<string, unknown> = {}) => ({
  id: "0199e000-0000-7000-8000-0000000000f1",
  hospital_id: hospital.id,
  from_shift_id: other.id,
  counter_id: counter.id,
  counter_code: "IPD-1",
  from_cashier: "user-clerk2",
  to_cashier: "user-clerk",
  currency: "INR",
  status: "offered",
  to_shift_id: null,
  agreed_counts: null,
  agreed_minor: null,
  settled_by: null,
  settlement_reason: null,
  offered_at: "2026-10-30T09:00:00Z",
  decided_at: null,
  settled_at: null,
  ...over,
});
const puts = (f: ReturnType<typeof server>) =>
  f.mock.calls.filter(([, init]) => init?.method === "PUT") as [string, RequestInit][];
const cashier = ["billing.account.read", "billing.cash.collect"];

describe("CashDesk", () => {
  it("opens a shift on a counter with a float", async () => {
    const fetcher = server({ permissions: cashier });
    const { user } = renderWithProviders(<CashDesk hospital={hospital} />);
    await chooseOption(user, await screen.findByLabelText("Counter"), "IPD-1 · IPD desk");
    await user.clear(screen.getByLabelText("Opening float (INR)"));
    await user.type(screen.getByLabelText("Opening float (INR)"), "2000");
    await user.click(screen.getByRole("button", { name: "Open shift" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(
      new RegExp(`/billing/hospitals/${hospital.id}/counters/${counter.id}/shifts/[0-9a-f-]{36}$`),
    );
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({ opening_float_minor: 200000 });
  });

  it("closes the shift blind, counting notes and coins", async () => {
    const fetcher = server({ permissions: cashier, mine: shift() });
    const { user } = renderWithProviders(<CashDesk hospital={hospital} />);
    expect(await screen.findByText(/Shift open/)).toBeInTheDocument();
    expect(screen.queryByText(/expected/i)).not.toBeInTheDocument();
    await user.type(await screen.findByLabelText("INR 500.00 notes"), "1.5");
    await user.click(screen.getByRole("button", { name: "Close shift" }));
    expect(await screen.findByText("Whole numbers only")).toBeInTheDocument();
    expect(puts(fetcher)).toHaveLength(0);
    await user.clear(screen.getByLabelText("INR 500.00 notes"));
    await user.type(screen.getByLabelText("INR 500.00 notes"), "2");
    await user.type(screen.getByLabelText("INR 0.50 coins"), "3");
    expect(screen.getByText("Counted: INR 1,001.50")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Close shift" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(`/cash-shifts/${shift().id}/close`);
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({
      counts: { "note:50000": 2, "coin:50": 3 },
    });
  });

  it("closes an empty drawer only when the cashier says it is empty", async () => {
    const fetcher = server({ permissions: cashier, mine: shift({ opening_float_minor: 0 }) });
    const { user } = renderWithProviders(<CashDesk hospital={hospital} />);
    await user.click(await screen.findByRole("button", { name: "Close shift" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Count the notes and coins, or confirm the drawer is empty",
    );
    expect(puts(fetcher)).toHaveLength(0);
    await user.click(screen.getByLabelText("The drawer is empty"));
    await user.click(screen.getByRole("button", { name: "Close shift" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({ counts: {} });
  });

  it("lets a supervisor record a counted pickup from someone else's open shift", async () => {
    const fetcher = server({
      permissions: ["billing.account.read", "billing.shift.authorise"],
      open: [other],
    });
    const { user } = renderWithProviders(<CashDesk hospital={hospital} />);
    await user.click(await screen.findByRole("button", { name: "Record a pickup from OPD-1" }));
    await user.click(await screen.findByRole("button", { name: "Record pickup" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Count at least one note or coin");
    expect(puts(fetcher)).toHaveLength(0);
    await user.type(screen.getByLabelText("INR 100.00 notes"), "4");
    await user.click(screen.getByRole("button", { name: "Record pickup" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(
      new RegExp(`/cash-shifts/${other.id}/pickups/[0-9a-f-]{36}$`),
    );
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({ counts: { "note:10000": 4 } });
  });

  it("offers no pickup from the supervisor's own shift", async () => {
    server({
      permissions: [...cashier, "billing.shift.authorise"],
      mine: shift(),
      open: [shift()],
    });
    renderWithProviders(<CashDesk hospital={hospital} />);
    expect(await screen.findByText("No other shift is open.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Record a pickup/ })).not.toBeInTheDocument();
  });

  it("corrects an overstated pickup with an amount and a reason", async () => {
    const fetcher = server({
      permissions: ["billing.account.read", "billing.shift.authorise"],
      open: [other],
      pickups: [
        pickup,
        {
          ...pickup,
          id: "0199e000-0000-7000-8000-0000000000e2",
          kind: "correction",
          amount_minor: 5000,
          counts: null,
          corrects_id: pickup.id,
          reason: "Miscounted",
        },
      ],
    });
    const { user } = renderWithProviders(<CashDesk hospital={hospital} />);
    expect(await screen.findByText(/Corrected by INR 50.00 \(Miscounted\)/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Correct the pickup of INR 1,000.00" }));
    await user.click(screen.getByRole("button", { name: "Correct" }));
    expect(await screen.findByText("Give a reason")).toBeInTheDocument();
    await user.clear(screen.getByLabelText("Overstated by (INR)"));
    await user.type(screen.getByLabelText("Overstated by (INR)"), "100");
    await user.type(screen.getByLabelText("Why"), "Counted a note twice");
    await user.click(screen.getByRole("button", { name: "Correct" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(
      new RegExp(`/cash-pickups/${pickup.id}/corrections/[0-9a-f-]{36}$`),
    );
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({
      amount_minor: 10000,
      reason: "Counted a note twice",
    });
  });

  it("shows a closed shift's variance waiting, and lets a supervisor authorise it", async () => {
    const fetcher = server({
      permissions: ["billing.account.read", "billing.shift.authorise"],
      shifts: [pending],
    });
    const { user } = renderWithProviders(<CashDesk hospital={hospital} />);
    await user.click(
      await screen.findByRole("button", { name: "Authorise the variance on IPD-1" }),
    );
    await user.click(screen.getByRole("button", { name: "Authorise" }));
    expect(await screen.findByText("Give a reason")).toBeInTheDocument();
    await user.type(screen.getByLabelText("Why the difference is accepted"), "One note short");
    await user.click(screen.getByRole("button", { name: "Authorise" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(`/cash-shifts/${pending.id}/authorisation`);
  });

  it("adds one counter after another, each under its own id", async () => {
    const fetcher = server({ permissions: ["billing.account.read", "billing.counter.manage"] });
    const { user } = renderWithProviders(<CashDesk hospital={hospital} />);
    for (const code of ["IPD-2", "IPD-3"]) {
      await user.type(await screen.findByLabelText("Code"), code);
      await user.type(screen.getByLabelText("Name"), `Desk ${code}`);
      await user.click(screen.getByRole("button", { name: "Add counter" }));
      await waitFor(() => expect(screen.getByLabelText("Code")).toHaveValue(""));
    }
    const ids = puts(fetcher).map(([url]) => url.split("/").pop());
    expect(ids).toHaveLength(2);
    expect(ids[0]).not.toEqual(ids[1]);
  });

  it("switches card, netbanking and wallet per counter", async () => {
    const fetcher = server({
      permissions: ["billing.account.read", "billing.counter.manage"],
      counters: [{ ...counter, modes: ["wallet"] }],
    });
    const { user } = renderWithProviders(<CashDesk hospital={hospital} />);
    expect(await screen.findByRole("button", { name: "Switch off wallet at IPD-1" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await user.click(screen.getByRole("button", { name: "Switch on card at IPD-1" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(
      new RegExp(`/billing/hospitals/${hospital.id}/counters/${counter.id}/modes/card$`),
    );
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({ accepted: true });
  });

  it("tells the cashier their short count waits for a supervisor", async () => {
    server({ permissions: cashier, mine: pending });
    renderWithProviders(<CashDesk hospital={hospital} />);
    expect(await screen.findByRole("status")).toHaveTextContent("waits for a supervisor");
  });
});

describe("CashPayments", () => {
  const account = (
    balance: number,
    over: { held?: number; applied?: number; status?: "open" | "closed" } = {},
  ): AdmissionAccount => ({
    admission_id: admission,
    hospital_id: hospital.id,
    patient_id: "p",
    status: over.status ?? "open",
    opened_at: null,
    closed_at: null,
    totals: [
      {
        currency: "INR",
        charged_minor: 500000,
        reversed_minor: 0,
        outstanding_minor: 500000,
        paid_minor: 500000 - balance - (over.applied ?? 0),
        balance_minor: balance,
        deposit_held_minor: over.held ?? 0,
        applied_minor: over.applied ?? 0,
        refunded_minor: 0,
        upi_waiting_minor: 0,
        discountable_minor: 0,
      },
    ],
    sources: [],
    bed_days: { configured: true, priced: 0, not_priced: 0 },
    entry_count: 1,
    discharge_ordered: false,
  });

  it("takes cash up to the balance due in the cashier's open shift", async () => {
    const fetcher = server({ permissions: cashier, mine: shift() });
    const { user } = renderWithProviders(
      <CashPayments hospital={hospital} admissionId={admission} account={account(150000)} />,
    );
    expect(screen.getByText(/balance due INR 1,500.00/)).toBeInTheDocument();
    await user.clear(await screen.findByLabelText("Cash received (INR)"));
    await user.type(screen.getByLabelText("Cash received (INR)"), "1600");
    await user.click(screen.getByRole("button", { name: "Take cash" }));
    expect(await screen.findByText(/take an advance as a deposit/)).toBeInTheDocument();
    expect(puts(fetcher)).toHaveLength(0);

    await user.clear(screen.getByLabelText("Cash received (INR)"));
    await user.type(screen.getByLabelText("Cash received (INR)"), "1500");
    await user.click(screen.getByRole("button", { name: "Take cash" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(
      new RegExp(`/admissions/${admission}/receipts/[0-9a-f-]{36}$`),
    );
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({
      amount_minor: 150000,
      currency: "INR",
    });
  });

  it("records a card payment with its slip's references, only by modes the counter takes", async () => {
    const fetcher = server({ permissions: cashier, mine: shift({ counter_modes: ["card", "wallet"] }) });
    const { user } = renderWithProviders(
      <CashPayments hospital={hospital} admissionId={admission} account={account(150000)} />,
    );
    const form = await screen.findByRole("form", {
      name: "Record a payment by card, netbanking or wallet",
    });
    const paidBy = within(form).getByLabelText("Paid by");
    await user.click(paidBy);
    expect((await screen.findAllByRole("option")).map((o) => o.textContent)).toEqual([
      "Card",
      "Wallet",
    ]);
    await user.keyboard("{Escape}");
    await user.clear(within(form).getByLabelText("Amount received (INR)"));
    await user.type(within(form).getByLabelText("Amount received (INR)"), "1500");
    await user.type(within(form).getByLabelText("Transaction reference (RRN)"), "412345678901");
    await user.click(within(form).getByRole("button", { name: "Record payment" }));
    expect(await within(form).findByText("The 6-character code on the slip")).toBeInTheDocument();
    expect(puts(fetcher)).toHaveLength(0);

    await user.type(within(form).getByLabelText("Approval code"), "a1b2c3");
    await user.click(within(form).getByRole("button", { name: "Record payment" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(new RegExp(`/admissions/${admission}/receipts/[0-9a-f-]{36}$`));
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({
      amount_minor: 150000,
      currency: "INR",
      mode: "card",
      reference: "412345678901",
      approval_code: "A1B2C3",
    });
    await waitFor(() =>
      expect(within(form).getByLabelText("Transaction reference (RRN)")).toHaveValue(""),
    );
  });

  it("records a wallet deposit with the wallet's name", async () => {
    const fetcher = server({ permissions: cashier, mine: shift({ counter_modes: ["wallet"] }) });
    const { user } = renderWithProviders(
      <CashPayments hospital={hospital} admissionId={admission} account={account(0)} />,
    );
    const form = await screen.findByRole("form", {
      name: "Record a deposit by card, netbanking or wallet",
    });
    expect(
      screen.queryByRole("form", { name: "Record a payment by card, netbanking or wallet" }),
    ).not.toBeInTheDocument();
    await user.clear(within(form).getByLabelText("Deposit received (INR)"));
    await user.type(within(form).getByLabelText("Deposit received (INR)"), "2000");
    await user.type(within(form).getByLabelText("Wallet"), "PhonePe");
    await user.type(within(form).getByLabelText("Wallet transaction ID"), "T2611021234");
    await user.click(within(form).getByRole("button", { name: "Record deposit" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(new RegExp(`/admissions/${admission}/deposits/[0-9a-f-]{36}$`));
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({
      amount_minor: 200000,
      currency: "INR",
      mode: "wallet",
      reference: "T2611021234",
      wallet: "phonepe",
    });
  });

  it("offers no card, netbanking or wallet at a counter that takes none", async () => {
    server({ permissions: cashier, mine: shift() });
    renderWithProviders(
      <CashPayments hospital={hospital} admissionId={admission} account={account(150000)} />,
    );
    expect(await screen.findByRole("button", { name: "Take cash" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Record payment" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Record deposit" })).not.toBeInTheDocument();
  });

  it("shows a card receipt's references and lets its cashier void it", async () => {
    server({
      permissions: cashier,
      mine: shift(),
      receipts: [
        {
          id: "0199e000-0000-7000-8000-0000000000e9",
          hospital_id: hospital.id,
          shift_id: shift().id,
          counter_id: counter.id,
          counter_code: "IPD-1",
          number: 3,
          cashier: "user-clerk",
          admission_id: admission,
          purpose: "payment",
          mode: "card",
          reference: "412345678901",
          approval_code: "A1B2C3",
          wallet: null,
          rrn: null,
          late: false,
          approval_id: null,
          amount_minor: 150000,
          currency: "INR",
          received_at: "2026-11-02T03:00:00Z",
          void_reason: null,
          voided_at: null,
        },
      ],
    });
    renderWithProviders(
      <CashPayments hospital={hospital} admissionId={admission} account={account(0)} />,
    );
    expect(await screen.findByText(/Card Receipt IPD-1-3/)).toHaveTextContent(
      "Ref 412345678901 · Approval A1B2C3",
    );
    expect(
      await screen.findByRole("button", { name: "Void Card Receipt IPD-1-3" }),
    ).toBeInTheDocument();
  });

  it("refreshes the billing desk's lists after taking cash", async () => {
    const fetcher = server({ permissions: cashier, mine: shift() });
    let deskFetches = 0;
    function DeskProbe() {
      useApiPagedQuery(["billing", "desk", hospital.id, "to_collect"], "/probe-desk");
      return null;
    }
    const counting = vi.fn(async (url: string, init: RequestInit = {}) => {
      if (String(url).includes("/probe-desk")) deskFetches++;
      return fetcher(url, init);
    });
    vi.stubGlobal("fetch", counting);
    const { user } = renderWithProviders(
      <>
        <DeskProbe />
        <CashPayments hospital={hospital} admissionId={admission} account={account(150000)} />
      </>,
    );
    await waitFor(() => expect(deskFetches).toBe(1));
    await user.clear(await screen.findByLabelText("Cash received (INR)"));
    await user.type(screen.getByLabelText("Cash received (INR)"), "1500");
    await user.click(screen.getByRole("button", { name: "Take cash" }));
    await waitFor(() => expect(deskFetches).toBe(2));
  });

  it("offers no payment without an open shift", async () => {
    server({ permissions: cashier });
    renderWithProviders(
      <CashPayments hospital={hospital} admissionId={admission} account={account(150000)} />,
    );
    expect(screen.getByText(/balance due/)).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByLabelText("Cash received (INR)")).not.toBeInTheDocument();
  });

  it("shows credit as credit", async () => {
    server({ permissions: cashier });
    renderWithProviders(
      <CashPayments hospital={hospital} admissionId={admission} account={account(-20000)} />,
    );
    expect(screen.getByText(/in credit INR 200.00/)).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByLabelText("Cash received (INR)")).not.toBeInTheDocument();
  });

  it("takes a deposit beyond the balance due, while the account is open", async () => {
    const fetcher = server({ permissions: cashier, mine: shift() });
    const { user } = renderWithProviders(
      <CashPayments hospital={hospital} admissionId={admission} account={account(0)} />,
    );
    expect(screen.queryByLabelText("Cash received (INR)")).not.toBeInTheDocument();
    await user.clear(await screen.findByLabelText("Deposit received (INR)"));
    await user.type(screen.getByLabelText("Deposit received (INR)"), "20000");
    await user.click(screen.getByRole("button", { name: "Take deposit" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(
      new RegExp(`/admissions/${admission}/deposits/[0-9a-f-]{36}$`),
    );
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({
      amount_minor: 2000000,
      currency: "INR",
    });
  });

  it("takes no deposit once the patient has left", async () => {
    server({ permissions: cashier, mine: shift() });
    renderWithProviders(
      <CashPayments
        hospital={hospital}
        admissionId={admission}
        account={account(1000, { status: "closed" })}
      />,
    );
    expect(await screen.findByLabelText("Cash received (INR)")).toBeInTheDocument();
    expect(screen.queryByLabelText("Deposit received (INR)")).not.toBeInTheDocument();
  });

  it("applies held deposits once a final bill exists, for the billing desk", async () => {
    const fetcher = server({ permissions: [...cashier, "billing.bill.issue"] });
    const held = account(150000, { held: 50000 });
    function Desk() {
      const [final, setFinal] = useState(false);
      return (
        <>
          <button type="button" onClick={() => setFinal(true)}>
            final bill issued
          </button>
          <CashPayments
            hospital={hospital}
            admissionId={admission}
            account={held}
            hasFinalBill={final}
          />
        </>
      );
    }
    const { user } = renderWithProviders(<Desk />);
    expect(screen.getByText(/deposits held INR 500.00/)).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 50));
    expect(
      screen.queryByRole("button", { name: "Apply deposits to the final bill" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "final bill issued" }));
    await user.click(
      await screen.findByRole("button", { name: "Apply deposits to the final bill" }),
    );
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(
      new RegExp(`/admissions/${admission}/deposit-applications/[0-9a-f-]{36}$`),
    );
  });
});

describe("CashPayments refunds", () => {
  it("offers to refund only the surplus over the bill", async () => {
    server({ permissions: ["billing.account.read", "billing.cash.collect"], mine: shift() });
    const account = {
      admission_id: admission,
      hospital_id: hospital.id,
      patient_id: "p",
      status: "open" as const,
      opened_at: null,
      closed_at: null,
      totals: [
        {
          currency: "INR",
          charged_minor: 300000,
          reversed_minor: 0,
          outstanding_minor: 300000,
          paid_minor: 0,
          balance_minor: 300000,
          deposit_held_minor: 300000,
          applied_minor: 0,
          refunded_minor: 0,
          upi_waiting_minor: 0,
          discountable_minor: 0,
        },
      ],
      sources: [],
      bed_days: { configured: true, priced: 0, not_priced: 0 },
      entry_count: 1,
      discharge_ordered: true,
    };
    renderWithProviders(
      <CashPayments hospital={hospital} admissionId={admission} account={account} hasFinalBill />,
    );
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByLabelText("Refund in cash (INR)")).not.toBeInTheDocument();
  });

  it("hands the drawer to a named cashier with a blind count", async () => {
    const fetcher = server({ permissions: cashier, mine: shift() });
    const { user } = renderWithProviders(<CashDesk hospital={hospital} />);
    await user.click(await screen.findByRole("button", { name: "Hand over" }));
    const form = await screen.findByRole("form", { name: "Hand over the drawer" });
    await user.type(within(form).getByLabelText("INR 500.00 notes"), "2");
    await user.click(within(form).getByRole("button", { name: "Offer handover" }));
    expect(await screen.findByText("Enter the incoming cashier's user ID")).toBeInTheDocument();
    expect(puts(fetcher)).toHaveLength(0);
    await user.type(screen.getByLabelText("Incoming cashier's user ID"), " user-clerk2 ");
    await user.click(within(form).getByRole("button", { name: "Offer handover" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(
      new RegExp(`/cash-shifts/${shift().id}/handovers/[0-9a-f-]{36}$`),
    );
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({
      to: "user-clerk2",
      counts: { "note:50000": 2 },
    });
  });

  it("shows a drawer being handed over and withdraws the offer", async () => {
    const id = handover().id;
    const fetcher = server({
      permissions: cashier,
      mine: shift({
        status: "handing_over",
        live_handover_id: id,
        live_handover_to: "user-clerk2",
        live_handover_status: "offered",
      }),
    });
    const { user } = renderWithProviders(<CashDesk hospital={hospital} />);
    expect(await screen.findByText(/waiting for their count/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Close shift" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Withdraw handover" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(`/cash-handovers/${id}/withdrawal`);
  });

  it("does not withdraw a disputed handover", async () => {
    server({
      permissions: cashier,
      mine: shift({
        status: "handing_over",
        live_handover_id: handover().id,
        live_handover_to: "user-clerk2",
        live_handover_status: "disputed",
      }),
    });
    renderWithProviders(<CashDesk hospital={hospital} />);
    expect(await screen.findByText(/a supervisor will count the drawer/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Withdraw handover" })).not.toBeInTheDocument();
  });

  it("counts a drawer handed to the caller blind, once", async () => {
    const fetcher = server({
      permissions: cashier,
      incoming: [handover()],
      put: handover({ status: "disputed" }),
    });
    const { user } = renderWithProviders(<CashDesk hospital={hospital} />);
    expect(await screen.findByText(/IPD-1 from user-clerk2/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Count the drawer of IPD-1" }));
    const form = await screen.findByRole("form", { name: "Count the drawer of IPD-1" });
    await user.type(within(form).getByLabelText("INR 100.00 notes"), "7");
    await user.click(within(form).getByRole("button", { name: "Record count" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(`/cash-handovers/${handover().id}/acceptance`);
    const body = JSON.parse(String(puts(fetcher)[0][1].body));
    expect(body.counts).toEqual({ "note:10000": 7 });
    expect(body.shift).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-/);
    expect(body.shift).not.toBe(handover().id);
  });

  it("lets a supervisor settle a disputed handover with a count and a reason", async () => {
    const fetcher = server({
      permissions: ["billing.account.read", "billing.shift.authorise"],
      disputed: [handover({ status: "disputed" })],
    });
    const { user } = renderWithProviders(<CashDesk hospital={hospital} />);
    await user.click(await screen.findByRole("button", { name: "Settle the handover on IPD-1" }));
    const form = await screen.findByRole("form", { name: "Settle the handover on IPD-1" });
    await user.type(within(form).getByLabelText("INR 500.00 notes"), "1");
    await user.click(within(form).getByRole("button", { name: "Settle handover" }));
    expect(await screen.findByText("Give a reason")).toBeInTheDocument();
    expect(puts(fetcher)).toHaveLength(0);
    await user.type(screen.getByLabelText("Why"), "Recounted with both cashiers");
    await user.click(within(form).getByRole("button", { name: "Settle handover" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(`/cash-handovers/${handover().id}/settlement`);
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({
      counts: { "note:50000": 1 },
      reason: "Recounted with both cashiers",
    });
  });

  it("keeps telling the incoming cashier a disputed drawer waits, and opens no other shift", async () => {
    server({ permissions: cashier, incoming: [handover({ status: "disputed" })] });
    renderWithProviders(<CashDesk hospital={hospital} />);
    expect(await screen.findByText(/differs from user-clerk2's/)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Count the drawer of IPD-1" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("form", { name: "Open a shift" })).not.toBeInTheDocument();
  });
});
