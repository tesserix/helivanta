import { z } from "zod";
import { apiFetch } from "./client";
import { pharmacyKey, pharmacyRoot } from "./pharmacy";
import { pharmacyActorHeaders, type PharmacyActorScope } from "./pharmacy-actor";
import {
  pharmacyPurchaseTaxInputSchema,
  pharmacyPurchaseTaxReceiptSchema,
  pharmacyPurchaseTaxViewSchema,
} from "./pharmacy-contract";

export { pharmacyPurchaseTaxInputSchema } from "./pharmacy-contract";
export type PharmacyPurchaseTaxInput = z.infer<typeof pharmacyPurchaseTaxInputSchema>;
export type PharmacyPurchaseTaxReceipt = z.infer<typeof pharmacyPurchaseTaxReceiptSchema>;
const id = z.string().guid();
const same = (a: string | null, b: string | null) => a?.toLowerCase() === b?.toLowerCase();
function root(scope: PharmacyActorScope, bill: string) {
  return `${pharmacyRoot(scope)}/accounts/payables/${id.parse(bill)}/purchase-tax`;
}
export function pharmacyPurchaseTaxKey(scope: PharmacyActorScope, bill: string) {
  return pharmacyKey(scope, "purchase-tax", scope.subject, bill);
}
function matches(receipt: PharmacyPurchaseTaxReceipt, scope: PharmacyActorScope, bill: string) {
  return (
    same(receipt.outlet_id, scope.outletId) &&
    same(receipt.payable_id, bill) &&
    receipt.lines.every((line, index) => line.position === index + 1)
  );
}
export async function readPharmacyPurchaseTax(
  scope: PharmacyActorScope,
  bill: string,
  signal: AbortSignal,
) {
  const parsed = pharmacyPurchaseTaxViewSchema.safeParse(
    await apiFetch(root(scope, bill), {
      headers: pharmacyActorHeaders(scope),
      cache: "no-store",
      signal,
    }),
  );
  signal.throwIfAborted();
  if (!parsed.success || (parsed.data.evidence && !matches(parsed.data.evidence, scope, bill)))
    throw new Error("The purchase tax evidence could not be verified. Reload the bill.");
  return parsed.data.evidence;
}
export async function savePharmacyPurchaseTax(
  scope: PharmacyActorScope,
  bill: string,
  command: string,
  input: PharmacyPurchaseTaxInput,
  signal: AbortSignal,
) {
  const body = pharmacyPurchaseTaxInputSchema.parse(input);
  const parsed = pharmacyPurchaseTaxReceiptSchema.safeParse(
    await apiFetch(`${root(scope, bill)}/${id.parse(command)}`, {
      method: "PUT",
      body: JSON.stringify(body),
      headers: pharmacyActorHeaders(scope),
      cache: "no-store",
      signal,
    }),
  );
  signal.throwIfAborted();
  if (
    !parsed.success ||
    !matches(parsed.data, scope, bill) ||
    !same(parsed.data.id, command) ||
    !same(parsed.data.replaces, body.replaces) ||
    parsed.data.recorded_by !== scope.subject ||
    parsed.data.supplier_gstin !== body.supplier_gstin ||
    parsed.data.supplier_state !== body.supplier_state ||
    parsed.data.place_of_supply !== body.place_of_supply ||
    parsed.data.reason !== body.reason ||
    JSON.stringify(parsed.data.lines) !== JSON.stringify(body.lines)
  )
    throw new Error(
      "The purchase tax receipt could not be verified. Retry the same evidence before starting another entry.",
    );
  return parsed.data;
}
