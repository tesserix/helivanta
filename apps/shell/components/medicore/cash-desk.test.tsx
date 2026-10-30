import { useState } from "react";
import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
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
const counter = { id: "0199e000-0000-7000-8000-0000000000c1", code: "IPD-1", name: "IPD desk" };
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

function server(opts: { permissions: string[]; mine?: unknown; shifts?: unknown[] }) {
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (url.includes("/iam/me/permissions"))
      return json(200, { subject: "user-clerk", data: opts.permissions });
    if (url.endsWith(`/billing/hospitals/${hospital.id}/access`))
      return json(200, { granted: true });
    if (init.method === "PUT") return json(200, {});
    if (url.endsWith("/cash-shifts/mine"))
      return opts.mine
        ? json(200, opts.mine)
        : json(404, { error: "no_open_shift", message: "none" });
    if (url.includes("/cash-shifts?status=pending_authorisation")) return page(opts.shifts ?? []);
    if (url.includes("/cash-shifts")) return page([]);
    if (url.includes("/counters")) return page([counter]);
    if (url.includes("/receipts")) return page([]);
    return page([]);
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
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

  it("closes the shift blind: the count is asked, nothing expected is shown", async () => {
    const fetcher = server({ permissions: cashier, mine: shift() });
    const { user } = renderWithProviders(<CashDesk hospital={hospital} />);
    expect(await screen.findByText(/Shift open/)).toBeInTheDocument();
    expect(screen.queryByText(/expected/i)).not.toBeInTheDocument();
    await user.clear(screen.getByLabelText("Cash counted in the drawer (INR)"));
    await user.type(screen.getByLabelText("Cash counted in the drawer (INR)"), "4900");
    await user.click(screen.getByRole("button", { name: "Close shift" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(`/cash-shifts/${shift().id}/close`);
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({ declared_minor: 490000 });
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
