import { afterEach, expect, it, vi } from "vitest";
import * as client from "./pharmacy-supplier-returns";
import { apiFetch } from "./client";
vi.mock("./client", () => ({ apiFetch: vi.fn() }));
afterEach(() => vi.resetAllMocks());
const scope = {
  tenantId: "0199e000-0000-7000-8000-000000000001",
  pharmacyId: "0199e000-0000-7000-8000-000000000002",
  outletId: "0199e000-0000-7000-8000-000000000003",
  subject: "TEST-owner",
};
const id = "0199e000-0000-7000-8000-000000000004";
const input = {
  payable_id: "0199e000-0000-7000-8000-000000000005",
  reason: "TEST review",
  lines: [
    {
      receipt_id: "0199e000-0000-7000-8000-000000000006",
      quarantine_location_id: "0199e000-0000-7000-8000-000000000007",
      quantity_units: 4,
      expected_credit_minor: 1500,
    },
  ],
};
const receipt = {
  id,
  outlet_id: scope.outletId,
  number: 1,
  payable_id: input.payable_id,
  payee_reference: "TEST-EXT",
  payee_name: "TEST External Supplier",
  document_reference: "TEST-BILL",
  currency: "INR",
  expected_credit_minor: 1500,
  reason: input.reason,
  prepared_by: scope.subject,
  prepared_at: "2026-11-21T00:00:00Z",
  lines: [
    {
      ...input.lines[0],
      id: "0199e000-0000-7000-8000-000000000008",
      batch_id: "0199e000-0000-7000-8000-000000000009",
      product_id: "0199e000-0000-7000-8000-000000000010",
      product_version: 1,
      product_name: "TEST Brand",
      generic_name: "TEST generic",
      unit_code: "capsule",
      lot_number: "TEST-LOT",
      expires_on: "2030-10-31",
      receipt_units: 80,
      source_reference: "TEST-delivery",
      source_created_at: "2026-11-20T00:00:00Z",
    },
  ],
};
it("binds draft creation to account, outlet, original receipts and explicit expectation", async () => {
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  expect(await client.preparePharmacySupplierReturn(scope, id, input)).toEqual(receipt);
  expect(apiFetch).toHaveBeenCalledTimes(1);
  expect(apiFetch).toHaveBeenCalledWith(
    expect.stringContaining(`/accounts/supplier-returns/${id}`),
    expect.objectContaining({
      method: "PUT",
      cache: "no-store",
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
      signal: expect.any(AbortSignal),
    }),
  );
});
it.each([
  { ...receipt, outlet_id: input.payable_id },
  { ...receipt, prepared_by: "TEST-other" },
  { ...receipt, expected_credit_minor: 1501 },
  { ...receipt, lines: [{ ...receipt.lines[0], quantity_units: 5 }] },
])("does not acknowledge an inconsistent creation receipt", async (value) => {
  vi.mocked(apiFetch).mockResolvedValue(value);
  await expect(client.preparePharmacySupplierReturn(scope, id, input)).rejects.toThrow(
    /recover|match|verified/i,
  );
  expect(apiFetch).toHaveBeenCalledTimes(1);
});
it("allows authorised business reads of another preparer's draft", async () => {
  vi.mocked(apiFetch).mockResolvedValue({
    ...receipt,
    prepared_by: "TEST-other",
    state: "draft",
    version: 0,
  });
  expect((await client.readPharmacySupplierReturn(scope, id)).prepared_by).toBe("TEST-other");
});
it("rejects an impossible state/version projection", async () => {
  vi.mocked(apiFetch).mockResolvedValue({ ...receipt, state: "cancelled", version: 0 });
  await expect(client.readPharmacySupplierReturn(scope, id)).rejects.toThrow();
});
it("does not send invalid commands or silently retry failures", async () => {
  await expect(
    client.preparePharmacySupplierReturn(scope, id, { ...input, lines: [] }),
  ).rejects.toThrow();
  expect(apiFetch).not.toHaveBeenCalled();
  const unavailable = new Error("TEST authority unavailable");
  vi.mocked(apiFetch).mockRejectedValue(unavailable);
  await expect(client.preparePharmacySupplierReturn(scope, id, input)).rejects.toBe(unavailable);
  expect(apiFetch).toHaveBeenCalledTimes(1);
});

it("binds cancellation and recovery to the original actor and command", async () => {
  const command = "0199e000-0000-7000-8000-000000000020";
  const cancellation = {
    id: command,
    return_id: id,
    outlet_id: scope.outletId,
    expected_version: 0,
    version: 1,
    state: "cancelled",
    reason: "TEST corrected supplier request",
    cancelled_by: scope.subject,
    cancelled_at: "2026-11-21T00:00:00Z",
  };
  vi.mocked(apiFetch).mockResolvedValue(cancellation);
  expect(
    await client.cancelPharmacySupplierReturn(scope, id, command, {
      expected_version: 0,
      reason: cancellation.reason,
    }),
  ).toEqual(cancellation);
  expect(await client.recoverPharmacySupplierReturnCancellation(scope, id, command)).toEqual(
    cancellation,
  );
  vi.mocked(apiFetch).mockResolvedValue({ ...cancellation, cancelled_by: "TEST-other" });
  await expect(
    client.recoverPharmacySupplierReturnCancellation(scope, id, command),
  ).rejects.toThrow(/recover|verified/i);
  vi.mocked(apiFetch).mockResolvedValue({ ...cancellation, return_id: input.payable_id });
  await expect(
    client.recoverPharmacySupplierReturnCancellation(scope, id, command),
  ).rejects.toThrow(/recover|verified/i);
});

it("lists bounded supplier return history with account fences and continuation", async () => {
  const summary = {
    id,
    outlet_id: scope.outletId,
    number: 1,
    payee_name: receipt.payee_name,
    document_reference: receipt.document_reference,
    currency: "INR",
    expected_credit_minor: 1500,
    line_count: 1,
    prepared_at: receipt.prepared_at,
    state: "draft",
    version: 0,
  };
  const result = { data: [summary], page: { next_cursor: "TEST-next", has_more: true } };
  vi.mocked(apiFetch).mockResolvedValue(result);
  expect(await Reflect.get(client, "listPharmacySupplierReturns")(scope, "TEST-current")).toEqual(
    result,
  );
  expect(apiFetch).toHaveBeenCalledWith(
    expect.stringMatching(/supplier-returns\?limit=25&cursor=TEST-current$/),
    expect.objectContaining({
      cache: "no-store",
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
    }),
  );
});

it.each(["foreign", "duplicate", "state", "continuation", "loop"])(
  "rejects inconsistent history %s",
  async (kind) => {
    const summary = {
      id,
      outlet_id: scope.outletId,
      number: 1,
      payee_name: receipt.payee_name,
      document_reference: receipt.document_reference,
      currency: "INR",
      expected_credit_minor: 1500,
      line_count: 1,
      prepared_at: receipt.prepared_at,
      state: "draft",
      version: 0,
    };
    const value = {
      data: [summary],
      page: { has_more: false, next_cursor: null as string | null },
    };
    if (kind === "foreign") summary.outlet_id = input.payable_id;
    if (kind === "duplicate") value.data.push(summary);
    if (kind === "state") summary.version = 1;
    if (kind === "continuation") value.page.has_more = true;
    if (kind === "loop") value.page = { has_more: true, next_cursor: "TEST-current" };
    vi.mocked(apiFetch).mockResolvedValue(value);
    await expect(client.listPharmacySupplierReturns(scope, "TEST-current")).rejects.toThrow();
    expect(apiFetch).toHaveBeenCalledTimes(1);
  },
);

it("binds physical dispatch/recovery to exact intent, actor and movement proofs", async () => {
  const command = "0199e000-0000-7000-8000-000000000020";
  const input = {
    expected_version: 0,
    licence_id: command,
    document_reference: "TEST-OUT-1",
    recipient_name: "TEST Supplier",
    handed_over_at: "2026-11-21T00:00:00Z",
    physical_disposition: "handed_to_supplier" as const,
    reason: "TEST physical handoff",
  };
  const row = {
    ...input,
    id: command,
    return_id: id,
    outlet_id: scope.outletId,
    number: 1,
    version: 1,
    state: "dispatched",
    licence_review_id: command,
    dispatched_by: scope.subject,
    recorded_at: input.handed_over_at,
    lines: [{ return_line_id: command, movement_id: id }],
  };
  vi.mocked(apiFetch).mockResolvedValue(row);
  expect(await client.dispatchPharmacySupplierReturn(scope, id, command, input)).toEqual(row);
  expect(await client.recoverPharmacySupplierReturnDispatch(scope, id, command)).toEqual(row);
  for (const changed of [
    { dispatched_by: "TEST-other" },
    { return_id: command },
    { lines: [row.lines[0], row.lines[0]] },
    { state: "draft" },
    { version: 0 },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue({ ...row, ...changed });
    await expect(
      client.recoverPharmacySupplierReturnDispatch(scope, id, command),
    ).rejects.toThrow();
  }
  vi.mocked(apiFetch).mockResolvedValue({ ...row, reason: "TEST changed" });
  await expect(client.dispatchPharmacySupplierReturn(scope, id, command, input)).rejects.toThrow(
    /match/,
  );
  expect(client.supplierReturnDispatchKey(scope, id, command)).not.toEqual(
    client.supplierReturnDispatchKey({ ...scope, subject: "TEST-other" }, id, command),
  );
});
it("does not retry a physical dispatch after response loss", async () => {
  const error = new Error("TEST response lost");
  vi.mocked(apiFetch).mockRejectedValue(error);
  await expect(
    client.dispatchPharmacySupplierReturn(scope, id, id, {
      expected_version: 0,
      licence_id: id,
      document_reference: "TEST-OUT",
      recipient_name: "TEST recipient",
      handed_over_at: "2026-11-21T00:00:00Z",
      physical_disposition: "handed_to_supplier",
      reason: "TEST actual handoff",
    }),
  ).rejects.toBe(error);
  expect(apiFetch).toHaveBeenCalledTimes(1);
});

it("compares canonical licence IDs and preserves offset microsecond handoff intent", async () => {
  const input = {
    expected_version: 0,
    licence_id: id.toUpperCase(),
    document_reference: "TEST-OUT",
    recipient_name: "TEST Supplier",
    handed_over_at: "2026-11-21T05:30:00.123456+05:30",
    physical_disposition: "handed_to_supplier" as const,
    reason: "TEST handoff",
  };
  const row = {
    ...input,
    licence_id: id,
    handed_over_at: "2026-11-21T00:00:00.123456Z",
    id,
    return_id: id,
    outlet_id: scope.outletId,
    number: 1,
    version: 1,
    state: "dispatched",
    licence_review_id: id,
    dispatched_by: scope.subject,
    recorded_at: "2026-11-21T01:00:00Z",
    lines: [{ return_line_id: id, movement_id: id }],
  };
  vi.mocked(apiFetch).mockResolvedValue(row);
  expect(await client.dispatchPharmacySupplierReturn(scope, id, id, input)).toEqual(row);
  vi.mocked(apiFetch).mockResolvedValue({ ...row, handed_over_at: "2026-11-21T00:00:00.123457Z" });
  await expect(client.dispatchPharmacySupplierReturn(scope, id, id, input)).rejects.toThrow(/time/);
});
it("refuses unsupported sub-microsecond precision before sending a dispatch", async () => {
  await expect(
    client.dispatchPharmacySupplierReturn(scope, id, id, {
      expected_version: 0,
      licence_id: id,
      document_reference: "TEST-OUT",
      recipient_name: "TEST Supplier",
      handed_over_at: "2026-11-21T00:00:00.1234561Z",
      physical_disposition: "handed_to_supplier",
      reason: "TEST handoff",
    }),
  ).rejects.toThrow(/precision/);
  expect(apiFetch).not.toHaveBeenCalled();
});

it("recovers a receipt with the licence owner's legacy UUIDv4 review identifier", async () => {
  const row = {
    expected_version: 0,
    licence_id: id,
    document_reference: "TEST-OUT",
    recipient_name: "TEST Supplier",
    handed_over_at: "2026-11-21T08:23:02.18Z",
    physical_disposition: "handed_to_supplier",
    reason: "TEST handoff",
    id,
    return_id: id,
    outlet_id: scope.outletId,
    number: 1,
    version: 1,
    state: "dispatched",
    licence_review_id: "00000000-0000-4000-8000-000000000001",
    dispatched_by: scope.subject,
    recorded_at: "2026-11-21T08:23:02.519032Z",
    lines: [{ return_line_id: id, movement_id: id }],
  };
  vi.mocked(apiFetch).mockResolvedValue(row);
  expect(await client.recoverPharmacySupplierReturnDispatch(scope, id, id)).toEqual(row);
  vi.mocked(apiFetch).mockResolvedValue({
    ...row,
    licence_review_id: "00000000-0000-0000-0000-000000000000",
  });
  await expect(client.recoverPharmacySupplierReturnDispatch(scope, id, id)).rejects.toThrow();
});

it("verifies bounded original bill sources and current returned quantities", async () => {
  const line = {
    receipt_id: id,
    batch_id: id,
    product_id: id,
    product_version: 1,
    product_name: "TEST Medicine",
    generic_name: "TEST Generic",
    unit_code: "tablet",
    lot_number: "TEST LOT",
    expires_on: "2028-01-01",
    receipt_units: 80,
    source_reference: "TEST original delivery",
    source_created_at: "2026-11-21T00:00:00Z",
    dispatched_units: 48,
    origin: "external",
  };
  const row = {
    outlet_id: scope.outletId,
    payable_id: id,
    payee_reference: "TEST-EXT",
    payee_name: "TEST Supplier",
    document_reference: "TEST-BILL",
    currency: "INR",
    stock_as_of: "2026-11-21T00:00:00Z",
    lines: [line],
  };
  vi.mocked(apiFetch).mockResolvedValue(row);
  expect(await client.readPharmacySupplierReturnPreparation(scope, id.toUpperCase())).toEqual(row);
  expect(apiFetch).toHaveBeenCalledWith(
    expect.stringContaining(`/supplier-returns/preparation/${id.toUpperCase()}`),
    expect.objectContaining({
      cache: "no-store",
      headers: expect.objectContaining({
        "X-Helivanta-Expected-Subject": scope.subject,
        "X-Helivanta-Expected-Tenant": scope.tenantId,
      }),
    }),
  );
  for (const changed of [
    { outlet_id: id },
    { payable_id: scope.outletId },
    { lines: [line, line] },
    { lines: [{ ...line, dispatched_units: 81 }] },
    { lines: [{ ...line, source_reference: "supplier-receipt:TEST" }] },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue({ ...row, ...changed });
    await expect(client.readPharmacySupplierReturnPreparation(scope, id)).rejects.toThrow();
  }
  vi.mocked(apiFetch).mockResolvedValue({ ...row, lines: [{ ...line, origin: "platform" }] });
  expect((await client.readPharmacySupplierReturnPreparation(scope, id)).lines[0]?.origin).toBe(
    "platform",
  );
});

it("recovers immutable preparation only for the original actor", async () => {
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  expect(await client.recoverPharmacySupplierReturnPreparation(scope, id)).toEqual(receipt);
  expect(apiFetch).toHaveBeenCalledWith(
    expect.stringContaining(`/supplier-returns/${id}/preparation`),
    expect.objectContaining({ cache: "no-store" }),
  );
  vi.mocked(apiFetch).mockResolvedValue({ ...receipt, prepared_by: "TEST-other-actor" });
  await expect(client.recoverPharmacySupplierReturnPreparation(scope, id)).rejects.toThrow(
    /verified/,
  );
});

it("loads only scoped audited original inventory bill choices with bounded pagination", async () => {
  const row = {
    id,
    outlet_id: scope.outletId,
    payee_name: "TEST Supplier",
    document_reference: "TEST-BILL",
    issued_on: "2026-11-01",
    currency: "INR",
  };
  vi.mocked(apiFetch).mockResolvedValue({
    data: [row],
    page: { has_more: false, next_cursor: null },
  });
  expect((await client.listPharmacySupplierReturnOriginalBills(scope)).data).toEqual([row]);
  expect(apiFetch).toHaveBeenCalledWith(
    expect.stringContaining("/supplier-returns/original-bills?limit=50"),
    expect.objectContaining({ cache: "no-store" }),
  );
  for (const data of [[{ ...row, outlet_id: id }], [row, row]]) {
    vi.mocked(apiFetch).mockResolvedValue({ data, page: { has_more: false, next_cursor: null } });
    await expect(client.listPharmacySupplierReturnOriginalBills(scope)).rejects.toThrow();
  }
});
