import { afterEach, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { renderWithProviders as render } from "@helivanta/api/testing";
import { PayableHistory } from "./payable-forms";
const id = (n: number) => `0199e000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const scope = { tenantId: id(1), pharmacyId: id(2), outletId: id(3) };
const bill = id(5),
  payment = id(8),
  receipt = id(10);
const empty = { data: [], page: { has_more: false, next_cursor: null } };
afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
  localStorage.clear();
});
it("integrates managed credit history into a readonly bill and uses payment references instead of record UUIDs", async () => {
  const doc = {
    id: bill,
    outlet_id: scope.outletId,
    payee_reference: "TEST-SUPPLIER",
    payee_name: "TEST Supplier",
    document_reference: "TEST-BILL",
    issued_on: "2026-11-18",
    due_on: "2026-11-30",
    currency: "INR",
    total_minor: 1180,
    paid_minor: 0,
    credit_minor: 500,
    outstanding_minor: 680,
    actor_subject: "TEST-finance",
    created_at: "2026-11-21T00:00:00Z",
    line_count: 1,
    lines: [
      {
        category: "inventory",
        receipt_id: receipt,
        description: "TEST received cartons",
        net_minor: 1000,
        tax_minor: 180,
        recoverable_tax_minor: 180,
        landed_minor: 0,
      },
    ],
  };
  const entry = {
    id: payment,
    kind: "payment",
    amount_minor: 1000,
    currency: "INR",
    paid_by: "TEST Finance operator",
    method: "bank",
    source_reference: "TEST-PAYMENT-REF",
    occurred_on: "2026-11-19",
    reason: "TEST payment",
    reversal_of: null,
    actor_subject: "TEST-finance",
    created_at: "2026-11-21T00:00:00Z",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (url: string) =>
        new Response(
          JSON.stringify(
            url.endsWith("/permissions")
              ? {
                  tenant_id: scope.tenantId,
                  subject: "TEST-finance",
                  data: ["pharmacy.accounts.read"],
                }
              : url.endsWith(`/payables/${bill}`)
                ? doc
                : url.includes("/settlements")
                  ? {
                      ...empty,
                      data: [
                        entry,
                        {
                          ...entry,
                          id: id(9),
                          kind: "payment_reversal",
                          source_reference: "TEST-REVERSAL-REF",
                          reversal_of: payment,
                        },
                      ],
                    }
                  : url.includes("supplier-credit-allocations")
                    ? {
                        ...empty,
                        data: [
                          {
                            id: id(6),
                            credit_id: id(4),
                            outlet_id: scope.outletId,
                            payable_id: bill,
                            expected_version: 1,
                            expected_available_minor: 750,
                            expected_payable_version: 1,
                            expected_outstanding_minor: 1180,
                            amount_minor: 500,
                            occurred_on: "2026-11-18",
                            reason: "TEST managed application",
                            version: 2,
                            payable_version: 2,
                            available_minor: 250,
                            outstanding_minor: 680,
                            payable_reference: "TEST-BILL",
                            credit_document_reference: "TEST-CN-ONE",
                            payee_name: "TEST Supplier",
                            recorded_by: "TEST-finance",
                            recorded_at: "2026-11-21T00:00:00Z",
                            reversal_id: null,
                          },
                        ],
                      }
                    : empty,
          ),
        ),
    ),
  );
  render(
    <PayableHistory
      scope={scope}
      id={bill}
      writable={false}
      onClose={vi.fn()}
      previous={{ disabled: true, onClick: vi.fn() }}
      next={{ disabled: true, onClick: vi.fn() }}
    />,
  );
  expect(await screen.findByText("TEST-CN-ONE")).toBeInTheDocument();
  expect(screen.getByText(/Reverses payment.*TEST-PAYMENT-REF/)).toBeInTheDocument();
  expect(screen.getByText("TEST received cartons")).toBeInTheDocument();
  for (const value of [receipt, payment, id(9), id(6), id(4)])
    expect(document.body).not.toHaveTextContent(value);
});
