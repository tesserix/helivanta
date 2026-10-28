import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type { PharmacyCounterPayment } from "@helivanta/api";
import { VerifiedUpi } from "./verified-upi";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const page = (data: unknown[]) => ({
  data,
  page: { has_more: false, next_cursor: null },
});

function payment(over: Partial<PharmacyCounterPayment>): PharmacyCounterPayment {
  return {
    id: "",
    purpose: "counter_sale",
    amount_minor: 68000,
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
    ...over,
  };
}

afterEach(() => vi.unstubAllGlobals());

/** The counter payment answers `state` to every read; `unused` lists paid money. */
function stub(state: Partial<PharmacyCounterPayment>, unused: PharmacyCounterPayment[] = []) {
  // Read on every request, so a test can move the payment on.
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (url.endsWith("/counter-payments")) return new Response(JSON.stringify(page(unused)));
    const id = /counter-payments\/([0-9a-f-]{36})/.exec(url)?.[1] ?? "";
    if (url.endsWith("/cancel"))
      return new Response(JSON.stringify(payment({ ...state, id, status: "cancelling" })));
    return new Response(JSON.stringify(payment({ ...state, id })), {
      status: init.method === "PUT" ? 201 : 200,
    });
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

function render(onPaid = vi.fn()) {
  const view = renderWithProviders(
    <VerifiedUpi scope={scope} amountMinor={68000} currency="INR" onPaid={onPaid} />,
  );
  return { ...view, onPaid };
}

describe("VerifiedUpi", () => {
  it("shows the QR and never reports paid while Razorpay has not confirmed", async () => {
    const fetcher = stub({ status: "awaiting_payment" });
    const { user, onPaid } = render();
    await user.click(await screen.findByRole("button", { name: /Show UPI QR for/ }));
    expect(await screen.findByRole("img", { name: /UPI QR for/ })).toHaveAttribute(
      "src",
      "https://rzp.io/i/qr1",
    );
    expect(screen.getByText(/Waiting for payment of/)).toBeVisible();
    expect(screen.getByText(/A screenshot is not a payment/)).toBeVisible();
    expect(onPaid).not.toHaveBeenCalledWith(expect.any(String));
    expect(fetcher).toHaveBeenCalledWith(
      expect.stringMatching(/\/counter-payments\/[0-9a-f-]{36}$/),
      expect.objectContaining({ method: "PUT", body: JSON.stringify({ amount_minor: 68000 }) }),
    );
    await user.click(screen.getByRole("button", { name: "Cancel UPI" }));
    await waitFor(() =>
      expect(fetcher).toHaveBeenCalledWith(
        expect.stringMatching(/\/cancel$/),
        expect.objectContaining({ method: "POST" }),
      ),
    );
  });

  it("hands over the payment id once it is paid", async () => {
    stub({
      status: "paid",
      rrn: "123456",
      provider_payment_id: "pay_1",
      paid_at: new Date().toISOString(),
    });
    const { user, onPaid } = render();
    await user.click(await screen.findByRole("button", { name: /Show UPI QR for/ }));
    expect(await screen.findByText(/Paid .*680.* · RRN 123456/)).toBeVisible();
    await waitFor(() =>
      expect(onPaid).toHaveBeenLastCalledWith(expect.stringMatching(/^[0-9a-f-]{36}$/)),
    );
  });

  it("sends the cashier to another tender when UPI cannot be verified", async () => {
    stub({ status: "failed", qr_image_url: null, failure_reason: "provider_not_configured" });
    const { user, onPaid } = render();
    await user.click(await screen.findByRole("button", { name: /Show UPI QR for/ }));
    expect(await screen.findByText(/No Razorpay account is set up/)).toBeVisible();
    expect(screen.queryByRole("button", { name: "Show a new QR" })).toBeNull();
    expect(onPaid).not.toHaveBeenCalledWith(expect.any(String));
  });

  it("offers a new QR when one expires", async () => {
    stub({ status: "expired" });
    const { user } = render();
    await user.click(await screen.findByRole("button", { name: /Show UPI QR for/ }));
    expect(await screen.findByText(/Expired\. Take another tender/)).toBeVisible();
    expect(screen.getByRole("button", { name: "Show a new QR" })).toBeVisible();
  });

  it("offers paid money that is not on a sale yet, for this amount only", async () => {
    const earlier = payment({
      id: "0199e000-0000-7000-8000-0000000000a1",
      status: "paid",
      rrn: "999",
      paid_at: new Date().toISOString(),
    });
    const other = payment({
      id: "0199e000-0000-7000-8000-0000000000a2",
      status: "paid",
      amount_minor: 100,
    });
    stub({ status: "awaiting_payment" }, [earlier, other]);
    const { user, onPaid } = render();
    const use = await screen.findByRole("button", { name: /Use the payment paid .* · RRN 999/ });
    expect(screen.getAllByRole("button", { name: /Use the payment/ })).toHaveLength(1);
    await user.click(use);
    await waitFor(() => expect(onPaid).toHaveBeenLastCalledWith(earlier.id));
  });

  it("closes an open QR when the counter moves on, so the payer cannot pay a bill it let go", async () => {
    const fetcher = stub({ status: "awaiting_payment" });
    const { user, unmount } = render();
    await user.click(await screen.findByRole("button", { name: /Show UPI QR for/ }));
    await screen.findByRole("img", { name: /UPI QR for/ });
    unmount();
    await waitFor(() =>
      expect(fetcher).toHaveBeenCalledWith(
        expect.stringMatching(/\/counter-payments\/[0-9a-f-]{36}\/cancel$/),
        expect.objectContaining({ method: "POST" }),
      ),
    );
  });

  it("keeps watching a cancelled QR and shows a late payment as paid", async () => {
    const state: Partial<PharmacyCounterPayment> = { status: "cancelled" };
    stub(state);
    const { user, onPaid } = render();
    await user.click(await screen.findByRole("button", { name: /Show UPI QR for/ }));
    expect(await screen.findByText("UPI cancelled.")).toBeVisible();
    expect(screen.getByText(/If the payer still paid, it shows here as paid/)).toBeVisible();
    Object.assign(state, { status: "paid", late: true, rrn: "77" });
    expect(
      await screen.findByText(/arrived after the QR closed/, {}, { timeout: 6000 }),
    ).toBeVisible();
    await waitFor(() =>
      expect(onPaid).toHaveBeenLastCalledWith(expect.stringMatching(/^[0-9a-f-]{36}$/)),
    );
  });

  it("does not offer earlier money while its own QR is open", async () => {
    const earlier = payment({
      id: "0199e000-0000-7000-8000-0000000000a1",
      status: "paid",
      paid_at: new Date().toISOString(),
    });
    stub({ status: "awaiting_payment" }, [earlier]);
    const { user } = render();
    await screen.findByRole("button", { name: /Use the payment paid/ });
    await user.click(screen.getByRole("button", { name: /Show UPI QR for/ }));
    await screen.findByRole("img", { name: /UPI QR for/ });
    expect(screen.queryByRole("button", { name: /Use the payment paid/ })).toBeNull();
  });
});
