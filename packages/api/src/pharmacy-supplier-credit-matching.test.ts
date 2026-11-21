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
const source = "0199e000-0000-7000-8000-000000000005";
const match = "0199e000-0000-7000-8000-000000000006";
const entry = "0199e000-0000-7000-8000-000000000007";
const input = {
  return_id: source,
  expected_version: 1,
  expected_return_matched_minor: 0,
  amount_minor: 500,
  reason: "TEST partial commercial attribution",
};
const receipt = {
  ...input,
  overage_reason: null,
  id: match,
  outlet_id: scope.outletId,
  credit_id: credit,
  version: 2,
  return_number: 1,
  expected_credit_minor: 1500,
  return_matched_minor: 500,
  recorded_by: scope.subject,
  recorded_at: "2026-11-21T00:00:00Z",
};
const root = `/pharmacy/pharmacies/${scope.pharmacyId}/outlets/${scope.outletId}/accounts/supplier-credits/${credit}/matches`;

it("matches explicit partial credit and verifies the normalized original receipt", async () => {
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  expect(
    await client.matchPharmacySupplierCredit(scope, credit, match, {
      ...input,
      reason: ` ${input.reason} `,
    }),
  ).toEqual(receipt);
  expect(apiFetch).toHaveBeenCalledWith(
    `${root}/${match}`,
    expect.objectContaining({
      method: "PUT",
      body: JSON.stringify(input),
      cache: "no-store",
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
    }),
  );
});

it.each([
  ["wrong match", { id: entry }],
  ["wrong credit", { credit_id: entry }],
  ["wrong outlet", { outlet_id: entry }],
  ["another actor", { recorded_by: "TEST-other-finance" }],
  ["different source", { return_id: entry }],
  ["changed amount", { amount_minor: 501, return_matched_minor: 501 }],
  ["changed observation", { expected_return_matched_minor: 1, return_matched_minor: 501 }],
  ["changed version", { expected_version: 2, version: 3 }],
  ["inconsistent version", { version: 3 }],
  ["inconsistent return total", { return_matched_minor: 499 }],
  ["unexplained overage", { expected_credit_minor: 499 }],
  ["changed overage rationale", { overage_reason: "TEST reason not submitted" }],
  ["changed reason", { reason: "TEST reason not submitted" }],
])("refuses %s in a financial matching result", async (_name, patch) => {
  vi.mocked(apiFetch).mockResolvedValue({ ...receipt, ...patch });
  await expect(client.matchPharmacySupplierCredit(scope, credit, match, input)).rejects.toThrow();
  expect(apiFetch).toHaveBeenCalledTimes(1);
});

it("reads retained match history across actors but only recovers the original actor's receipt", async () => {
  const view = { ...receipt, recorded_by: "TEST-other-finance", reversal_id: entry };
  vi.mocked(apiFetch).mockResolvedValue(view);
  expect(await client.readPharmacySupplierCreditMatch(scope, credit, match)).toEqual(view);
  expect(apiFetch).toHaveBeenLastCalledWith(
    `${root}/${match}`,
    expect.objectContaining({ cache: "no-store" }),
  );
  vi.mocked(apiFetch).mockResolvedValue({ ...receipt, recorded_by: "TEST-other-finance" });
  await expect(client.recoverPharmacySupplierCreditMatch(scope, credit, match)).rejects.toThrow();
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  expect(await client.recoverPharmacySupplierCreditMatch(scope, credit, match)).toEqual(receipt);
  expect(apiFetch).toHaveBeenLastCalledWith(
    `${root}/${match}/recovery`,
    expect.objectContaining({ cache: "no-store" }),
  );
});

it("lists bounded original match history with current reversal references", async () => {
  const page = {
    data: [{ ...receipt, reversal_id: entry }],
    page: { has_more: true, next_cursor: "TEST-next" },
  };
  vi.mocked(apiFetch).mockResolvedValue(page);
  expect(await client.listPharmacySupplierCreditMatches(scope, credit, "TEST-prior")).toEqual(page);
  expect(apiFetch).toHaveBeenCalledWith(
    `${root}?limit=50&cursor=TEST-prior`,
    expect.objectContaining({ cache: "no-store" }),
  );
});

const reverseInput = {
  expected_version: 2,
  expected_return_matched_minor: 500,
  reason: "TEST correct commercial attribution",
};
const reversal = {
  ...reverseInput,
  id: entry,
  outlet_id: scope.outletId,
  credit_id: credit,
  match_id: match,
  return_id: source,
  return_number: 1,
  amount_minor: 500,
  return_matched_minor: 0,
  version: 3,
  recorded_by: scope.subject,
  recorded_at: receipt.recorded_at,
};
it("reverses only the original match amount at the explicit current observations", async () => {
  vi.mocked(apiFetch).mockResolvedValue(reversal);
  expect(
    await client.reversePharmacySupplierCreditMatch(scope, credit, receipt, entry, reverseInput),
  ).toEqual(reversal);
  expect(apiFetch).toHaveBeenCalledWith(
    `${root}/${match}/reversals/${entry}`,
    expect.objectContaining({
      method: "PUT",
      body: JSON.stringify(reverseInput),
      cache: "no-store",
    }),
  );
});

it.each([
  ["wrong command", { id: source }],
  ["wrong match", { match_id: source }],
  ["wrong credit", { credit_id: source }],
  ["wrong outlet", { outlet_id: source }],
  ["another actor", { recorded_by: "TEST-other-finance" }],
  ["changed original source", { return_id: entry }],
  ["changed return reference", { return_number: 2 }],
  ["changed original amount", { amount_minor: 499, return_matched_minor: 1 }],
  ["inconsistent total", { return_matched_minor: 1 }],
  ["changed observation", { expected_return_matched_minor: 501, return_matched_minor: 1 }],
  ["changed version", { expected_version: 3, version: 4 }],
  ["changed reason", { reason: "TEST another reason" }],
])("refuses %s in a reversal command result", async (_name, patch) => {
  vi.mocked(apiFetch).mockResolvedValue({ ...reversal, ...patch });
  await expect(
    client.reversePharmacySupplierCreditMatch(scope, credit, receipt, entry, reverseInput),
  ).rejects.toThrow();
});

it("reads reversal history across actors while recovery binds original actor and command", async () => {
  vi.mocked(apiFetch).mockResolvedValue({ ...reversal, recorded_by: "TEST-other-finance" });
  expect(
    (await client.readPharmacySupplierCreditMatchReversal(scope, credit, match)).recorded_by,
  ).toBe("TEST-other-finance");
  expect(apiFetch).toHaveBeenLastCalledWith(
    `${root}/${match}/reversal`,
    expect.objectContaining({ cache: "no-store" }),
  );
  await expect(
    client.recoverPharmacySupplierCreditMatchReversal(scope, credit, match, entry),
  ).rejects.toThrow();
  vi.mocked(apiFetch).mockResolvedValue(reversal);
  expect(
    await client.recoverPharmacySupplierCreditMatchReversal(scope, credit, match, entry),
  ).toEqual(reversal);
  expect(apiFetch).toHaveBeenLastCalledWith(
    `${root}/${match}/reversals/${entry}/recovery`,
    expect.objectContaining({ cache: "no-store" }),
  );
  vi.mocked(apiFetch).mockResolvedValue({ ...reversal, id: source });
  await expect(
    client.recoverPharmacySupplierCreditMatchReversal(scope, credit, match, entry),
  ).rejects.toThrow();
});

it.each([
  ["wrong credit", [{ ...receipt, credit_id: source }], { has_more: false, next_cursor: null }],
  ["wrong outlet", [{ ...receipt, outlet_id: source }], { has_more: false, next_cursor: null }],
  [
    "wrong arithmetic",
    [{ ...receipt, return_matched_minor: 499 }],
    { has_more: false, next_cursor: null },
  ],
  [
    "duplicate record",
    [receipt, { ...receipt, id: match.toUpperCase() }],
    { has_more: false, next_cursor: null },
  ],
  [
    "oversized page",
    Array.from({ length: 51 }, () => receipt),
    { has_more: false, next_cursor: null },
  ],
  ["cursor loop", [receipt], { has_more: true, next_cursor: "TEST-prior" }],
  ["missing cursor", [receipt], { has_more: true, next_cursor: null }],
  ["contradictory cursor", [receipt], { has_more: false, next_cursor: "TEST-next" }],
  ["empty continuation", [], { has_more: true, next_cursor: "TEST-next" }],
])("refuses %s in matching history", async (_name, data, page) => {
  vi.mocked(apiFetch).mockResolvedValue({ data, page });
  await expect(
    client.listPharmacySupplierCreditMatches(scope, credit, "TEST-prior"),
  ).rejects.toThrow();
});

it("allows explicitly explained commercial excess without changing actual attribution", async () => {
  const overage = "TEST supplier accepted additional credit";
  vi.mocked(apiFetch).mockResolvedValue({
    ...receipt,
    expected_credit_minor: 499,
    overage_reason: overage,
  });
  expect(
    (
      await client.matchPharmacySupplierCredit(scope, credit, match, {
        ...input,
        overage_reason: ` ${overage} `,
      })
    ).amount_minor,
  ).toBe(500);
});

it("refuses invalid observations, source snapshots or identifiers before transport", async () => {
  await expect(
    client.matchPharmacySupplierCredit(scope, credit, match, {
      ...input,
      expected_return_matched_minor: 1000000000000,
    }),
  ).rejects.toThrow();
  await expect(
    client.matchPharmacySupplierCredit(scope, credit, match, { ...input, amount_minor: 0 }),
  ).rejects.toThrow();
  await expect(
    client.matchPharmacySupplierCredit(scope, credit, match, { ...input, expected_version: 0 }),
  ).rejects.toThrow();
  await expect(
    client.matchPharmacySupplierCredit(scope, credit, match, { ...input, overage_reason: " " }),
  ).rejects.toThrow();
  await expect(
    client.reversePharmacySupplierCreditMatch(scope, credit, receipt, entry, {
      ...reverseInput,
      expected_return_matched_minor: 499,
    }),
  ).rejects.toThrow();
  await expect(
    client.reversePharmacySupplierCreditMatch(
      scope,
      credit,
      { ...receipt, outlet_id: source },
      entry,
      reverseInput,
    ),
  ).rejects.toThrow();
  await expect(client.readPharmacySupplierCreditMatch(scope, credit, "../other")).rejects.toThrow();
  await expect(
    client.recoverPharmacySupplierCreditMatchReversal(scope, credit, match, "../other"),
  ).rejects.toThrow();
  await expect(client.listPharmacySupplierCreditMatches(scope, credit, "")).rejects.toThrow();
  expect(apiFetch).not.toHaveBeenCalled();
});

it("keeps retained matching reads cancellable and private", async () => {
  vi.mocked(apiFetch).mockResolvedValue({ ...receipt, reversal_id: null });
  const controller = new AbortController();
  await client.readPharmacySupplierCreditMatch(scope, credit, match, controller.signal);
  const request = vi.mocked(apiFetch).mock.calls[0]![1]!;
  expect(request.cache).toBe("no-store");
  controller.abort();
  expect(request.signal?.aborted).toBe(true);
});

it("preserves an uncertain command outcome without an automatic second intent", async () => {
  const error = new Error("TEST outcome unconfirmed");
  vi.mocked(apiFetch).mockRejectedValue(error);
  await expect(client.matchPharmacySupplierCredit(scope, credit, match, input)).rejects.toBe(error);
  expect(apiFetch).toHaveBeenCalledTimes(1);
});
