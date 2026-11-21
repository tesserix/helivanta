import { apiFetch } from "./client";
import { pharmacyKey, pharmacyRoot } from "./pharmacy";
import { pharmacyActorHeaders, type PharmacyActorScope } from "./pharmacy-actor";
import {
  pharmacySupplierCreditDocumentInputSchema,
  pharmacySupplierCreditDocumentReceiptSchema,
  pharmacySupplierCreditDocumentViewSchema,
  pharmacySupplierCreditVoidInputSchema,
  pharmacySupplierCreditVoidReceiptSchema,
} from "./pharmacy-contract";

export type PharmacySupplierCreditInput = z.infer<typeof pharmacySupplierCreditDocumentInputSchema>;
export type PharmacySupplierCreditReceipt = z.infer<
  typeof pharmacySupplierCreditDocumentReceiptSchema
>;
export type PharmacySupplierCredit = z.infer<typeof pharmacySupplierCreditDocumentViewSchema>;
export type PharmacySupplierCreditVoidInput = z.infer<typeof pharmacySupplierCreditVoidInputSchema>;
export type PharmacySupplierCreditVoidReceipt = z.infer<
  typeof pharmacySupplierCreditVoidReceiptSchema
>;

export function pharmacySupplierCreditKey(scope: PharmacyActorScope, ...parts: unknown[]) {
  return pharmacyKey(scope, "supplier-credit-actor", scope.subject, ...parts);
}

const identifier = pharmacySupplierCreditDocumentInputSchema.shape.return_id;
function sameID(a: string, b: string) {
  return a.toLowerCase() === b.toLowerCase();
}
function mismatch(): never {
  throw new Error(
    "The supplier credit result could not be verified. Recover the original reference before retrying.",
  );
}
function root(scope: PharmacyActorScope, credit: string) {
  return `${pharmacyRoot(scope)}/accounts/supplier-credits/${identifier.parse(credit)}`;
}
function request(scope: PharmacyActorScope, signal?: AbortSignal): RequestInit {
  const timeout = AbortSignal.timeout(15000);
  return {
    headers: pharmacyActorHeaders(scope),
    cache: "no-store",
    signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
  };
}
function checkedView(scope: PharmacyActorScope, credit: string, value: unknown) {
  const row = pharmacySupplierCreditDocumentViewSchema.parse(value);
  if (
    !sameID(row.id, credit) ||
    !sameID(row.outlet_id, scope.outletId) ||
    row.matched_minor > row.amount_minor ||
    row.allocated_minor > row.amount_minor
  )
    return mismatch();
  const available = row.state === "active" ? row.amount_minor - row.allocated_minor : 0;
  if (
    row.available_minor !== available ||
    (row.state !== "active" && (row.matched_minor !== 0 || row.allocated_minor !== 0))
  )
    return mismatch();
  return row;
}
function checkedReceipt(scope: PharmacyActorScope, credit: string, value: unknown) {
  const row = pharmacySupplierCreditDocumentReceiptSchema.parse(value);
  if (
    !sameID(row.id, credit) ||
    !sameID(row.outlet_id, scope.outletId) ||
    row.recorded_by !== scope.subject
  )
    return mismatch();
  return row;
}
function documentIntent(value: PharmacySupplierCreditInput) {
  const input = pharmacySupplierCreditDocumentInputSchema.strip().parse(value);
  return JSON.stringify({
    ...input,
    return_id: input.return_id.toLowerCase(),
    correction_of: input.correction_of?.toLowerCase() ?? null,
  });
}

export async function recordPharmacySupplierCredit(
  scope: PharmacyActorScope,
  credit: string,
  input: PharmacySupplierCreditInput,
  signal?: AbortSignal,
) {
  const body = pharmacySupplierCreditDocumentInputSchema.parse(input);
  const row = checkedReceipt(
    scope,
    credit,
    await apiFetch(root(scope, credit), {
      ...request(scope, signal),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  if (documentIntent(row) !== documentIntent(body)) return mismatch();
  return row;
}

export async function recoverPharmacySupplierCredit(
  scope: PharmacyActorScope,
  credit: string,
  signal?: AbortSignal,
) {
  return checkedReceipt(
    scope,
    credit,
    await apiFetch(`${root(scope, credit)}/recovery`, request(scope, signal)),
  );
}

const cursorSchema = z.string().min(1).max(4096);
const creditPage = z
  .object({
    data: z.array(pharmacySupplierCreditDocumentViewSchema).max(50),
    page: z.object({ next_cursor: cursorSchema.nullable(), has_more: z.boolean() }).strict(),
  })
  .strict();

export async function listPharmacySupplierCredits(
  scope: PharmacyActorScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ limit: "50" });
  if (cursor !== null) query.set("cursor", cursorSchema.parse(cursor));
  const page = creditPage.parse(
    await apiFetch(
      `${pharmacyRoot(scope)}/accounts/supplier-credits?${query}`,
      request(scope, signal),
    ),
  );
  if (
    page.page.has_more !== (page.page.next_cursor !== null) ||
    (page.page.has_more && (!page.data.length || page.page.next_cursor === cursor)) ||
    new Set(page.data.map((row) => row.id.toLowerCase())).size !== page.data.length
  )
    return mismatch();
  for (const row of page.data) checkedView(scope, row.id, row);
  return page;
}

function checkedVoid(scope: PharmacyActorScope, credit: string, value: unknown) {
  const row = pharmacySupplierCreditVoidReceiptSchema.parse(value);
  if (
    !sameID(row.credit_id, credit) ||
    !sameID(row.outlet_id, scope.outletId) ||
    row.version !== row.expected_version + 1
  )
    return mismatch();
  return row;
}

export async function voidPharmacySupplierCredit(
  scope: PharmacyActorScope,
  credit: string,
  entry: string,
  input: PharmacySupplierCreditVoidInput,
  signal?: AbortSignal,
) {
  const body = pharmacySupplierCreditVoidInputSchema.parse(input);
  const row = checkedVoid(
    scope,
    credit,
    await apiFetch(`${root(scope, credit)}/voids/${identifier.parse(entry)}`, {
      ...request(scope, signal),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  if (
    !sameID(row.id, entry) ||
    row.recorded_by !== scope.subject ||
    row.expected_version !== body.expected_version ||
    row.reason !== body.reason
  )
    return mismatch();
  return row;
}

export async function readPharmacySupplierCreditVoid(
  scope: PharmacyActorScope,
  credit: string,
  signal?: AbortSignal,
) {
  return checkedVoid(
    scope,
    credit,
    await apiFetch(`${root(scope, credit)}/void`, request(scope, signal)),
  );
}

export async function recoverPharmacySupplierCreditVoid(
  scope: PharmacyActorScope,
  credit: string,
  entry: string,
  signal?: AbortSignal,
) {
  const row = checkedVoid(
    scope,
    credit,
    await apiFetch(
      `${root(scope, credit)}/voids/${identifier.parse(entry)}/recovery`,
      request(scope, signal),
    ),
  );
  if (!sameID(row.id, entry) || row.recorded_by !== scope.subject) return mismatch();
  return row;
}

export { pharmacySupplierCreditDocumentInputSchema, pharmacySupplierCreditVoidInputSchema };

export async function readPharmacySupplierCredit(
  scope: PharmacyActorScope,
  credit: string,
  signal?: AbortSignal,
) {
  return checkedView(scope, credit, await apiFetch(root(scope, credit), request(scope, signal)));
}
import { z } from "zod";
