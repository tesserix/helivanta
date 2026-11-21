import { z } from "zod";
import { apiFetch } from "./client";
import { pharmacyRoot, pharmacyKey } from "./pharmacy";
import { pharmacyActorHeaders, type PharmacyActorScope } from "./pharmacy-actor";
import {
  pharmacySupplierReturnDispatchInputSchema,
  pharmacySupplierReturnDispatchReceiptSchema,
  pharmacySupplierReturnSummarySchema,
  pharmacySupplierReturnDraftInputSchema,
  pharmacySupplierReturnCreationReceiptSchema,
  pharmacySupplierReturnViewSchema,
  pharmacySupplierReturnCancelInputSchema,
  pharmacySupplierReturnCancellationReceiptSchema,
} from "./pharmacy-contract";

export type PharmacySupplierReturnInput = z.infer<typeof pharmacySupplierReturnDraftInputSchema>;
export type PharmacySupplierReturnReceipt = z.infer<
  typeof pharmacySupplierReturnCreationReceiptSchema
>;
export type PharmacySupplierReturn = z.infer<typeof pharmacySupplierReturnViewSchema>;
export type PharmacySupplierReturnCancelInput = z.infer<
  typeof pharmacySupplierReturnCancelInputSchema
>;
export type PharmacySupplierReturnCancellation = z.infer<
  typeof pharmacySupplierReturnCancellationReceiptSchema
>;

export type PharmacySupplierReturnDispatchInput = z.infer<
  typeof pharmacySupplierReturnDispatchInputSchema
>;
export type PharmacySupplierReturnDispatchReceipt = z.infer<
  typeof pharmacySupplierReturnDispatchReceiptSchema
>;

const id = z.string().guid();
function sameID(a: string, b: string) {
  return a.toLowerCase() === b.toLowerCase();
}
export function pharmacySupplierReturnKey(scope: PharmacyActorScope, ...parts: unknown[]) {
  return pharmacyKey(scope, "supplier-return-actor", scope.subject, ...parts);
}
function request(scope: PharmacyActorScope, signal?: AbortSignal): RequestInit {
  const timeout = AbortSignal.timeout(15000);
  return {
    headers: pharmacyActorHeaders(scope),
    cache: "no-store",
    signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
  };
}
function root(scope: PharmacyActorScope, returnID: string) {
  return `${pharmacyRoot(scope)}/accounts/supplier-returns/${id.parse(returnID)}`;
}
function mismatch(): never {
  throw new Error(
    "The supplier return receipt could not be verified. Recover the original reference before retrying.",
  );
}
function intent(input: PharmacySupplierReturnInput) {
  return JSON.stringify({
    payable: input.payable_id.toLowerCase(),
    reason: input.reason,
    lines: input.lines
      .map((line) => ({
        receipt: line.receipt_id.toLowerCase(),
        bucket: line.quarantine_location_id.toLowerCase(),
        units: line.quantity_units,
        credit: line.expected_credit_minor,
      }))
      .sort((a, b) => a.receipt.localeCompare(b.receipt) || a.bucket.localeCompare(b.bucket)),
  });
}
function checked(
  scope: PharmacyActorScope,
  returnID: string,
  value: unknown,
): PharmacySupplierReturnReceipt {
  const parsed = pharmacySupplierReturnCreationReceiptSchema.safeParse(value);
  if (!parsed.success) return mismatch();
  const receipt = parsed.data;
  if (!sameID(receipt.id, returnID) || !sameID(receipt.outlet_id, scope.outletId))
    return mismatch();
  const pairs = new Set<string>();
  let total = 0;
  for (const line of receipt.lines) {
    const pair = `${line.receipt_id.toLowerCase()}:${line.quarantine_location_id.toLowerCase()}`;
    if (pairs.has(pair) || line.quantity_units > line.receipt_units) return mismatch();
    pairs.add(pair);
    total += line.expected_credit_minor;
  }
  if (total !== receipt.expected_credit_minor) return mismatch();
  return receipt;
}
export async function preparePharmacySupplierReturn(
  scope: PharmacyActorScope,
  returnID: string,
  input: PharmacySupplierReturnInput,
  signal?: AbortSignal,
) {
  const body = pharmacySupplierReturnDraftInputSchema.parse(input);
  const receipt = checked(
    scope,
    returnID,
    await apiFetch(root(scope, returnID), {
      ...request(scope, signal),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  if (receipt.prepared_by !== scope.subject || intent(receipt) !== intent(body)) return mismatch();
  return receipt;
}
export async function readPharmacySupplierReturn(
  scope: PharmacyActorScope,
  returnID: string,
  signal?: AbortSignal,
): Promise<PharmacySupplierReturn> {
  const parsed = pharmacySupplierReturnViewSchema.safeParse(
    await apiFetch(root(scope, returnID), request(scope, signal)),
  );
  if (!parsed.success) return mismatch();
  const view = parsed.data;
  const { state, version, ...creation } = view;
  checked(scope, returnID, creation);
  if (version !== (state === "draft" ? 0 : 1)) return mismatch();
  return view;
}
function checkedCancellation(
  scope: PharmacyActorScope,
  returnID: string,
  command: string,
  value: unknown,
) {
  const parsed = pharmacySupplierReturnCancellationReceiptSchema.safeParse(value);
  if (!parsed.success) return mismatch();
  const receipt = parsed.data;
  if (
    !sameID(receipt.id, command) ||
    !sameID(receipt.return_id, returnID) ||
    !sameID(receipt.outlet_id, scope.outletId) ||
    receipt.cancelled_by !== scope.subject ||
    receipt.version !== receipt.expected_version + 1
  )
    return mismatch();
  return receipt;
}
export async function cancelPharmacySupplierReturn(
  scope: PharmacyActorScope,
  returnID: string,
  command: string,
  input: PharmacySupplierReturnCancelInput,
  signal?: AbortSignal,
) {
  const body = pharmacySupplierReturnCancelInputSchema.parse(input);
  const receipt = checkedCancellation(
    scope,
    returnID,
    command,
    await apiFetch(`${root(scope, returnID)}/cancellations/${id.parse(command)}`, {
      ...request(scope, signal),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  if (receipt.reason !== body.reason || receipt.expected_version !== body.expected_version)
    return mismatch();
  return receipt;
}
export async function recoverPharmacySupplierReturnCancellation(
  scope: PharmacyActorScope,
  returnID: string,
  command: string,
  signal?: AbortSignal,
) {
  return checkedCancellation(
    scope,
    returnID,
    command,
    await apiFetch(
      `${root(scope, returnID)}/cancellations/${id.parse(command)}`,
      request(scope, signal),
    ),
  );
}

export type PharmacySupplierReturnSummary = z.infer<typeof pharmacySupplierReturnSummarySchema>;
const historyPage = z.object({
  data: z.array(pharmacySupplierReturnSummarySchema).max(25),
  page: z.object({ next_cursor: z.string().min(1).max(4096).nullable(), has_more: z.boolean() }),
});
export async function listPharmacySupplierReturns(
  scope: PharmacyActorScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ limit: "25" });
  if (cursor !== null) query.set("cursor", z.string().min(1).max(4096).parse(cursor));
  const page = historyPage.parse(
    await apiFetch(
      `${pharmacyRoot(scope)}/accounts/supplier-returns?${query}`,
      request(scope, signal),
    ),
  );
  if (
    page.page.has_more !== (page.page.next_cursor !== null) ||
    (page.page.has_more && (page.data.length === 0 || page.page.next_cursor === cursor)) ||
    new Set(page.data.map((row) => row.id.toLowerCase())).size !== page.data.length ||
    page.data.some(
      (row) =>
        !sameID(row.outlet_id, scope.outletId) || row.version !== (row.state === "draft" ? 0 : 1),
    )
  )
    return mismatch();
  return page;
}

export { pharmacySupplierReturnCancelInputSchema, pharmacySupplierReturnDispatchInputSchema };

function checkedDispatch(
  value: unknown,
  scope: PharmacyActorScope,
  returnId: string,
  commandId: string,
): PharmacySupplierReturnDispatchReceipt {
  const row = pharmacySupplierReturnDispatchReceiptSchema.parse(value);
  if (
    !sameID(row.id, commandId) ||
    !sameID(row.return_id, returnId) ||
    !sameID(row.outlet_id, scope.outletId) ||
    row.dispatched_by !== scope.subject ||
    new Set(row.lines.map((line) => line.return_line_id.toLowerCase())).size !== row.lines.length ||
    new Set(row.lines.map((line) => line.movement_id.toLowerCase())).size !== row.lines.length
  ) {
    throw new Error(
      "Could not verify the original supplier return dispatch; recover it before continuing.",
    );
  }
  return row;
}
export function supplierReturnDispatchKey(
  scope: PharmacyActorScope,
  returnId: string,
  commandId: string,
) {
  return [...pharmacySupplierReturnKey(scope, returnId), "dispatch", commandId] as const;
}
function dispatchMicros(value: string) {
  const fraction = value.match(/\.(\d+)(?:Z|[+-]\d{2}:\d{2})$/)?.[1] ?? "";
  if (fraction.length > 6)
    throw new Error("Supplier return handoff time supports at most microsecond precision.");
  return BigInt(Date.parse(value)) * 1000n + BigInt(fraction.padEnd(6, "0").slice(3));
}
export async function dispatchPharmacySupplierReturn(
  scope: PharmacyActorScope,
  returnId: string,
  commandId: string,
  input: PharmacySupplierReturnDispatchInput,
) {
  const normalized = pharmacySupplierReturnDispatchInputSchema.parse(input);
  dispatchMicros(normalized.handed_over_at);
  const value = await apiFetch(`${root(scope, returnId)}/dispatches/${id.parse(commandId)}`, {
    method: "PUT",
    body: JSON.stringify(normalized),
    ...request(scope),
  });
  const row = checkedDispatch(value, scope, returnId, commandId);
  for (const field of [
    "licence_id",
    "document_reference",
    "recipient_name",
    "physical_disposition",
    "reason",
  ] as const) {
    const matches =
      field === "licence_id"
        ? sameID(row[field], normalized[field])
        : row[field] === normalized[field];
    if (!matches)
      throw new Error(
        "Supplier return dispatch did not match the submitted handoff; recover it before continuing.",
      );
  }
  if (dispatchMicros(row.handed_over_at) !== dispatchMicros(normalized.handed_over_at))
    throw new Error(
      "Supplier return dispatch time could not be verified; recover it before continuing.",
    );
  return row;
}
export async function recoverPharmacySupplierReturnDispatch(
  scope: PharmacyActorScope,
  returnId: string,
  commandId: string,
) {
  return checkedDispatch(
    await apiFetch(`${root(scope, returnId)}/dispatches/${id.parse(commandId)}`, request(scope)),
    scope,
    returnId,
    commandId,
  );
}
