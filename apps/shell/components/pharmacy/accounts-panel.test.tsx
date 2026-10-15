import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { PharmacyWorkspace } from "./pharmacy-workspace";
const pharmacy = "0199e000-0000-7000-8000-000000000010",
  outlet = "0199e000-0000-7000-8000-000000000001";
const id = "0199e000-0000-7000-8000-000000000020";
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
});
function mockAccounts(financial = true, country = "IN") {
  const fetcher = vi.fn(async (url: string) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({
          tenant_id: "11111111-1111-1111-1111-111111111111",
          subject: "TEST-finance",
          data: [
            "pharmacy.catalogue.read",
            ...(financial
              ? [
                  "pharmacy.accounts.read",
                  "pharmacy.accounts.write",
                  "pharmacy.accounts.payment",
                  "pharmacy.accounts.adjust",
                ]
              : []),
          ],
        }),
      );
    if (url.endsWith("/pharmacies"))
      return new Response(
        JSON.stringify(page([{ id: pharmacy, name: "TEST Independent Pharmacy" }])),
      );
    if (url.endsWith("/outlets"))
      return new Response(
        JSON.stringify(page([{ id: outlet, name: "TEST Main Outlet", country_code: country }])),
      );
    if (url.endsWith("/access")) return new Response(JSON.stringify({ level: "editor" }));
    if (url.endsWith("/accounts/payables"))
      return new Response(
        JSON.stringify(
          page([
            {
              id,
              payee_reference: "TEST-SUPPLIER",
              payee_name: "TEST Supplier",
              document_reference: "TEST-EXPENSE-001",
              currency: "INR",
              issued_on: "2026-10-01",
              due_on: "2026-10-31",
              total_minor: 11800,
              paid_minor: 3000,
              credit_minor: 800,
              outstanding_minor: 8000,
              line_count: 1,
              created_at: "2026-10-15T00:00:00Z",
            },
          ]),
        ),
      );
    if (url.endsWith("/inventory-audit"))
      return new Response(
        JSON.stringify(
          page([
            {
              id,
              sku: "TEST-500",
              brand_name: "TEST Capsule",
              lot_number: "TEST-LOT",
              expires_on: "2030-10-31",
              currency: "INR",
              unit_of_issue: "capsule",
              received_units: 25,
              on_hand_units: 25,
              uncosted_receipt_units: 25,
              acquisition_cost_minor: "0",
              valuation_minor: null,
              valuation_status: "cost_missing",
            },
          ]),
        ),
      );
    return new Response(JSON.stringify(page([])));
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
it("shows paid, credited and remaining amounts without valuing uncosted drugs", async () => {
  mockAccounts();
  renderWithProviders(<PharmacyWorkspace view="accounts" />);
  expect(await screen.findByText("TEST-EXPENSE-001")).toBeInTheDocument();
  expect(screen.getByText("TEST Supplier")).toBeInTheDocument();
  expect(screen.getByRole("columnheader", { name: "Outstanding" })).toBeInTheDocument();
  expect(screen.getByText("Purchase cost missing")).toBeInTheDocument();
  expect(screen.getByText("25 uncosted units")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Record bill or expense" })).toBeInTheDocument();
});
it("does not fetch confidential costs without financial read permission", async () => {
  const fetcher = mockAccounts(false);
  renderWithProviders(<PharmacyWorkspace view="accounts" />);
  expect(await screen.findByText("Financial access required")).toBeInTheDocument();
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([url]) => url.includes("/accounts/"))).toBe(false),
  );
  expect(screen.queryByText("TEST Supplier")).not.toBeInTheDocument();
});

it("focuses the expense form and records exact minor amounts with a stable retry ID", async () => {
  const base = mockAccounts();
  const writes: { url: string; body: Record<string, unknown> }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        const body = JSON.parse(init.body as string);
        writes.push({ url, body });
        return new Response(
          JSON.stringify({ error: "unavailable", message: "TEST record unavailable" }),
          { status: 503 },
        );
      }
      return base(url);
    }),
  );
  const { user } = renderWithProviders(<PharmacyWorkspace view="accounts" />);
  await user.click(await screen.findByRole("button", { name: "Record bill or expense" }));
  const payee = await screen.findByLabelText("Payee name");
  expect(payee).toHaveFocus();
  await user.type(payee, "TEST Supplier");
  await user.type(screen.getByLabelText("Payee reference"), "TEST-SUPPLIER");
  await user.type(screen.getByLabelText("Document reference"), "TEST-LOCAL-BILL");
  await user.type(screen.getByLabelText("Document date"), "2026-10-01");
  await user.type(screen.getByLabelText("Due date"), "2026-10-31");
  await user.type(screen.getByLabelText("Description 1"), "TEST Electricity");
  await user.clear(screen.getByLabelText("Net amount 1 (INR)"));
  await user.type(screen.getByLabelText("Net amount 1 (INR)"), "125.25");
  await user.click(screen.getByRole("button", { name: "Save bill or expense" }));
  expect(await screen.findByText("TEST record unavailable")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Save bill or expense" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0].url).toBe(writes[1].url);
  expect(writes[0].body.lines).toEqual([
    expect.objectContaining({ net_minor: 12525, tax_minor: 0 }),
  ]);
  expect(writes[0].url).toMatch(/\/payables\/[0-9a-f-]{14}7[0-9a-f-]+$/);
});

it("defaults Pakistan expenses to PKR instead of an India fallback", async () => {
  mockAccounts(true, "PK");
  const { user } = renderWithProviders(<PharmacyWorkspace view="accounts" />);
  await user.click(await screen.findByRole("button", { name: "Record bill or expense" }));
  expect(screen.getByLabelText("Currency")).toHaveTextContent("PKR");
  expect(screen.getByLabelText("Net amount 1 (PKR)")).toBeInTheDocument();
});
