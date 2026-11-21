import { fireEvent, screen, within } from "@testing-library/react";
import { renderWithProviders as renderBase } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import type { ReactElement } from "react";
import { SupplierCreditHistory } from "./supplier-credit-history";

const render = (ui: ReactElement) =>
  renderBase(<NavigationGuardProvider>{ui}</NavigationGuardProvider>);
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/pharmacy/accounts",
  useSearchParams: () => new URLSearchParams("tab=supplier-credits"),
}));
const scope = {
  tenantId: "0199e000-0000-7000-8000-000000000001",
  pharmacyId: "0199e000-0000-7000-8000-000000000002",
  outletId: "0199e000-0000-7000-8000-000000000003",
  subject: "TEST-finance",
};
const row = {
  id: "0199e000-0000-7000-8000-000000000004",
  outlet_id: scope.outletId,
  return_id: "0199e000-0000-7000-8000-000000000005",
  return_number: 17,
  payee_reference: "TEST-SUPPLIER",
  payee_name: "TEST Supplier",
  original_bill_reference: "TEST-ORIGINAL-BILL",
  document_reference: "TEST-CN-ONE",
  issued_on: "2026-11-18",
  amount_minor: 750,
  currency: "INR",
  reason: "TEST accepted actual supplier note",
  correction_of: null,
  recorded_by: "TEST-account-subject-never-display",
  recorded_at: "2026-11-21T00:00:00Z",
  version: 3,
  state: "active",
  matched_minor: 300,
  allocated_minor: 200,
  available_minor: 550,
};
afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
});
it("shows separate actual, matched, applied and unused values with human references in retained finance history", async () => {
  const fetcher = vi.fn(
    async (url: string) =>
      new Response(
        JSON.stringify(
          url.includes("/context")
            ? {
                ...row,
                local_date: "2026-11-22",
                as_of: "2026-11-21T20:00:00Z",
                timezone: "Asia/Kolkata",
              }
            : {
                data: url.includes("/matches?") || url.includes("/allocations?") ? [] : [row],
                page: { has_more: false, next_cursor: null },
              },
        ),
      ),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = render(
    <SupplierCreditHistory scope={scope} canRecord={false} timeZone="Asia/Kolkata" />,
  );
  await user.click(await screen.findByRole("button", { name: "Open credit TEST-CN-ONE" }));
  const detail = within(await screen.findByRole("region", { name: "Supplier credit details" }));
  expect(await detail.findByText("Actual credit")).toBeInTheDocument();
  expect(detail.getByText("Matched to returns")).toBeInTheDocument();
  expect(detail.getByText("Applied to bills")).toBeInTheDocument();
  expect(detail.getByText("Unused credit")).toBeInTheDocument();
  expect(await detail.findByRole("heading", { name: "Credit void" })).toBeInTheDocument();
  for (const value of ["INR 7.50", "INR 3.00", "INR 2.00", "INR 5.50"])
    expect(detail.getByText(value)).toBeInTheDocument();
  expect(detail.getByText(/Original bill TEST-ORIGINAL-BILL/)).toBeInTheDocument();
  expect(
    await detail.findByText("No bill applications recorded for this credit."),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Record supplier credit" })).not.toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => url.includes("source-returns"))).toBe(false);
  for (const value of [
    row.recorded_by,
    row.id,
    row.return_id,
    scope.tenantId,
    scope.pharmacyId,
    scope.outletId,
  ])
    expect(document.body).not.toHaveTextContent(value);
});

it("hides a previously verified credit reference when financial history access is refused", async () => {
  let refused = false;
  const source = {
    id: row.return_id,
    outlet_id: scope.outletId,
    number: 17,
    payee_name: row.payee_name,
    original_bill_reference: row.original_bill_reference,
    currency: "INR",
    expected_credit_minor: 1500,
    matched_minor: 0,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === "PUT") {
        refused = true;
        return new Response(
          JSON.stringify({
            ...JSON.parse(options.body as string),
            id: url.split("/").at(-1),
            outlet_id: scope.outletId,
            return_number: 17,
            payee_reference: row.payee_reference,
            payee_name: row.payee_name,
            original_bill_reference: row.original_bill_reference,
            recorded_by: scope.subject,
            recorded_at: row.recorded_at,
          }),
        );
      }
      if (refused)
        return new Response(
          JSON.stringify({ error: "forbidden", message: "TEST current financial access denied" }),
          { status: 403 },
        );
      return new Response(
        JSON.stringify({
          data: url.includes("source-returns") ? [source] : [],
          page: { has_more: false, next_cursor: null },
        }),
      );
    }),
  );
  const { user } = render(
    <SupplierCreditHistory scope={scope} canRecord timeZone="Asia/Kolkata" />,
  );
  await user.click(await screen.findByRole("button", { name: "Record supplier credit" }));
  await user.click(await screen.findByRole("combobox", { name: "Dispatched supplier return" }));
  await user.click(await screen.findByRole("option", { name: /Return 17.*TEST Supplier/ }));
  await user.type(screen.getByLabelText("Supplier credit reference"), "TEST-CN-REVOKED");
  fireEvent.change(screen.getByLabelText("Credit issue date"), { target: { value: "2026-11-18" } });
  await user.type(screen.getByLabelText("Actual credit amount (INR)"), "7.50");
  await user.type(screen.getByLabelText("Credit record reason"), "TEST actual note");
  await user.click(screen.getByRole("button", { name: "Save supplier credit" }));
  expect(
    await within(
      screen.getByRole("heading", { name: "Supplier credits" }).parentElement!,
    ).findByText(/don't have access/),
  ).toBeInTheDocument();
  expect(document.body).not.toHaveTextContent("TEST-CN-REVOKED");
  expect(screen.queryByRole("region", { name: "Supplier credit details" })).not.toBeInTheDocument();
});

it("opens replacement entry from a named voided credit detail without any raw-ID or supplier lookup", async () => {
  const voided = {
    ...row,
    state: "voided",
    version: 4,
    matched_minor: 0,
    allocated_minor: 0,
    available_minor: 0,
  };
  const fetcher = vi.fn(
    async (url: string) =>
      new Response(
        JSON.stringify(
          url.endsWith("/context")
            ? {
                ...voided,
                local_date: "2026-11-22",
                timezone: "Asia/Kolkata",
                as_of: "2026-11-22T00:00:00Z",
              }
            : url.endsWith("/void")
              ? {
                  id: "0199e000-0000-7000-8000-000000000009",
                  outlet_id: scope.outletId,
                  credit_id: row.id,
                  expected_version: 3,
                  version: 4,
                  reason: "TEST incorrect supplier amount",
                  recorded_by: scope.subject,
                  recorded_at: row.recorded_at,
                }
              : {
                  data: url.includes("/matches?") || url.includes("/allocations?") ? [] : [voided],
                  page: { has_more: false, next_cursor: null },
                },
        ),
      ),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = render(
    <SupplierCreditHistory scope={scope} canRecord timeZone="Asia/Kolkata" />,
  );
  await user.click(await screen.findByRole("button", { name: "Open credit TEST-CN-ONE" }));
  await user.click(await screen.findByRole("button", { name: "Replace voided credit" }));
  expect(
    await screen.findByRole("heading", { name: "Replace voided supplier credit" }),
  ).toBeInTheDocument();
  expect(screen.getByText(/Replacing TEST-CN-ONE/)).toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => url.includes("source-returns"))).toBe(false);
});
