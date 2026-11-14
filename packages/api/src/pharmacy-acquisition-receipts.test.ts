import { afterEach, expect, it, vi } from "vitest";
import { listAcquisitionReceipts } from "./pharmacy-acquisition-receipts";
const id = (n: number) => `0199e000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const scope = { tenantId: id(1), subject: "TEST-finance", pharmacyId: id(2), outletId: id(3) };
const direct = {
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
  source_reference: "TEST-RECEIPT",
  created_at: "2026-11-09T08:00:00Z",
};
const supplier = {
  ...direct,
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
const page = (data: unknown[], has_more = false, next_cursor: string | null = null) => ({
  data,
  page: { has_more, next_cursor },
});
afterEach(() => vi.unstubAllGlobals());
it("reads scoped finance receipts with the exact account and workspace", async () => {
  const fetcher = vi.fn(async () => new Response(JSON.stringify(page([direct, supplier]))));
  // Distinct receipt identities even when two receipts share a batch.
  const second = { ...supplier, id: id(9) };
  fetcher.mockResolvedValueOnce(new Response(JSON.stringify(page([direct, second]))));
  vi.stubGlobal("fetch", fetcher);
  expect((await listAcquisitionReceipts(scope)).data).toEqual([direct, second]);
  const [url, request] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(url).toContain(`/accounts/acquisition-receipts?limit=50`);
  const headers = new Headers(request.headers);
  expect(headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(headers.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});
it.each([
  ["foreign tenant", { ...direct, owner_tenant_id: id(10) }],
  ["foreign pharmacy", { ...direct, pharmacy_id: id(10) }],
  ["foreign outlet", { ...direct, outlet_id: id(10) }],
  [
    "inconsistent original quantity",
    { ...supplier, supplier: { ...supplier.supplier, original_accepted_units: 19 } },
  ],
  [
    "uncorrected quantity change",
    { ...supplier, supplier: { ...supplier.supplier, receipt_version: 1 } },
  ],
  [
    "unsupported source owner",
    { ...supplier, supplier: { ...supplier.supplier, source_product: "clinical" } },
  ],
])("refuses %s", async (_, row) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify(page([row])))),
  );
  await expect(listAcquisitionReceipts(scope)).rejects.toThrow();
});
it.each([page([direct, direct]), page([direct], true, null), page([direct], false, "unexpected")])(
  "refuses unsafe receipt pages",
  async (response) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify(response))),
    );
    await expect(listAcquisitionReceipts(scope)).rejects.toThrow();
  },
);
it("retains recorded payable linkage and current supplier discrepancies", async () => {
  const row = { ...supplier, payable_id: id(11) };
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify(page([row])))),
  );
  const value = (await listAcquisitionReceipts(scope, "signed-cursor")).data[0];
  expect(value.payable_id).toBe(id(11));
  expect(value.supplier?.effective_accepted_units).toBe(18);
  expect(value.supplier?.discrepancy_units).toBe(2);
});

it("recovers a committed bill with its original input after later partial payment", async () => {
  const { recoverAcquisitionBill } = await import("./pharmacy-acquisition-receipts");
  const input = {
    payee_reference: "TEST-SUPPLIER",
    payee_name: "TEST Supplier",
    document_reference: "TEST-INVOICE",
    issued_on: "2026-11-09",
    due_on: "2026-11-10",
    currency: "INR" as const,
    lines: [
      {
        category: "inventory" as const,
        receipt_id: direct.id,
        description: "TEST supplies",
        net_minor: 10000,
        tax_minor: 1800,
        recoverable_tax_minor: 0,
        landed_minor: 0,
      },
    ],
  };
  const row = {
    ...input,
    id: id(20),
    outlet_id: scope.outletId,
    actor_subject: scope.subject,
    created_at: "2026-11-09T08:00:00Z",
    line_count: 1,
    total_minor: 11800,
    paid_minor: 3000,
    credit_minor: 0,
    outstanding_minor: 8800,
  };
  const fetcher = vi.fn(async () => new Response(JSON.stringify(row)));
  vi.stubGlobal("fetch", fetcher);
  expect((await recoverAcquisitionBill(scope, row.id, input)).outstanding_minor).toBe(8800);
  expect((fetcher.mock.calls[0] as unknown as [string, RequestInit])[1]).not.toHaveProperty(
    "method",
    "PUT",
  );
});
it("rejects a recovered bill with changed original monetary evidence", async () => {
  const { recoverAcquisitionBill } = await import("./pharmacy-acquisition-receipts");
  const input = {
    payee_reference: "TEST-SUPPLIER",
    payee_name: "TEST Supplier",
    document_reference: "TEST-INVOICE",
    issued_on: "2026-11-09",
    due_on: "2026-11-10",
    currency: "INR" as const,
    lines: [
      {
        category: "other" as const,
        receipt_id: null,
        description: "TEST expense",
        net_minor: 10000,
        tax_minor: 0,
        recoverable_tax_minor: 0,
        landed_minor: 0,
      },
    ],
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            ...input,
            document_reference: "DIFFERENT",
            id: id(20),
            outlet_id: scope.outletId,
            actor_subject: scope.subject,
            created_at: "2026-11-09T08:00:00Z",
            line_count: 1,
            total_minor: 10000,
            paid_minor: 0,
            credit_minor: 0,
            outstanding_minor: 10000,
          }),
        ),
    ),
  );
  await expect(recoverAcquisitionBill(scope, id(20), input)).rejects.toThrow();
});

it.each([
  ["another outlet", { outlet_id: id(99) }],
  ["another original actor", { actor_subject: "OTHER-finance" }],
  ["incorrect line count", { line_count: 2 }],
  ["incorrect total", { total_minor: 10001, outstanding_minor: 10001 }],
  ["inconsistent balance", { outstanding_minor: 9999 }],
])("refuses a recovered bill with %s", async (_, changed) => {
  const { recoverAcquisitionBill } = await import("./pharmacy-acquisition-receipts");
  const input = {
    payee_reference: "TEST-SUPPLIER",
    payee_name: "TEST Supplier",
    document_reference: "TEST-INVOICE",
    issued_on: "2026-11-09",
    due_on: "2026-11-10",
    currency: "INR" as const,
    lines: [
      {
        category: "other" as const,
        receipt_id: null,
        description: "TEST expense",
        net_minor: 10000,
        tax_minor: 0,
        recoverable_tax_minor: 0,
        landed_minor: 0,
      },
    ],
  };
  const row = {
    ...input,
    id: id(20),
    outlet_id: scope.outletId,
    actor_subject: scope.subject,
    created_at: "2026-11-09T08:00:00Z",
    line_count: 1,
    total_minor: 10000,
    paid_minor: 0,
    credit_minor: 0,
    outstanding_minor: 10000,
    ...changed,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify(row))),
  );
  await expect(recoverAcquisitionBill(scope, id(20), input)).rejects.toThrow();
});
it("normalizes an omitted optional expense receipt to the owner's null representation", async () => {
  const { recordAcquisitionBill } = await import("./pharmacy-acquisition-receipts");
  const input = {
    payee_reference: "TEST-SUPPLIER",
    payee_name: "TEST Supplier",
    document_reference: "TEST-INVOICE",
    issued_on: "2026-11-09",
    due_on: "2026-11-10",
    currency: "INR" as const,
    lines: [
      {
        category: "other" as const,
        description: "TEST expense",
        net_minor: 10000,
        tax_minor: 0,
        recoverable_tax_minor: 0,
        landed_minor: 0,
      },
    ],
  };
  const row = {
    ...input,
    lines: [{ ...input.lines[0], receipt_id: null }],
    id: id(20),
    outlet_id: scope.outletId,
    actor_subject: scope.subject,
    created_at: "2026-11-09T08:00:00Z",
    line_count: 1,
    total_minor: 10000,
    paid_minor: 0,
    credit_minor: 0,
    outstanding_minor: 10000,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify(row))),
  );
  expect((await recordAcquisitionBill(scope, id(20), input)).id).toBe(id(20));
});
