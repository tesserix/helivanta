import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { PayableForm } from "./payable-forms";
const id = (n: number) => `0199e000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const scope = { tenantId: id(1), pharmacyId: id(2), outletId: id(3) };
const receipt = {
  id: id(4),
  owner_tenant_id: scope.tenantId,
  pharmacy_id: scope.pharmacyId,
  outlet_id: scope.outletId,
  batch_id: id(5),
  product_id: id(6),
  product_version: 1,
  currency: "INR",
  unit_code: "unit",
  quantity_units: 20,
  source_reference: "supplier-receipt:TEST",
  created_at: "2026-11-09T08:00:00Z",
  supplier: {
    shipment_id: id(7),
    order_id: id(8),
    source_product: "supply",
    original_accepted_units: 20,
    effective_accepted_units: 18,
    receipt_version: 2,
    summary_version: 4,
    receiving_closed: true,
    discrepancy_units: 2,
  },
};
afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
  sessionStorage.clear();
});
function setup() {
  const fetcher = vi.fn(async (url: string) => {
    if (url.endsWith("/permissions"))
      return new Response(
        JSON.stringify({
          tenant_id: scope.tenantId,
          subject: "TEST-finance",
          data: ["pharmacy.accounts.read", "pharmacy.accounts.write"],
        }),
      );
    if (url.includes("/accounts/acquisition-receipts"))
      return new Response(
        JSON.stringify({
          data: [
            receipt,
            { ...receipt, id: id(9), currency: "USD", source_reference: "TEST-USD" },
            { ...receipt, id: id(10), payable_id: id(11), source_reference: "TEST-COSTED" },
          ],
          page: { has_more: false, next_cursor: null },
        }),
      );
    return new Response(JSON.stringify({ message: "Unexpected stock access" }), { status: 403 });
  });
  vi.stubGlobal("fetch", fetcher);
  const view = renderWithProviders(
    <PayableForm scope={scope} defaultCurrency="INR" onSaved={vi.fn()} onCancel={vi.fn()} />,
  );
  return { ...view, fetcher };
}
it("lets finance-only users select receipt evidence and see current discrepancies", async () => {
  const { user, fetcher } = setup();
  await user.click(screen.getByRole("combobox", { name: "Category 1" }));
  await user.click(screen.getByRole("option", { name: "Inventory purchase" }));
  const picker = await screen.findByRole("combobox", { name: "Inventory receipt" });
  await user.click(picker);
  await user.click(await screen.findByRole("option", { name: /supplier-receipt:TEST/ }));
  expect(await screen.findByText(/18 accepted now/)).toBeInTheDocument();
  expect(screen.getByText(/2 units require receiving reconciliation/)).toBeInTheDocument();
  expect(screen.queryByRole("option", { name: /TEST-USD/ })).not.toBeInTheDocument();
  expect(screen.queryByRole("option", { name: /TEST-COSTED/ })).not.toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => url.includes("/stock/"))).toBe(false);
});
it("freezes a lost bill response and recovers the committed original without another write", async () => {
  let stored: Record<string, unknown> | null = null;
  const requests: { url: string; method: string }[] = [];
  const onSaved = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: "TEST-finance",
            data: ["pharmacy.accounts.read", "pharmacy.accounts.write"],
          }),
        );
      requests.push({ url, method: init?.method ?? "GET" });
      if (init?.method === "PUT") {
        stored = {
          ...JSON.parse(String(init.body)),
          id: url.split("/").at(-1),
          outlet_id: scope.outletId,
          actor_subject: "TEST-finance",
          created_at: "2026-11-09T08:00:00Z",
          line_count: 1,
          total_minor: 10000,
          paid_minor: 0,
          credit_minor: 0,
          outstanding_minor: 10000,
        };
        throw new TypeError("TEST response lost after commit");
      }
      return new Response(JSON.stringify(stored));
    }),
  );
  const { user } = renderWithProviders(
    <PayableForm scope={scope} defaultCurrency="INR" onSaved={onSaved} onCancel={vi.fn()} />,
  );
  await user.type(screen.getByLabelText("Payee name"), "TEST Supplier");
  await user.type(screen.getByLabelText("Payee reference"), "TEST-SUPPLIER");
  await user.type(screen.getByLabelText("Document reference"), "TEST-INVOICE");
  await user.type(screen.getByLabelText("Document date"), "2026-11-09");
  await user.type(screen.getByLabelText("Due date"), "2026-11-10");
  await user.type(screen.getByLabelText("Description 1"), "TEST external invoice");
  await user.clear(screen.getByLabelText("Net amount 1 (INR)"));
  await user.type(screen.getByLabelText("Net amount 1 (INR)"), "100.00");
  await user.click(screen.getByRole("button", { name: "Save bill or expense" }));
  const recover = await screen.findByRole("button", { name: "Recover original bill" });
  expect(screen.getByLabelText("Document reference")).toBeDisabled();
  expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
  await user.click(recover);
  await waitFor(() => expect(onSaved).toHaveBeenCalledWith(stored?.id));
  expect(requests.map((x) => x.method)).toEqual(["PUT", "GET"]);
  expect(requests[0].url).toBe(requests[1].url);
});
