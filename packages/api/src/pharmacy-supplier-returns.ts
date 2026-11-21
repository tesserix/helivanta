import { z } from "zod";
import { apiFetch } from "./client";
import { pharmacyRoot, pharmacyKey } from "./pharmacy";
import { pharmacyActorHeaders, type PharmacyActorScope } from "./pharmacy-actor";
import {
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
