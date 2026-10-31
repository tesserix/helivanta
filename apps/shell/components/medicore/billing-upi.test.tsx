import { useState } from "react";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type { AdmissionAccount, BillingUpiRequest } from "@helivanta/api";
import { CashPayments } from "./cash-payments";

const hospital = {
  id: "0199e000-0000-7000-8000-00000000a001",
  name: "TEST General",
  access_level: "manager" as const,
};
const admission = "0199e000-0000-7000-8000-0000000000a9";
const shiftId = "0199e000-0000-7000-8000-0000000000d1";
const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const page = (data: unknown[]) => json(200, { data, page: { next_cursor: null, has_more: false } });
const shift = {
  id: shiftId,
  hospital_id: hospital.id,
  counter_id: "c1",
  counter_code: "IPD-1",
  cashier: "user-clerk",
  currency: "INR",
  opening_float_minor: 0,
  opened_at: "2026-11-01T03:00:00Z",
  status: "open",
  expected_minor: null,
  declared_minor: null,
  variance_minor: null,
  closed_at: null,
  authorised_by: null,
  authorisation_reason: null,
  authorised_at: null,
};

function request(over: Partial<BillingUpiRequest>): BillingUpiRequest {
  return {
    id: "",
    hospital_id: hospital.id,
    shift_id: shiftId,
    counter_id: "c1",
    counter_code: "IPD-1",
    cashier: "user-clerk",
    admission_id: admission,
    purpose: "payment",
    amount_minor: 100000,
    currency: "INR",
    status: "awaiting_payment",
    qr_image_url: "https://rzp.io/i/qr1",
    expires_at: new Date(Date.now() + 300_000).toISOString(),
    failure_reason: null,
    provider_payment_id: null,
    rrn: null,
    paid_at: null,
    late: false,
    created_at: new Date().toISOString(),
    receipt_number: null,
    ...over,
  };
}

const account = (balance: number, waiting = 0): AdmissionAccount => ({
  admission_id: admission,
  hospital_id: hospital.id,
  patient_id: "p",
  status: "open",
  opened_at: null,
  closed_at: null,
  totals: [
    {
      currency: "INR",
      charged_minor: 500000,
      reversed_minor: 0,
      outstanding_minor: 500000,
      paid_minor: 500000 - balance,
      balance_minor: balance,
      deposit_held_minor: 0,
      applied_minor: 0,
      refunded_minor: 0,
      upi_waiting_minor: waiting,
    },
  ],
  sources: [],
  bed_days: { configured: true, priced: 0, not_priced: 0 },
  entry_count: 1,
  discharge_ordered: false,
});

afterEach(() => vi.unstubAllGlobals());

/** The request answers `state()` to every read; `listed` is the admission's list. */
function server(state: () => Partial<BillingUpiRequest>, listed: BillingUpiRequest[] = []) {
  // A cancel holds: later reads find the request cancelling, as the server does.
  let cancelled = false;
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (url.includes("/iam/me/permissions"))
      return json(200, {
        subject: "user-clerk",
        data: ["billing.account.read", "billing.cash.collect"],
      });
    if (url.endsWith(`/billing/hospitals/${hospital.id}/access`))
      return json(200, { granted: true });
    if (url.endsWith("/cash-shifts/mine")) return json(200, shift);
    if (url.endsWith("/upi-requests")) return page(listed);
    const id = /upi-requests\/([0-9a-f-]{36})/.exec(url)?.[1];
    if (id && url.endsWith("/cancel")) cancelled = true;
    if (id && cancelled) return json(200, request({ ...state(), id, status: "cancelling" }));
    if (id) return json(init.method === "PUT" ? 201 : 200, request({ ...state(), id }));
    return page([]);
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
const calls = (f: ReturnType<typeof server>, re: RegExp, method = "GET") =>
  f.mock.calls.filter(([url, init]) => re.test(String(url)) && (init?.method ?? "GET") === method);

describe("BillingUpi", () => {
  it("shows the QR, and the receipt only once Razorpay confirms the payment", async () => {
    let now: Partial<BillingUpiRequest> = { status: "requested", qr_image_url: null };
    const fetcher = server(() => now);
    const { user } = renderWithProviders(
      <CashPayments hospital={hospital} admissionId={admission} account={account(150000)} />,
    );
    const form = await screen.findByRole("form", { name: "Take a UPI payment" });
    await user.clear(within(form).getByLabelText("UPI amount (INR)"));
    await user.type(within(form).getByLabelText("UPI amount (INR)"), "1000");
    await user.click(within(form).getByRole("button", { name: "Show UPI QR" }));
    await waitFor(() =>
      expect(calls(fetcher, /upi-requests\/[0-9a-f-]{36}$/, "PUT")).toHaveLength(1),
    );
    const [url, init] = calls(fetcher, /upi-requests\/[0-9a-f-]{36}$/, "PUT")[0]!;
    expect(String(url)).toMatch(new RegExp(`/admissions/${admission}/upi-requests/`));
    expect(JSON.parse(String(init!.body))).toEqual({ purpose: "payment", amount_minor: 100000 });
    expect(await screen.findByText("Starting UPI…")).toBeInTheDocument();

    now = { status: "awaiting_payment" };
    expect(
      await screen.findByRole("img", { name: "UPI QR for INR 1,000.00" }, { timeout: 5000 }),
    ).toBeInTheDocument();
    expect(screen.getByText(/The receipt is recorded once Razorpay confirms/)).toBeInTheDocument();
    expect(screen.queryByText(/UPI payment received/)).not.toBeInTheDocument();

    const receiptsBefore = calls(fetcher, /\/receipts$/).length;
    now = { status: "paid", rrn: "RRN77", provider_payment_id: "pay_1", receipt_number: 2 };
    expect(
      await screen.findByText(
        "UPI payment received: INR 1,000.00 · receipt IPD-1-2 · RRN RRN77",
        {},
        { timeout: 5000 },
      ),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(calls(fetcher, /\/receipts$/).length).toBeGreaterThan(receiptsBefore),
    );
    // Two poll intervals: waiting, then paid.
  }, 15_000);

  it("holds the payment to the balance less UPI already waiting", async () => {
    const fetcher = server(() => ({}));
    const { user } = renderWithProviders(
      <CashPayments
        hospital={hospital}
        admissionId={admission}
        account={account(150000, 100000)}
      />,
    );
    expect(screen.getByText(/UPI waiting INR 1,000.00/)).toBeInTheDocument();
    const form = await screen.findByRole("form", { name: "Take a UPI payment" });
    await user.clear(within(form).getByLabelText("UPI amount (INR)"));
    await user.type(within(form).getByLabelText("UPI amount (INR)"), "501");
    await user.click(within(form).getByRole("button", { name: "Show UPI QR" }));
    expect(await screen.findByText(/At most INR 500.00/)).toBeInTheDocument();
    expect(calls(fetcher, /upi-requests\//, "PUT")).toHaveLength(0);
    const cash = screen.getByRole("form", { name: "Take cash" });
    await user.clear(within(cash).getByLabelText("Cash received (INR)"));
    await user.type(within(cash).getByLabelText("Cash received (INR)"), "501");
    await user.click(within(cash).getByRole("button", { name: "Take cash" }));
    expect(await screen.findByText(/At most the balance due, INR 500.00/)).toBeInTheDocument();
  });

  it("picks up a QR this shift is still waiting on, and cancels it", async () => {
    const waitingId = "0199e000-0000-7000-8000-0000000000f1";
    const fetcher = server(() => ({}), [request({ id: waitingId })]);
    const { user } = renderWithProviders(
      <CashPayments
        hospital={hospital}
        admissionId={admission}
        account={account(150000, 100000)}
      />,
    );
    expect(await screen.findByRole("img", { name: "UPI QR for INR 1,000.00" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Cancel UPI" }));
    await waitFor(() =>
      expect(calls(fetcher, new RegExp(`upi-requests/${waitingId}/cancel$`), "POST")).toHaveLength(
        1,
      ),
    );
    expect(await screen.findByText(/Cancelling/)).toBeInTheDocument();
  });

  it("shows a new form after a closed QR, and never resumes a request long unanswered", async () => {
    const stale = "0199e000-0000-7000-8000-0000000000f2";
    const closed = "0199e000-0000-7000-8000-0000000000f3";
    const minutesAgo = (m: number) => new Date(Date.now() - m * 60_000).toISOString();
    // The list still says waiting; the request itself has since expired.
    const fetcher = server(
      () => ({ status: "expired" }),
      [
        request({ id: closed, status: "awaiting_payment", created_at: minutesAgo(6) }),
        request({ id: stale, status: "requested", qr_image_url: null, created_at: minutesAgo(20) }),
      ],
    );
    const { user } = renderWithProviders(
      <CashPayments hospital={hospital} admissionId={admission} account={account(150000)} />,
    );
    await user.click(await screen.findByRole("button", { name: "Show a new QR" }));
    expect(await screen.findByRole("form", { name: "Take a UPI payment" })).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.getByRole("form", { name: "Take a UPI payment" })).toBeInTheDocument();
    expect(calls(fetcher, new RegExp(`upi-requests/${stale}$`))).toHaveLength(0);
  });

  it("keeps the confirmation once the payment clears the balance", async () => {
    const waitingId = "0199e000-0000-7000-8000-0000000000f4";
    server(
      () => ({ status: "paid", rrn: "RRN9", provider_payment_id: "pay_9", receipt_number: 3 }),
      [request({ id: waitingId })],
    );
    function Desk() {
      const [settledAccount, setSettled] = useState(false);
      return (
        <>
          <button type="button" onClick={() => setSettled(true)}>
            Settle
          </button>
          <CashPayments
            hospital={hospital}
            admissionId={admission}
            account={settledAccount ? account(0) : account(150000, 100000)}
          />
        </>
      );
    }
    const { user } = renderWithProviders(<Desk />);
    expect(await screen.findByText(/UPI payment received: INR 1,000.00/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Settle" }));
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.getByText(/UPI payment received: INR 1,000.00/)).toBeInTheDocument();
    expect(screen.queryByRole("form", { name: "Take a UPI payment" })).not.toBeInTheDocument();
  });

  it("offers no UPI payment with nothing due, but still a UPI deposit", async () => {
    server(() => ({}));
    renderWithProviders(
      <CashPayments hospital={hospital} admissionId={admission} account={account(0)} />,
    );
    expect(await screen.findByRole("form", { name: "Take a UPI deposit" })).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByRole("form", { name: "Take a UPI payment" })).not.toBeInTheDocument();
  });

  it("never offers to void a UPI receipt, and shows its reference", async () => {
    const fetcher = server(() => ({}));
    fetcher.mockImplementation(async (url: string, init: RequestInit = {}) => {
      if (String(url).endsWith("/receipts"))
        return page([
          {
            id: "0199e000-0000-7000-8000-0000000000e1",
            hospital_id: hospital.id,
            shift_id: shiftId,
            counter_id: "c1",
            counter_code: "IPD-1",
            number: 2,
            cashier: "user-clerk",
            admission_id: admission,
            purpose: "payment",
            mode: "upi",
            rrn: "RRN77",
            late: true,
            approval_id: null,
            amount_minor: 100000,
            currency: "INR",
            received_at: "2026-11-01T04:00:00Z",
            void_reason: null,
            voided_at: null,
          },
        ]);
      if (String(url).endsWith("/cash-shifts/mine")) return json(200, shift);
      if (String(url).includes("/iam/me/permissions"))
        return json(200, {
          subject: "user-clerk",
          data: ["billing.account.read", "billing.cash.collect"],
        });
      if (String(url).endsWith(`/billing/hospitals/${hospital.id}/access`))
        return json(200, { granted: true });
      void init;
      return page([]);
    });
    renderWithProviders(
      <CashPayments hospital={hospital} admissionId={admission} account={account(0)} />,
    );
    expect(
      await screen.findByText(/UPI Receipt IPD-1-2 .* · RRN RRN77 · arrived after the QR closed/),
    ).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByRole("button", { name: /Void/ })).not.toBeInTheDocument();
  });
});
