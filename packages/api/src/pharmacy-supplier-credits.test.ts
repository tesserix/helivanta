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
const input = {
  return_id: "0199e000-0000-7000-8000-000000000005",
  document_reference: "TEST-CN-1",
  issued_on: "2026-11-16",
  amount_minor: 750,
  currency: "INR" as const,
  reason: "TEST actual partial supplier credit",
};
const receipt = {
  ...input,
  id: credit,
  outlet_id: scope.outletId,
  return_number: 1,
  payee_reference: "TEST-EXT",
  payee_name: "TEST External Supplier",
  original_bill_reference: "TEST-BILL-1",
  recorded_by: scope.subject,
  recorded_at: "2026-11-21T00:00:00Z",
};
const view = {
  ...receipt,
  version: 1,
  state: "active" as const,
  matched_minor: 0,
  allocated_minor: 0,
  available_minor: 750,
};
const root = `/pharmacy/pharmacies/${scope.pharmacyId}/outlets/${scope.outletId}/accounts/supplier-credits`;

it("reads the current credit using the exact actor and no-store request", async () => {
  vi.mocked(apiFetch).mockResolvedValue(view);
  const controller = new AbortController();
  expect(await client.readPharmacySupplierCredit(scope, credit, controller.signal)).toEqual(view);
  expect(apiFetch).toHaveBeenCalledWith(
    `${root}/${credit}`,
    expect.objectContaining({
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
      cache: "no-store",
      signal: expect.any(AbortSignal),
    }),
  );
  const request = vi.mocked(apiFetch).mock.calls[0]![1]!;
  controller.abort();
  expect(request.signal?.aborted).toBe(true);
});

it.each([
  ["another document", { id: input.return_id }],
  ["another outlet", { outlet_id: input.return_id }],
  ["overmatched credit", { matched_minor: 751 }],
  ["overallocated credit", { allocated_minor: 751 }],
  ["unreconciled active pool", { available_minor: 749 }],
  ["unavailable pool on void", { state: "voided", version: 2 }],
  ["used voided credit", { state: "voided", version: 2, available_minor: 0, matched_minor: 1 }],
  [
    "used corrected credit",
    { state: "corrected", version: 3, available_minor: 0, allocated_minor: 1 },
  ],
  ["unexpected private fields", { supplier_contact: "TEST do not accept" }],
])("refuses %s in a current credit response", async (_name, patch) => {
  vi.mocked(apiFetch).mockResolvedValue({ ...view, ...patch });
  await expect(client.readPharmacySupplierCredit(scope, credit)).rejects.toThrow();
});

it("records normalized actual credit and checks the original actor-bound intent", async () => {
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  expect(
    await client.recordPharmacySupplierCredit(scope, credit, {
      ...input,
      document_reference: " test-cn-1 ",
      reason: ` ${input.reason} `,
    }),
  ).toEqual(receipt);
  expect(apiFetch).toHaveBeenCalledWith(
    `${root}/${credit}`,
    expect.objectContaining({
      method: "PUT",
      body: JSON.stringify(input),
      cache: "no-store",
    }),
  );
});

it.each([
  ["another document", { id: input.return_id }],
  ["another outlet", { outlet_id: input.return_id }],
  ["another recorder", { recorded_by: "TEST-other-finance" }],
  ["another return", { return_id: credit }],
  ["changed actual amount", { amount_minor: 751 }],
  ["changed document reference", { document_reference: "TEST-CN-2" }],
  ["changed date", { issued_on: "2026-11-17" }],
  ["changed reason", { reason: "TEST another reason" }],
  ["unexpected correction", { correction_of: credit }],
])("refuses %s in the recorded command receipt", async (_name, patch) => {
  vi.mocked(apiFetch).mockResolvedValue({ ...receipt, ...patch });
  await expect(client.recordPharmacySupplierCredit(scope, credit, input)).rejects.toThrow();
  expect(apiFetch).toHaveBeenCalledTimes(1);
});

it("recovers the original credit document independently of its current position", async () => {
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  expect(await client.recoverPharmacySupplierCredit(scope, credit)).toEqual(receipt);
  expect(apiFetch).toHaveBeenCalledWith(
    `${root}/${credit}/recovery`,
    expect.objectContaining({ cache: "no-store" }),
  );
  vi.mocked(apiFetch).mockResolvedValue({ ...receipt, recorded_by: "TEST-other-finance" });
  await expect(client.recoverPharmacySupplierCredit(scope, credit)).rejects.toThrow();
});

it("pages current credit positions with an opaque cursor and account-specific query key", async () => {
  const page = { data: [view], page: { has_more: true, next_cursor: "TEST-next" } };
  vi.mocked(apiFetch).mockResolvedValue(page);
  expect(await client.listPharmacySupplierCredits(scope, "TEST-prior")).toEqual(page);
  expect(apiFetch).toHaveBeenCalledWith(
    `${root}?limit=50&cursor=TEST-prior`,
    expect.objectContaining({ cache: "no-store" }),
  );
  expect(client.pharmacySupplierCreditKey(scope, credit)).not.toEqual(
    client.pharmacySupplierCreditKey({ ...scope, subject: "TEST-other-finance" }, credit),
  );
  expect(client.pharmacySupplierCreditKey(scope, credit)).not.toEqual(
    client.pharmacySupplierCreditKey({ ...scope, tenantId: input.return_id }, credit),
  );
});

it.each([
  [
    "foreign outlet",
    [{ ...view, outlet_id: input.return_id }],
    { has_more: false, next_cursor: null },
  ],
  ["invalid position", [{ ...view, available_minor: 749 }], { has_more: false, next_cursor: null }],
  [
    "duplicate document",
    [view, { ...view, id: credit.toUpperCase() }],
    { has_more: false, next_cursor: null },
  ],
  [
    "oversized page",
    Array.from({ length: 51 }, () => view),
    { has_more: false, next_cursor: null },
  ],
  ["missing continuation", [view], { has_more: true, next_cursor: null }],
  ["contradictory continuation", [view], { has_more: false, next_cursor: "TEST-next" }],
  ["cursor loop", [view], { has_more: true, next_cursor: "TEST-prior" }],
  ["empty continuing page", [], { has_more: true, next_cursor: "TEST-next" }],
])("refuses %s in a credit collection", async (_name, data, page) => {
  vi.mocked(apiFetch).mockResolvedValue({ data, page });
  await expect(client.listPharmacySupplierCredits(scope, "TEST-prior")).rejects.toThrow();
});

const entry = "0199e000-0000-7000-8000-000000000006";
const voidInput = { expected_version: 1, reason: "TEST wrong amount recorded" };
const voidReceipt = {
  ...voidInput,
  id: entry,
  outlet_id: scope.outletId,
  credit_id: credit,
  version: 2,
  recorded_by: scope.subject,
  recorded_at: receipt.recorded_at,
};

it("voids only the observed version and verifies the actor-bound original intent", async () => {
  vi.mocked(apiFetch).mockResolvedValue(voidReceipt);
  expect(
    await client.voidPharmacySupplierCredit(scope, credit, entry, {
      ...voidInput,
      reason: ` ${voidInput.reason} `,
    }),
  ).toEqual(voidReceipt);
  expect(apiFetch).toHaveBeenCalledWith(
    `${root}/${credit}/voids/${entry}`,
    expect.objectContaining({ method: "PUT", body: JSON.stringify(voidInput), cache: "no-store" }),
  );
});

it.each([
  ["different entry", { id: input.return_id }],
  ["different credit", { credit_id: input.return_id }],
  ["different outlet", { outlet_id: input.return_id }],
  ["different actor", { recorded_by: "TEST-other-finance" }],
  ["different reason", { reason: "TEST other reason" }],
  ["different precondition", { expected_version: 2, version: 3 }],
  ["inconsistent final version", { version: 3 }],
])("refuses %s in a void result", async (_name, patch) => {
  vi.mocked(apiFetch).mockResolvedValue({ ...voidReceipt, ...patch });
  await expect(
    client.voidPharmacySupplierCredit(scope, credit, entry, voidInput),
  ).rejects.toThrow();
});

it("reads another authorised recorder's void history but only recovers its own command", async () => {
  vi.mocked(apiFetch).mockResolvedValue({ ...voidReceipt, recorded_by: "TEST-other-finance" });
  expect((await client.readPharmacySupplierCreditVoid(scope, credit)).recorded_by).toBe(
    "TEST-other-finance",
  );
  expect(apiFetch).toHaveBeenLastCalledWith(
    `${root}/${credit}/void`,
    expect.objectContaining({ cache: "no-store" }),
  );
  await expect(client.recoverPharmacySupplierCreditVoid(scope, credit, entry)).rejects.toThrow();
  vi.mocked(apiFetch).mockResolvedValue(voidReceipt);
  expect(await client.recoverPharmacySupplierCreditVoid(scope, credit, entry)).toEqual(voidReceipt);
  expect(apiFetch).toHaveBeenLastCalledWith(
    `${root}/${credit}/voids/${entry}/recovery`,
    expect.objectContaining({ cache: "no-store" }),
  );
  vi.mocked(apiFetch).mockResolvedValue({ ...voidReceipt, id: input.return_id });
  await expect(client.recoverPharmacySupplierCreditVoid(scope, credit, entry)).rejects.toThrow();
});

it("accepts allocated and matched positions separately and keeps historical recorder identity", async () => {
  const partial = {
    ...view,
    version: 4,
    matched_minor: 500,
    allocated_minor: 250,
    available_minor: 500,
    recorded_by: "TEST-other-finance",
  };
  vi.mocked(apiFetch).mockResolvedValue(partial);
  expect(await client.readPharmacySupplierCredit(scope, credit)).toEqual(partial);
  for (const [state, version] of [
    ["voided", 2],
    ["corrected", 3],
  ] as const) {
    const historical = { ...view, state, version, available_minor: 0 };
    vi.mocked(apiFetch).mockResolvedValue(historical);
    expect(await client.readPharmacySupplierCredit(scope, credit)).toEqual(historical);
  }
});

it("normalizes identifier case and optional null correction without changing actual intent", async () => {
  vi.mocked(apiFetch).mockResolvedValue({
    ...receipt,
    id: credit.toUpperCase(),
    outlet_id: scope.outletId.toUpperCase(),
    return_id: input.return_id.toUpperCase(),
    correction_of: null,
  });
  await expect(client.recordPharmacySupplierCredit(scope, credit, input)).resolves.toMatchObject({
    amount_minor: 750,
  });
});

it("refuses invalid request identities, actors, cursors and monetary intent before transport", async () => {
  await expect(client.readPharmacySupplierCredit(scope, "../other")).rejects.toThrow();
  await expect(
    client.readPharmacySupplierCredit({ ...scope, subject: "" }, credit),
  ).rejects.toThrow();
  await expect(
    client.readPharmacySupplierCredit({ ...scope, tenantId: "invalid" }, credit),
  ).rejects.toThrow();
  await expect(client.listPharmacySupplierCredits(scope, "")).rejects.toThrow();
  await expect(client.listPharmacySupplierCredits(scope, "x".repeat(4097))).rejects.toThrow();
  await expect(
    client.recordPharmacySupplierCredit(scope, credit, { ...input, amount_minor: 0 }),
  ).rejects.toThrow();
  await expect(
    client.recordPharmacySupplierCredit(scope, credit, { ...input, amount_minor: 0.5 }),
  ).rejects.toThrow();
  await expect(
    client.voidPharmacySupplierCredit(scope, credit, entry, { ...voidInput, expected_version: 0 }),
  ).rejects.toThrow();
  await expect(
    client.recoverPharmacySupplierCreditVoid(scope, credit, "../other"),
  ).rejects.toThrow();
  expect(apiFetch).not.toHaveBeenCalled();
});

it("propagates refusal or uncertain transport without inventing a receipt or retrying", async () => {
  const error = new Error("TEST outcome unconfirmed");
  vi.mocked(apiFetch).mockRejectedValue(error);
  await expect(client.recordPharmacySupplierCredit(scope, credit, input)).rejects.toBe(error);
  expect(apiFetch).toHaveBeenCalledTimes(1);
});

it("refuses an original recovery receipt that differs from a retained financial intent", async () => {
  vi.mocked(apiFetch).mockResolvedValue({ ...receipt, reason: "TEST different original intent" });
  await expect(
    client.recoverPharmacySupplierCredit(scope, credit, undefined, input),
  ).rejects.toThrow();
});
