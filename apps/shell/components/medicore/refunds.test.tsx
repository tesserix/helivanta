import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { DepositRefunds, RefundPolicySetup } from "./refunds";

const hospital = {
  id: "0199e000-0000-7000-8000-00000000a001",
  name: "TEST General",
  access_level: "manager" as const,
};
const admission = "0199e000-0000-7000-8000-0000000000a9";
const approval = {
  id: "0199e000-0000-7000-8000-0000000000e1",
  admission_id: admission,
  amount_minor: 2500000,
  currency: "INR",
  reason: "Discharged early",
  approved_by: "user-super",
  approved_at: "2026-10-30T08:00:00Z",
  used_by: null,
};
const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const page = (data: unknown[]) => json(200, { data, page: { next_cursor: null, has_more: false } });

afterEach(() => vi.unstubAllGlobals());

function server(permissions: string[], approvals: unknown[] = []) {
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (url.includes("/iam/me/permissions"))
      return json(200, { subject: "user-clerk", data: permissions });
    if (init.method === "PUT") return json(201, {});
    if (url.endsWith("/refund-policy"))
      return json(200, {
        hospital_id: hospital.id,
        threshold_minor: 1000000,
        currency: "INR",
        created_by: null,
        created_at: null,
      });
    if (url.includes("/refund-approvals")) return page(approvals);
    return page([]);
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
const puts = (f: ReturnType<typeof server>) =>
  f.mock.calls.filter(([, init]) => init?.method === "PUT") as [string, RequestInit][];
const props = {
  hospital,
  admissionId: admission,
  held: 4000000,
  shiftOpen: true,
  receiptsKey: ["r"],
};

describe("DepositRefunds", () => {
  it("pays a refund within the threshold without an approval", async () => {
    const fetcher = server(["billing.cash.collect"]);
    const { user } = renderWithProviders(<DepositRefunds {...props} />);
    await user.clear(await screen.findByLabelText("Refund in cash (INR)"));
    await user.type(screen.getByLabelText("Refund in cash (INR)"), "10000");
    await user.click(screen.getByRole("button", { name: "Pay refund" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(
      new RegExp(`/admissions/${admission}/refunds/[0-9a-f-]{36}$`),
    );
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({
      amount_minor: 1000000,
      currency: "INR",
    });
  });

  it("asks for an approval above the threshold, and pays against it for its amount", async () => {
    const fetcher = server(["billing.cash.collect"], [approval]);
    const { user } = renderWithProviders(<DepositRefunds {...props} />);
    await user.clear(await screen.findByLabelText("Refund in cash (INR)"));
    await user.type(screen.getByLabelText("Refund in cash (INR)"), "20000");
    await user.click(screen.getByRole("button", { name: "Pay refund" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "a supervisor approves this amount first",
    );
    expect(puts(fetcher)).toHaveLength(0);
    await user.clear(screen.getByLabelText("Refund in cash (INR)"));
    await user.type(screen.getByLabelText("Refund in cash (INR)"), "25000");
    await user.click(screen.getByRole("button", { name: "Pay refund" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({
      amount_minor: 2500000,
      approval_id: approval.id,
      currency: "INR",
    });
  });

  it("offers no payout without an open shift", async () => {
    server(["billing.cash.collect"]);
    renderWithProviders(<DepositRefunds {...props} shiftOpen={false} />);
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByLabelText("Refund in cash (INR)")).not.toBeInTheDocument();
  });

  it("offers no approval to a cashier", async () => {
    server(["billing.cash.collect"]);
    renderWithProviders(<DepositRefunds {...props} />);
    expect(await screen.findByLabelText("Refund in cash (INR)")).toBeInTheDocument();
    expect(screen.queryByLabelText("Approve a refund of (INR)")).not.toBeInTheDocument();
  });

  it("never refunds more than is held", async () => {
    const fetcher = server(["billing.cash.collect"]);
    const { user } = renderWithProviders(<DepositRefunds {...props} held={500000} />);
    await user.clear(await screen.findByLabelText("Refund in cash (INR)"));
    await user.type(screen.getByLabelText("Refund in cash (INR)"), "5001");
    await user.click(screen.getByRole("button", { name: "Pay refund" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "At most what is held beyond the bill",
    );
    expect(puts(fetcher)).toHaveLength(0);
  });

  it("lets a supervisor approve an amount, with a reason", async () => {
    const fetcher = server(["billing.shift.authorise"]);
    const { user } = renderWithProviders(<DepositRefunds {...props} shiftOpen={false} />);
    expect(screen.queryByLabelText("Refund in cash (INR)")).not.toBeInTheDocument();
    await user.clear(await screen.findByLabelText("Approve a refund of (INR)"));
    await user.type(screen.getByLabelText("Approve a refund of (INR)"), "25000");
    await user.type(screen.getByLabelText("Reason"), "Discharged early");
    await user.click(screen.getByRole("button", { name: "Approve refund" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(`/admissions/${admission}/refund-approvals/`);
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({
      amount_minor: 2500000,
      reason: "Discharged early",
    });
  });
});

describe("RefundPolicySetup", () => {
  it("shows the default and sets a new threshold", async () => {
    const fetcher = server(["billing.counter.manage"]);
    const { user } = renderWithProviders(<RefundPolicySetup hospital={hospital} />);
    expect(
      await screen.findByText(/above INR 10,000.00 need a supervisor's approval \(the default\)/),
    ).toBeInTheDocument();
    await user.clear(screen.getByLabelText("Approval needed above (INR)"));
    await user.type(screen.getByLabelText("Approval needed above (INR)"), "20000");
    await user.click(screen.getByRole("button", { name: "Set threshold" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({ threshold_minor: 2000000 });
  });
});
