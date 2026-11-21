import { z } from "zod";
import { apiFetch } from "./client";
import { pharmacyRoot, pharmacyKey } from "./pharmacy";
import { pharmacyActorHeaders, type PharmacyActorScope } from "./pharmacy-actor";
import { pharmacyStockHoldInputSchema, pharmacyStockHoldReceiptSchema } from "./pharmacy-contract";

export type PharmacyStockHoldInput = z.infer<typeof pharmacyStockHoldInputSchema>;
export type PharmacyStockHoldReceipt = z.infer<typeof pharmacyStockHoldReceiptSchema>;
const id = z.string().guid();
export function pharmacyStockHoldKey(scope: PharmacyActorScope, ...parts: unknown[]) {
  return pharmacyKey(scope, "stock-hold-actor", scope.subject, ...parts);
}
function root(scope: PharmacyActorScope, hold: string) {
  return `${pharmacyRoot(scope)}/stock/holds/${id.parse(hold)}`;
}
function request(scope: PharmacyActorScope, signal?: AbortSignal): RequestInit {
  const timeout = AbortSignal.timeout(15000);
  return {
    headers: pharmacyActorHeaders(scope),
    cache: "no-store",
    signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
  };
}
function mismatch(): never {
  throw new Error(
    "The physical hold receipt could not be verified. Recover the original reference before retrying.",
  );
}
function checked(scope: PharmacyActorScope, hold: string, value: unknown) {
  const parsed = pharmacyStockHoldReceiptSchema.safeParse(value);
  if (!parsed.success) return mismatch();
  const receipt = parsed.data;
  if (
    receipt.id.toLowerCase() !== hold.toLowerCase() ||
    receipt.outlet_id.toLowerCase() !== scope.outletId.toLowerCase() ||
    receipt.held_by !== scope.subject ||
    receipt.out_movement_id.toLowerCase() === receipt.in_movement_id.toLowerCase() ||
    receipt.from_location_id.toLowerCase() === receipt.quarantine_location_id.toLowerCase()
  )
    return mismatch();
  return receipt;
}
function intent(input: PharmacyStockHoldInput) {
  return JSON.stringify({
    batch: input.batch_id.toLowerCase(),
    from: input.from_location_id.toLowerCase(),
    to: input.quarantine_location_id.toLowerCase(),
    units: input.quantity_units,
    reason: input.reason,
    disposition: input.physical_disposition,
  });
}
export async function recordPharmacyStockHold(
  scope: PharmacyActorScope,
  hold: string,
  input: PharmacyStockHoldInput,
  signal?: AbortSignal,
) {
  const body = pharmacyStockHoldInputSchema.parse(input);
  if (body.from_location_id.toLowerCase() === body.quarantine_location_id.toLowerCase())
    throw new Error("Choose different source and quarantine locations.");
  const receipt = checked(
    scope,
    hold,
    await apiFetch(root(scope, hold), {
      ...request(scope, signal),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  if (intent(receipt) !== intent(body)) return mismatch();
  return receipt;
}
export async function readPharmacyStockHold(
  scope: PharmacyActorScope,
  hold: string,
  signal?: AbortSignal,
) {
  return checked(scope, hold, await apiFetch(root(scope, hold), request(scope, signal)));
}
