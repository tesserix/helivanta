import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { act, screen, waitFor } from "@testing-library/react";
import { renderWithProviders as render } from "@helivanta/api/testing";
import { PayableCreditJournal } from "./payable-credit-journal";
const scope = {
  tenantId: "0199e000-0000-7000-8000-000000000001",
  pharmacyId: "0199e000-0000-7000-8000-000000000002",
  outletId: "0199e000-0000-7000-8000-000000000003",
  subject: "TEST-finance",
};
const bill = "0199e000-0000-7000-8000-000000000005";
const row = {
  id: "0199e000-0000-7000-8000-000000000006",
  credit_id: "0199e000-0000-7000-8000-000000000004",
  outlet_id: scope.outletId,
  payable_id: bill,
  expected_version: 1,
  expected_available_minor: 750,
  expected_payable_version: 1,
  expected_outstanding_minor: 1180,
  amount_minor: 500,
  occurred_on: "2026-11-18",
  reason: "TEST apply retained actual credit",
  version: 2,
  payable_version: 2,
  available_minor: 250,
  outstanding_minor: 680,
  payable_reference: "TEST-FUTURE-BILL",
  credit_document_reference: "TEST-CN-ONE",
  payee_name: "TEST External Supplier",
  recorded_by: scope.subject,
  recorded_at: "2026-11-21T00:00:00Z",
};
const reversal = {
  ...row,
  id: "0199e000-0000-7000-8000-000000000007",
  allocation_id: row.id,
  expected_version: 2,
  expected_available_minor: 250,
  expected_payable_version: 3,
  expected_outstanding_minor: 0,
  version: 3,
  payable_version: 4,
  available_minor: 750,
  outstanding_minor: 500,
  occurred_on: "2026-11-21",
  reason: "TEST correct application after payment",
};
const page = (data: unknown[], cursor: string | null = null) => ({
  data,
  page: { has_more: cursor !== null, next_cursor: cursor },
});
function stub(reversed = false) {
  const fetcher = vi.fn(async (url: string) =>
    url.includes("supplier-credit-allocations")
      ? new Response(JSON.stringify(page([{ ...row, reversal_id: reversed ? reversal.id : null }])))
      : url.endsWith("/reversal")
        ? new Response(JSON.stringify(reversal))
        : new Response(
            JSON.stringify({ error: { code: "forbidden", message: "TEST name unavailable" } }),
            { status: 403 },
          ),
  );
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  sessionStorage.clear();
  localStorage.clear();
});
it("shows a readonly supplier-credit application with human references and no raw record IDs", async () => {
  stub();
  render(<PayableCreditJournal scope={scope} payable={bill} currency="INR" />);
  expect(await screen.findByText("TEST-CN-ONE")).toBeInTheDocument();
  expect(screen.getByText("INR 5.00")).toBeInTheDocument();
  expect(screen.getByText(row.reason)).toBeInTheDocument();
  expect(screen.getByText("Active")).toBeInTheDocument();
  for (const value of [row.id, row.credit_id, row.payable_id, row.recorded_by])
    expect(document.body).not.toHaveTextContent(value);
  expect(screen.queryByRole("button", { name: /Save|Reverse full/ })).not.toBeInTheDocument();
});
it("reviews the exact retained reversal without treating it as a cash refund", async () => {
  const fetcher = stub(true);
  const { user } = render(<PayableCreditJournal scope={scope} payable={bill} currency="INR" />);
  await user.click(await screen.findByRole("button", { name: "View reversal for TEST-CN-ONE" }));
  expect(await screen.findByText(reversal.reason)).toBeInTheDocument();
  expect(screen.getByText(/Recorded payments are retained/)).toBeInTheDocument();
  expect(
    fetcher.mock.calls.some(([url]) =>
      url.includes(`/supplier-credits/${row.credit_id}/allocations/${row.id}/reversal`),
    ),
  ).toBe(true);
  expect(document.body).not.toHaveTextContent(reversal.id);
});
it("hides cached application and reversal evidence when a refreshed journal read is refused", async () => {
  let denied = false;
  const fetcher = stub(true);
  const original = fetcher.getMockImplementation()!;
  fetcher.mockImplementation(async (url) =>
    denied && url.includes("supplier-credit-allocations")
      ? new Response(JSON.stringify({ error: { code: "forbidden", message: "TEST revoked" } }), {
          status: 403,
        })
      : original(url),
  );
  const { user } = render(<PayableCreditJournal scope={scope} payable={bill} currency="INR" />);
  await user.click(await screen.findByRole("button", { name: "View reversal for TEST-CN-ONE" }));
  expect(await screen.findByText(reversal.reason)).toBeInTheDocument();
  denied = true;
  await user.click(screen.getByRole("button", { name: "Refresh bill and credit history" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/don't have access/);
  expect(screen.queryByText("TEST-CN-ONE")).not.toBeInTheDocument();
  expect(screen.queryByText(reversal.reason)).not.toBeInTheDocument();
});
it("loads a further bounded journal page without summing it as the bill balance", async () => {
  const fetcher = vi.fn(async (url: string) =>
    url.includes("supplier-credit-allocations")
      ? new Response(
          JSON.stringify(
            url.includes("cursor=")
              ? page([
                  {
                    ...row,
                    id: reversal.id,
                    credit_id: reversal.id,
                    credit_document_reference: "TEST-CN-TWO",
                    reversal_id: null,
                  },
                ])
              : page([{ ...row, reversal_id: null }], "TEST-cursor"),
          ),
        )
      : new Response("{}", { status: 403 }),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = render(<PayableCreditJournal scope={scope} payable={bill} currency="INR" />);
  await user.click(
    await screen.findByRole("button", { name: "Load more supplier credit applications" }),
  );
  expect(await screen.findByText("TEST-CN-TWO")).toBeInTheDocument();
  expect(screen.getByText("TEST-CN-ONE")).toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => url.includes("cursor=TEST-cursor"))).toBe(true);
  await waitFor(() =>
    expect(
      screen.queryByRole("button", { name: "Load more supplier credit applications" }),
    ).not.toBeInTheDocument(),
  );
});

it("clears old journal and reversal state immediately when the account changes", async () => {
  let resume: ((value: Response) => void) | undefined;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (!url.includes("supplier-credit-allocations")) return new Response("{}", { status: 403 });
      const subject = new Headers(options?.headers).get("X-Helivanta-Expected-Subject");
      if (subject === "TEST-new-finance")
        return new Promise<Response>((resolve) => {
          resume = resolve;
        });
      return new Response(JSON.stringify(page([{ ...row, reversal_id: null }])));
    }),
  );
  function AccountSwitcher() {
    const [subject, setSubject] = useState(scope.subject);
    return (
      <>
        <button onClick={() => setSubject("TEST-new-finance")}>Change account</button>
        <PayableCreditJournal scope={{ ...scope, subject }} payable={bill} currency="INR" />
      </>
    );
  }
  const { user } = render(<AccountSwitcher />);
  expect(await screen.findByText("TEST-CN-ONE")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Change account" }));
  expect(await screen.findByText("Loading supplier credit applications…")).toBeInTheDocument();
  expect(screen.queryByText("TEST-CN-ONE")).not.toBeInTheDocument();
  await act(async () => resume!(new Response(JSON.stringify(page([])))));
  expect(
    await screen.findByText("No managed supplier credits applied to this bill."),
  ).toBeInTheDocument();
});
