import { afterEach, expect, it, vi } from "vitest";
import { apiFetch } from "./client";
import * as client from "./pharmacy-supplier-credits";

vi.mock("./client", () => ({ apiFetch: vi.fn() }));
afterEach(() => vi.resetAllMocks());
const scope = {
  tenantId: "0199e000-0000-7000-8000-000000000001",
  pharmacyId: "0199e000-0000-7000-8000-000000000002",
  outletId: "0199e000-0000-7000-8000-000000000003",
  subject: "TEST-finance",
};
const credit = "0199e000-0000-7000-8000-000000000004";
const payable = "0199e000-0000-7000-8000-000000000005";
const allocation = "0199e000-0000-7000-8000-000000000006";
const entry = "0199e000-0000-7000-8000-000000000007";
const input = {
  payable_id: payable,
  expected_version: 1,
  expected_available_minor: 750,
  expected_payable_version: 1,
  expected_outstanding_minor: 1180,
  amount_minor: 500,
  occurred_on: "2026-11-18",
  reason: "TEST allocate retained actual credit",
};
const receipt = {
  ...input,
  id: allocation,
  outlet_id: scope.outletId,
  credit_id: credit,
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
const root = `/pharmacy/pharmacies/${scope.pharmacyId}/outlets/${scope.outletId}/accounts/supplier-credits/${credit}/allocations`;

it("allocates retained credit with exact owner observations and verifies its receipt", async () => {
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  expect(await client.allocatePharmacySupplierCredit(scope, credit, allocation, input)).toEqual(
    receipt,
  );
  expect(apiFetch).toHaveBeenCalledWith(
    `${root}/${allocation}`,
    expect.objectContaining({
      method: "PUT",
      cache: "no-store",
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
    }),
  );
  expect(JSON.parse(vi.mocked(apiFetch).mock.calls[0]![1]!.body as string)).toEqual(input);
});

it.each([
  ["wrong allocation", { id: entry }],
  ["wrong credit", { credit_id: entry }],
  ["wrong outlet", { outlet_id: entry }],
  ["another actor", { recorded_by: "TEST-other" }],
  ["different bill", { payable_id: entry }],
  ["changed amount", { amount_minor: 501, available_minor: 249, outstanding_minor: 679 }],
  ["changed available observation", { expected_available_minor: 751, available_minor: 251 }],
  ["changed outstanding observation", { expected_outstanding_minor: 1181, outstanding_minor: 681 }],
  ["changed credit version", { expected_version: 2, version: 3 }],
  ["changed bill version", { expected_payable_version: 2, payable_version: 3 }],
  ["inconsistent credit version", { version: 3 }],
  ["inconsistent bill version", { payable_version: 3 }],
  ["inconsistent credit pool", { available_minor: 251 }],
  ["inconsistent liability", { outstanding_minor: 681 }],
  ["changed date", { occurred_on: "2026-11-19" }],
  ["changed reason", { reason: "TEST not submitted" }],
])("refuses %s in allocation results", async (_name, patch) => {
  vi.mocked(apiFetch).mockResolvedValue({ ...receipt, ...patch });
  await expect(
    client.allocatePharmacySupplierCredit(scope, credit, allocation, input),
  ).rejects.toThrow();
  expect(apiFetch).toHaveBeenCalledTimes(1);
});

it.each([
  { amount_minor: 751 },
  { expected_outstanding_minor: 499 },
  { expected_version: 0 },
  { expected_payable_version: 0 },
  { expected_available_minor: -1 },
  { amount_minor: 0 },
  { occurred_on: "2026-02-30" },
  { reason: " " },
  { payable_id: "TEST-not-a-record" },
])("rejects invalid or unconserved intent before sending: %j", async (patch) => {
  await expect(
    client.allocatePharmacySupplierCredit(scope, credit, allocation, { ...input, ...patch }),
  ).rejects.toThrow();
  expect(apiFetch).not.toHaveBeenCalled();
});

it("reads allocations across actors but recovers only the original actor", async () => {
  const view = { ...receipt, recorded_by: "TEST-other", reversal_id: entry };
  vi.mocked(apiFetch).mockResolvedValue(view);
  expect(await client.readPharmacySupplierCreditAllocation(scope, credit, allocation)).toEqual(
    view,
  );
  vi.mocked(apiFetch).mockResolvedValue({ ...receipt, recorded_by: "TEST-other" });
  await expect(
    client.recoverPharmacySupplierCreditAllocation(scope, credit, allocation),
  ).rejects.toThrow();
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  expect(await client.recoverPharmacySupplierCreditAllocation(scope, credit, allocation)).toEqual(
    receipt,
  );
  expect(apiFetch).toHaveBeenLastCalledWith(
    `${root}/${allocation}/recovery`,
    expect.objectContaining({ cache: "no-store" }),
  );
});

const reverseInput = {
  expected_version: 2,
  expected_available_minor: 250,
  expected_payable_version: 3,
  expected_outstanding_minor: 0,
  occurred_on: "2026-11-20",
  reason: "TEST reverse allocation after payment",
};
const reversal = {
  ...reverseInput,
  id: entry,
  outlet_id: scope.outletId,
  credit_id: credit,
  allocation_id: allocation,
  payable_id: payable,
  amount_minor: 500,
  version: 3,
  payable_version: 4,
  available_minor: 750,
  outstanding_minor: 500,
  payable_reference: receipt.payable_reference,
  credit_document_reference: receipt.credit_document_reference,
  payee_name: receipt.payee_name,
  recorded_by: scope.subject,
  recorded_at: receipt.recorded_at,
};
it("reverses the entire original allocation while preserving intervening payment evidence", async () => {
  vi.mocked(apiFetch).mockResolvedValue(reversal);
  expect(
    await client.reversePharmacySupplierCreditAllocation(
      scope,
      credit,
      receipt,
      entry,
      reverseInput,
    ),
  ).toEqual(reversal);
  expect(apiFetch).toHaveBeenCalledWith(
    `${root}/${allocation}/reversals/${entry}`,
    expect.objectContaining({ method: "PUT", cache: "no-store" }),
  );
  expect(JSON.parse(vi.mocked(apiFetch).mock.calls[0]![1]!.body as string)).toEqual(reverseInput);
});

it.each([
  ["wrong reversal", { id: payable }],
  ["wrong allocation", { allocation_id: entry }],
  ["wrong credit", { credit_id: entry }],
  ["wrong outlet", { outlet_id: entry }],
  ["another actor", { recorded_by: "TEST-other" }],
  ["changed bill", { payable_id: entry }],
  ["changed original amount", { amount_minor: 499, available_minor: 749, outstanding_minor: 499 }],
  ["inconsistent credit", { available_minor: 751 }],
  ["inconsistent liability", { outstanding_minor: 501 }],
  ["changed credit version", { expected_version: 3, version: 4 }],
  ["changed bill version", { expected_payable_version: 4, payable_version: 5 }],
  ["inconsistent credit version", { version: 4 }],
  ["inconsistent bill version", { payable_version: 5 }],
  ["changed date", { occurred_on: "2026-11-21" }],
  ["changed reason", { reason: "TEST not submitted" }],
  ["changed bill reference", { payable_reference: "TEST-OTHER-BILL" }],
  ["changed credit reference", { credit_document_reference: "TEST-OTHER-CREDIT" }],
  ["changed supplier", { payee_name: "TEST Other supplier" }],
])("refuses %s in allocation reversals", async (_name, patch) => {
  vi.mocked(apiFetch).mockResolvedValue({ ...reversal, ...patch });
  await expect(
    client.reversePharmacySupplierCreditAllocation(scope, credit, receipt, entry, reverseInput),
  ).rejects.toThrow();
  expect(apiFetch).toHaveBeenCalledTimes(1);
});

it("lists unique bounded allocation history and retains original reversal evidence", async () => {
  const page = {
    data: [{ ...receipt, reversal_id: entry }],
    page: { has_more: true, next_cursor: "TEST-next" },
  };
  vi.mocked(apiFetch).mockResolvedValue(page);
  expect(await client.listPharmacySupplierCreditAllocations(scope, credit, "TEST-before")).toEqual(
    page,
  );
  expect(apiFetch).toHaveBeenLastCalledWith(
    `${root}?limit=50&cursor=TEST-before`,
    expect.objectContaining({ cache: "no-store" }),
  );
  vi.mocked(apiFetch).mockResolvedValue({ ...reversal, recorded_by: "TEST-other" });
  expect(
    await client.readPharmacySupplierCreditAllocationReversal(scope, credit, allocation),
  ).toEqual({ ...reversal, recorded_by: "TEST-other" });
  await expect(
    client.recoverPharmacySupplierCreditAllocationReversal(scope, credit, allocation, entry),
  ).rejects.toThrow();
  vi.mocked(apiFetch).mockResolvedValue(reversal);
  expect(
    await client.recoverPharmacySupplierCreditAllocationReversal(scope, credit, allocation, entry),
  ).toEqual(reversal);
  expect(apiFetch).toHaveBeenLastCalledWith(
    `${root}/${allocation}/reversals/${entry}/recovery`,
    expect.objectContaining({ cache: "no-store" }),
  );
});

it.each([
  {
    data: [
      { ...receipt, reversal_id: null },
      { ...receipt, reversal_id: null },
    ],
    page: { has_more: false, next_cursor: null },
  },
  { data: [], page: { has_more: true, next_cursor: "TEST-next" } },
  {
    data: [{ ...receipt, reversal_id: null }],
    page: { has_more: true, next_cursor: "TEST-prior" },
  },
  {
    data: [{ ...receipt, reversal_id: null }],
    page: { has_more: false, next_cursor: "TEST-next" },
  },
  {
    data: [{ ...receipt, outlet_id: entry, reversal_id: null }],
    page: { has_more: false, next_cursor: null },
  },
  {
    data: [{ ...receipt, outstanding_minor: 681, reversal_id: null }],
    page: { has_more: false, next_cursor: null },
  },
  {
    data: Array.from({ length: 51 }, () => ({ ...receipt, reversal_id: null })),
    page: { has_more: false, next_cursor: null },
  },
])("refuses inconsistent allocation collection pages", async (page) => {
  vi.mocked(apiFetch).mockResolvedValue(page);
  await expect(
    client.listPharmacySupplierCreditAllocations(scope, credit, "TEST-prior"),
  ).rejects.toThrow();
});

it("does not send reversal intent if the supplied original evidence or money bounds are invalid", async () => {
  await expect(
    client.reversePharmacySupplierCreditAllocation(
      scope,
      credit,
      { ...receipt, outstanding_minor: 681 },
      entry,
      reverseInput,
    ),
  ).rejects.toThrow();
  await expect(
    client.reversePharmacySupplierCreditAllocation(scope, credit, receipt, entry, {
      ...reverseInput,
      expected_available_minor: 1000000000000,
    }),
  ).rejects.toThrow();
  await expect(
    client.reversePharmacySupplierCreditAllocation(scope, credit, receipt, entry, {
      ...reverseInput,
      expected_outstanding_minor: 1000000000000,
    }),
  ).rejects.toThrow();
  expect(apiFetch).not.toHaveBeenCalled();
});

it("preserves unavailable outcomes and cancellation without automatic financial retry", async () => {
  const failure = new Error("TEST outcome unconfirmed");
  vi.mocked(apiFetch).mockRejectedValue(failure);
  const controller = new AbortController();
  controller.abort();
  await expect(
    client.allocatePharmacySupplierCredit(scope, credit, allocation, input, controller.signal),
  ).rejects.toBe(failure);
  expect(apiFetch).toHaveBeenCalledTimes(1);
  expect(vi.mocked(apiFetch).mock.calls[0]![1]!.signal?.aborted).toBe(true);
});

it("verifies retained allocation and reversal intent and exact original bill when supplied", async () => {
  vi.mocked(apiFetch).mockResolvedValue({ ...receipt, reason: "TEST changed allocation" });
  await expect(
    client.recoverPharmacySupplierCreditAllocation(scope, credit, allocation, undefined, input),
  ).rejects.toThrow();
  vi.mocked(apiFetch).mockResolvedValue({ ...reversal, payable_id: entry });
  await expect(
    client.readPharmacySupplierCreditAllocationReversal(
      scope,
      credit,
      allocation,
      undefined,
      receipt,
    ),
  ).rejects.toThrow();
  await expect(
    client.recoverPharmacySupplierCreditAllocationReversal(
      scope,
      credit,
      allocation,
      entry,
      undefined,
      { original: receipt, input: reverseInput },
    ),
  ).rejects.toThrow();
  vi.mocked(apiFetch).mockResolvedValue({ ...reversal, reason: "TEST changed reversal" });
  await expect(
    client.recoverPharmacySupplierCreditAllocationReversal(
      scope,
      credit,
      allocation,
      entry,
      undefined,
      { original: receipt, input: reverseInput },
    ),
  ).rejects.toThrow();
  vi.mocked(apiFetch).mockResolvedValue(reversal);
  expect(
    await client.recoverPharmacySupplierCreditAllocationReversal(
      scope,
      credit,
      allocation,
      entry,
      undefined,
      { original: receipt, input: reverseInput },
    ),
  ).toEqual(reversal);
});

it("reads a bounded bill journal across credit documents with current actor/outlet preconditions", async () => {
  const row = { ...receipt, reversal_id: null };
  const page = {
    data: [
      row,
      {
        ...row,
        id: entry,
        credit_id: entry,
        credit_document_reference: "TEST-CN-TWO",
        recorded_by: "TEST-other-finance",
      },
    ],
    page: { has_more: false, next_cursor: null },
  };
  vi.mocked(apiFetch).mockResolvedValue(page);
  expect(await client.listPharmacyPayableCreditAllocations(scope, payable)).toEqual(page);
  expect(apiFetch).toHaveBeenCalledWith(
    `/pharmacy/pharmacies/${scope.pharmacyId}/outlets/${scope.outletId}/accounts/payables/${payable}/supplier-credit-allocations?limit=50`,
    expect.objectContaining({
      cache: "no-store",
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
    }),
  );
});
it.each([
  { payable_id: entry },
  { outlet_id: entry },
  { version: 3 },
  { available_minor: 251 },
  { outstanding_minor: 681 },
])("refuses contradictory or wrong-scope bill journal evidence %j", async (patch) => {
  vi.mocked(apiFetch).mockResolvedValue({
    data: [{ ...receipt, reversal_id: null, ...patch }],
    page: { has_more: false, next_cursor: null },
  });
  await expect(client.listPharmacyPayableCreditAllocations(scope, payable)).rejects.toThrow();
});
it("rejects duplicate journal entries and non-advancing continuation", async () => {
  const row = { ...receipt, reversal_id: null };
  vi.mocked(apiFetch).mockResolvedValue({
    data: [row, row],
    page: { has_more: false, next_cursor: null },
  });
  await expect(client.listPharmacyPayableCreditAllocations(scope, payable)).rejects.toThrow();
  vi.mocked(apiFetch).mockResolvedValue({
    data: [row],
    page: { has_more: true, next_cursor: "TEST-cursor" },
  });
  await expect(
    client.listPharmacyPayableCreditAllocations(scope, payable, "TEST-cursor"),
  ).rejects.toThrow();
});
