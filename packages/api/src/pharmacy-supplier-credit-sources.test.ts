import { afterEach, expect, it, vi } from "vitest";
import { apiFetch } from "./client";
import * as client from "./pharmacy-supplier-credits";

vi.mock("./client", () => ({ apiFetch: vi.fn() }));
afterEach(() => vi.resetAllMocks());
const scope = {
  tenantId: "0199e000-0000-7000-8000-000000000001",
  pharmacyId: "0199e000-0000-7000-8000-000000000002",
  outletId: "0199e000-0000-7000-8000-000000000003",
  subject: "TEST-reader",
};
const credit = "0199e000-0000-7000-8000-000000000004";
const source = "0199e000-0000-7000-8000-000000000005";
const context = {
  id: credit,
  outlet_id: scope.outletId,
  return_id: source,
  return_number: 12,
  payee_reference: "TEST-SUPPLIER",
  payee_name: "TEST External Supplier",
  original_bill_reference: "TEST-ORIGINAL-BILL",
  document_reference: "TEST-CN-ONE",
  issued_on: "2026-11-18",
  amount_minor: 750,
  currency: "INR",
  reason: "TEST actual credit",
  correction_of: null,
  recorded_by: "TEST-writer",
  recorded_at: "2026-11-19T00:00:00Z",
  version: 1,
  state: "active",
  matched_minor: 0,
  allocated_minor: 0,
  available_minor: 750,
  local_date: "2026-11-22",
  as_of: "2026-11-21T20:00:00Z",
  timezone: "Asia/Kolkata",
};
const payable = {
  id: source,
  outlet_id: scope.outletId,
  payee_name: "TEST External Supplier",
  document_reference: "TEST-FUTURE-BILL",
  issued_on: "2026-11-18",
  currency: "INR",
  due_on: "2026-12-18",
  total_minor: 1180,
  paid_minor: 100,
  credit_minor: 200,
  outstanding_minor: 880,
  version: 4,
};
const returned = {
  id: source,
  outlet_id: scope.outletId,
  number: 12,
  payee_name: "TEST External Supplier",
  original_bill_reference: "TEST-ORIGINAL-BILL",
  currency: "INR",
  expected_credit_minor: 1000,
  matched_minor: 1200,
};
const root = `/pharmacy/pharmacies/${scope.pharmacyId}/outlets/${scope.outletId}/accounts/supplier-credits/${credit}`;

it("loads the owner's current credit and date across the India midnight boundary", async () => {
  vi.mocked(apiFetch).mockResolvedValue(context);
  expect(await client.readPharmacySupplierCreditContext(scope, credit)).toEqual(context);
  expect(apiFetch).toHaveBeenCalledWith(
    `${root}/context`,
    expect.objectContaining({ cache: "no-store" }),
  );
});
it("loads current bill observations and dispatched return matching choices", async () => {
  const payables = { data: [payable], page: { has_more: false, next_cursor: null } };
  vi.mocked(apiFetch).mockResolvedValue(payables);
  expect(await client.listPharmacySupplierCreditPayableChoices(scope, credit)).toEqual(payables);
  expect(apiFetch).toHaveBeenLastCalledWith(
    `${root}/eligible-payables?limit=50`,
    expect.objectContaining({ cache: "no-store" }),
  );
  const returns = { data: [returned], page: { has_more: true, next_cursor: "TEST-next" } };
  vi.mocked(apiFetch).mockResolvedValue(returns);
  expect(await client.listPharmacySupplierCreditReturnChoices(scope, credit, "TEST-prior")).toEqual(
    returns,
  );
  expect(apiFetch).toHaveBeenLastCalledWith(
    `${root}/eligible-returns?limit=50&cursor=TEST-prior`,
    expect.objectContaining({ cache: "no-store" }),
  );
});

it.each([
  { id: source },
  { outlet_id: source },
  { available_minor: 751 },
  { version: 0 },
  { local_date: "2026-11-21" },
  { local_date: "2026-02-30" },
  { as_of: "not-a-timestamp" },
  { timezone: "TEST-not-a-zone" },
])("refuses inconsistent credit/date context: %j", async (patch) => {
  vi.mocked(apiFetch).mockResolvedValue({ ...context, ...patch });
  await expect(client.readPharmacySupplierCreditContext(scope, credit)).rejects.toThrow();
});
it.each([
  { total_minor: 1181 },
  { paid_minor: 101 },
  { credit_minor: 201 },
  { outstanding_minor: 881 },
  { outstanding_minor: 0 },
  { version: 0 },
  { outlet_id: source },
  { currency: "USD" },
])("refuses invalid payable observations: %j", async (patch) => {
  vi.mocked(apiFetch).mockResolvedValue({
    data: [{ ...payable, ...patch }],
    page: { has_more: false, next_cursor: null },
  });
  await expect(client.listPharmacySupplierCreditPayableChoices(scope, credit)).rejects.toThrow();
});
it.each([
  { number: 0 },
  { expected_credit_minor: -1 },
  { matched_minor: -1 },
  { outlet_id: source },
  { currency: "USD" },
])("refuses invalid dispatched return choices: %j", async (patch) => {
  vi.mocked(apiFetch).mockResolvedValue({
    data: [{ ...returned, ...patch }],
    page: { has_more: false, next_cursor: null },
  });
  await expect(client.listPharmacySupplierCreditReturnChoices(scope, credit)).rejects.toThrow();
});
it.each(["payables", "returns"])(
  "refuses duplicate, oversized and inconsistent %s pages",
  async (kind) => {
    const row = kind === "payables" ? payable : returned;
    const read =
      kind === "payables"
        ? client.listPharmacySupplierCreditPayableChoices
        : client.listPharmacySupplierCreditReturnChoices;
    for (const page of [
      { data: [row, row], page: { has_more: false, next_cursor: null } },
      { data: Array.from({ length: 51 }, () => row), page: { has_more: false, next_cursor: null } },
      { data: [], page: { has_more: true, next_cursor: "TEST-next" } },
      { data: [row], page: { has_more: true, next_cursor: "TEST-prior" } },
      { data: [row], page: { has_more: false, next_cursor: "TEST-next" } },
    ]) {
      vi.mocked(apiFetch).mockResolvedValue(page);
      await expect(read(scope, credit, "TEST-prior")).rejects.toThrow();
    }
  },
);
it("preserves empty authorised collections and owner read failures", async () => {
  const page = { data: [], page: { has_more: false, next_cursor: null } };
  vi.mocked(apiFetch).mockResolvedValue(page);
  expect(await client.listPharmacySupplierCreditPayableChoices(scope, credit)).toEqual(page);
  expect(await client.listPharmacySupplierCreditReturnChoices(scope, credit)).toEqual(page);
  const error = new Error("TEST owner unavailable");
  vi.mocked(apiFetch).mockRejectedValue(error);
  await expect(client.readPharmacySupplierCreditContext(scope, credit)).rejects.toBe(error);
});

const allocationId = "0199e000-0000-7000-8000-000000000006";
const allocated = {
  id: allocationId,
  outlet_id: scope.outletId,
  credit_id: credit,
  payable_id: source,
  expected_version: 1,
  expected_available_minor: 750,
  expected_payable_version: 1,
  expected_outstanding_minor: 1180,
  amount_minor: 500,
  occurred_on: "2026-11-18",
  reason: "TEST allocate",
  version: 2,
  payable_version: 2,
  available_minor: 250,
  outstanding_minor: 680,
  payable_reference: payable.document_reference,
  credit_document_reference: context.document_reference,
  payee_name: context.payee_name,
  recorded_by: "TEST-writer",
  recorded_at: context.recorded_at,
  reversal_id: null,
};
const paidContext = {
  credit: { ...context, version: 2, allocated_minor: 500, available_minor: 250 },
  allocation: allocated,
  payable: { ...payable, paid_minor: 680, credit_minor: 500, outstanding_minor: 0, version: 3 },
};
it("loads a paid bill's current context without mistaking the original allocation for today's balances", async () => {
  vi.mocked(apiFetch).mockResolvedValue(paidContext);
  expect(
    await client.readPharmacySupplierCreditAllocationContext(scope, credit, allocationId),
  ).toEqual(paidContext);
  expect(apiFetch).toHaveBeenLastCalledWith(
    `${root}/allocations/${allocationId}/context`,
    expect.objectContaining({ cache: "no-store" }),
  );
});

it.each([
  { credit: { ...paidContext.credit, id: source } },
  { allocation: { ...allocated, id: source } },
  { payable: { ...paidContext.payable, id: allocationId } },
  { payable: { ...paidContext.payable, outlet_id: source } },
  { payable: { ...paidContext.payable, version: 1 } },
  { payable: { ...paidContext.payable, paid_minor: 679 } },
  { payable: { ...paidContext.payable, outstanding_minor: -1 } },
  { payable: { ...paidContext.payable, currency: "USD" } },
  { payable: { ...paidContext.payable, document_reference: "TEST-other-bill" } },
  { credit: { ...paidContext.credit, document_reference: "TEST-other-credit" } },
  { credit: { ...paidContext.credit, payee_name: "TEST-other-supplier" } },
  { credit: { ...paidContext.credit, local_date: "2026-11-21" } },
  { credit: { ...paidContext.credit, allocated_minor: 499, available_minor: 251 } },
  { payable: { ...paidContext.payable, credit_minor: 499, paid_minor: 681 } },
  { allocation: { ...allocated, reversal_id: credit } },
])("refuses mismatched current allocation context", async (patch) => {
  vi.mocked(apiFetch).mockResolvedValue({ ...paidContext, ...patch });
  await expect(
    client.readPharmacySupplierCreditAllocationContext(scope, credit, allocationId),
  ).rejects.toThrow();
});
it("keeps original evidence after reversal and later void while using current bill observations", async () => {
  const reversed = {
    credit: { ...paidContext.credit, version: 3, allocated_minor: 0, available_minor: 750 },
    allocation: { ...allocated, reversal_id: credit },
    payable: { ...paidContext.payable, version: 4, credit_minor: 0, outstanding_minor: 500 },
  };
  vi.mocked(apiFetch).mockResolvedValue(reversed);
  expect(
    await client.readPharmacySupplierCreditAllocationContext(scope, credit, allocationId),
  ).toEqual(reversed);
  const voided = {
    ...reversed,
    credit: { ...reversed.credit, version: 4, state: "voided", available_minor: 0 },
  };
  vi.mocked(apiFetch).mockResolvedValue(voided);
  expect(
    await client.readPharmacySupplierCreditAllocationContext(scope, credit, allocationId),
  ).toEqual(voided);
});
it("uses owner identity evidence rather than requiring identical historical supplier display names", async () => {
  const renamed = {
    ...paidContext,
    payable: { ...paidContext.payable, payee_name: "TEST Supplier billing name" },
  };
  vi.mocked(apiFetch).mockResolvedValue(renamed);
  expect(
    await client.readPharmacySupplierCreditAllocationContext(scope, credit, allocationId),
  ).toEqual(renamed);
});

it("lists dispatched sources before a first credit exists using only finance scope", async () => {
  const page = { data: [returned], page: { has_more: false, next_cursor: null } };
  vi.mocked(apiFetch).mockResolvedValue(page);
  expect(await client.listPharmacySupplierCreditInitialSources(scope)).toEqual(page);
  expect(apiFetch).toHaveBeenLastCalledWith(
    `/pharmacy/pharmacies/${scope.pharmacyId}/outlets/${scope.outletId}/accounts/supplier-credits/source-returns?limit=50`,
    expect.objectContaining({
      cache: "no-store",
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
    }),
  );
});
