import { apiFetch } from "./client";
import { pharmacyKey, pharmacyRoot } from "./pharmacy";
import { pharmacyActorHeaders, type PharmacyActorScope } from "./pharmacy-actor";
import {
  pharmacySupplierCreditAllocationContextSchema,
  pharmacySupplierCreditAllocationInputSchema,
  pharmacySupplierCreditAllocationReceiptSchema,
  pharmacySupplierCreditAllocationViewSchema,
  pharmacySupplierCreditAllocationReversalInputSchema,
  pharmacySupplierCreditAllocationReversalReceiptSchema,
  pharmacySupplierCreditContextSchema,
  pharmacySupplierCreditPayableChoiceSchema,
  pharmacySupplierCreditReturnChoiceSchema,
  pharmacySupplierCreditDocumentInputSchema,
  pharmacySupplierCreditDocumentReceiptSchema,
  pharmacySupplierCreditDocumentViewSchema,
  pharmacySupplierCreditVoidInputSchema,
  pharmacySupplierCreditVoidReceiptSchema,
  pharmacySupplierCreditMatchInputSchema,
  pharmacySupplierCreditMatchReceiptSchema,
  pharmacySupplierCreditMatchViewSchema,
  pharmacySupplierCreditMatchReversalInputSchema,
  pharmacySupplierCreditMatchReversalReceiptSchema,
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

function checkCreditContinuation(
  value: Pick<z.infer<typeof creditPage>, "page"> & { data: { id: string }[] },
  cursor: string | null,
) {
  if (
    value.page.has_more !== (value.page.next_cursor !== null) ||
    (value.page.has_more && (!value.data.length || value.page.next_cursor === cursor)) ||
    new Set(value.data.map((row) => row.id.toLowerCase())).size !== value.data.length
  )
    return mismatch();
}

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
  checkCreditContinuation(page, cursor);
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

export {
  pharmacySupplierCreditDocumentInputSchema,
  pharmacySupplierCreditVoidInputSchema,
  pharmacySupplierCreditMatchInputSchema,
  pharmacySupplierCreditMatchReversalInputSchema,
};

export type PharmacySupplierCreditMatchInput = z.infer<
  typeof pharmacySupplierCreditMatchInputSchema
>;
export type PharmacySupplierCreditMatchReceipt = z.infer<
  typeof pharmacySupplierCreditMatchReceiptSchema
>;
export type PharmacySupplierCreditMatch = z.infer<typeof pharmacySupplierCreditMatchViewSchema>;

function checkedMatch(scope: PharmacyActorScope, credit: string, match: string, value: unknown) {
  const row = pharmacySupplierCreditMatchReceiptSchema.parse(value);
  if (
    !sameID(row.id, match) ||
    !sameID(row.credit_id, credit) ||
    !sameID(row.outlet_id, scope.outletId) ||
    row.version !== row.expected_version + 1 ||
    row.return_matched_minor !== row.expected_return_matched_minor + row.amount_minor ||
    (row.return_matched_minor > row.expected_credit_minor && !row.overage_reason)
  )
    return mismatch();
  return row;
}
function matchIntent(value: PharmacySupplierCreditMatchInput) {
  const input = pharmacySupplierCreditMatchInputSchema.strip().parse(value);
  return JSON.stringify({
    ...input,
    return_id: input.return_id.toLowerCase(),
    overage_reason: input.overage_reason ?? null,
  });
}
export async function matchPharmacySupplierCredit(
  scope: PharmacyActorScope,
  credit: string,
  match: string,
  input: PharmacySupplierCreditMatchInput,
  signal?: AbortSignal,
) {
  const body = pharmacySupplierCreditMatchInputSchema.parse(input);
  if (body.expected_return_matched_minor + body.amount_minor > 1000000000000) return mismatch();
  const row = checkedMatch(
    scope,
    credit,
    match,
    await apiFetch(`${root(scope, credit)}/matches/${identifier.parse(match)}`, {
      ...request(scope, signal),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  if (row.recorded_by !== scope.subject || matchIntent(row) !== matchIntent(body))
    return mismatch();
  return row;
}

export async function readPharmacySupplierCreditMatch(
  scope: PharmacyActorScope,
  credit: string,
  match: string,
  signal?: AbortSignal,
) {
  return checkedMatchView(
    scope,
    credit,
    match,
    await apiFetch(
      `${root(scope, credit)}/matches/${identifier.parse(match)}`,
      request(scope, signal),
    ),
  );
}

function checkedMatchView(
  scope: PharmacyActorScope,
  credit: string,
  match: string,
  value: unknown,
) {
  const row = pharmacySupplierCreditMatchViewSchema.parse(value);
  checkedMatch(scope, credit, match, pharmacySupplierCreditMatchReceiptSchema.strip().parse(row));
  return row;
}

const matchPage = creditPage.extend({
  data: z.array(pharmacySupplierCreditMatchViewSchema).max(50),
});
export async function listPharmacySupplierCreditMatches(
  scope: PharmacyActorScope,
  credit: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ limit: "50" });
  if (cursor !== null) query.set("cursor", cursorSchema.parse(cursor));
  const page = matchPage.parse(
    await apiFetch(`${root(scope, credit)}/matches?${query}`, request(scope, signal)),
  );
  checkCreditContinuation(page, cursor);
  for (const row of page.data) checkedMatchView(scope, credit, row.id, row);
  return page;
}

export async function recoverPharmacySupplierCreditMatch(
  scope: PharmacyActorScope,
  credit: string,
  match: string,
  signal?: AbortSignal,
) {
  const row = checkedMatch(
    scope,
    credit,
    match,
    await apiFetch(
      `${root(scope, credit)}/matches/${identifier.parse(match)}/recovery`,
      request(scope, signal),
    ),
  );
  if (row.recorded_by !== scope.subject) return mismatch();
  return row;
}

export type PharmacySupplierCreditMatchReversalInput = z.infer<
  typeof pharmacySupplierCreditMatchReversalInputSchema
>;
export type PharmacySupplierCreditMatchReversalReceipt = z.infer<
  typeof pharmacySupplierCreditMatchReversalReceiptSchema
>;
function checkedMatchReversal(
  scope: PharmacyActorScope,
  credit: string,
  match: string,
  value: unknown,
) {
  const row = pharmacySupplierCreditMatchReversalReceiptSchema.parse(value);
  if (
    !sameID(row.credit_id, credit) ||
    !sameID(row.match_id, match) ||
    !sameID(row.outlet_id, scope.outletId) ||
    row.version !== row.expected_version + 1 ||
    row.return_matched_minor !== row.expected_return_matched_minor - row.amount_minor
  )
    return mismatch();
  return row;
}

export async function reversePharmacySupplierCreditMatch(
  scope: PharmacyActorScope,
  credit: string,
  original: PharmacySupplierCreditMatchReceipt,
  entry: string,
  input: PharmacySupplierCreditMatchReversalInput,
  signal?: AbortSignal,
) {
  const match = checkedMatch(
    scope,
    credit,
    original.id,
    pharmacySupplierCreditMatchReceiptSchema.strip().parse(original),
  );
  const body = pharmacySupplierCreditMatchReversalInputSchema.parse(input);
  if (body.expected_return_matched_minor < match.amount_minor) return mismatch();
  const row = checkedMatchReversal(
    scope,
    credit,
    match.id,
    await apiFetch(
      `${root(scope, credit)}/matches/${identifier.parse(match.id)}/reversals/${identifier.parse(entry)}`,
      {
        ...request(scope, signal),
        method: "PUT",
        body: JSON.stringify(body),
      },
    ),
  );
  if (
    !sameID(row.id, entry) ||
    row.recorded_by !== scope.subject ||
    row.expected_version !== body.expected_version ||
    row.expected_return_matched_minor !== body.expected_return_matched_minor ||
    row.reason !== body.reason ||
    !sameID(row.return_id, match.return_id) ||
    row.return_number !== match.return_number ||
    row.amount_minor !== match.amount_minor
  )
    return mismatch();
  return row;
}

export async function readPharmacySupplierCreditMatchReversal(
  scope: PharmacyActorScope,
  credit: string,
  match: string,
  signal?: AbortSignal,
) {
  return checkedMatchReversal(
    scope,
    credit,
    match,
    await apiFetch(
      `${root(scope, credit)}/matches/${identifier.parse(match)}/reversal`,
      request(scope, signal),
    ),
  );
}

export async function recoverPharmacySupplierCreditMatchReversal(
  scope: PharmacyActorScope,
  credit: string,
  match: string,
  entry: string,
  signal?: AbortSignal,
) {
  const row = checkedMatchReversal(
    scope,
    credit,
    match,
    await apiFetch(
      `${root(scope, credit)}/matches/${identifier.parse(match)}/reversals/${identifier.parse(entry)}/recovery`,
      request(scope, signal),
    ),
  );
  if (!sameID(row.id, entry) || row.recorded_by !== scope.subject) return mismatch();
  return row;
}

export async function readPharmacySupplierCredit(
  scope: PharmacyActorScope,
  credit: string,
  signal?: AbortSignal,
) {
  return checkedView(scope, credit, await apiFetch(root(scope, credit), request(scope, signal)));
}
import { z } from "zod";

export type PharmacySupplierCreditAllocationInput = z.infer<
  typeof pharmacySupplierCreditAllocationInputSchema
>;
export type PharmacySupplierCreditAllocationReceipt = z.infer<
  typeof pharmacySupplierCreditAllocationReceiptSchema
>;
function checkedAllocation(
  scope: PharmacyActorScope,
  credit: string,
  allocation: string,
  value: unknown,
) {
  const row = pharmacySupplierCreditAllocationReceiptSchema.parse(value);
  if (
    !sameID(row.credit_id, credit) ||
    !sameID(row.id, allocation) ||
    !sameID(row.outlet_id, scope.outletId) ||
    row.version !== row.expected_version + 1 ||
    row.payable_version !== row.expected_payable_version + 1 ||
    row.available_minor !== row.expected_available_minor - row.amount_minor ||
    row.outstanding_minor !== row.expected_outstanding_minor - row.amount_minor
  )
    return mismatch();
  return row;
}
function allocationIntent(value: PharmacySupplierCreditAllocationInput) {
  const input = pharmacySupplierCreditAllocationInputSchema.strip().parse(value);
  return JSON.stringify({ ...input, payable_id: input.payable_id.toLowerCase() });
}
export async function allocatePharmacySupplierCredit(
  scope: PharmacyActorScope,
  credit: string,
  allocation: string,
  input: PharmacySupplierCreditAllocationInput,
  signal?: AbortSignal,
) {
  const body = pharmacySupplierCreditAllocationInputSchema.parse(input);
  if (
    body.amount_minor > body.expected_available_minor ||
    body.amount_minor > body.expected_outstanding_minor
  )
    return mismatch();
  const row = checkedAllocation(
    scope,
    credit,
    allocation,
    await apiFetch(`${root(scope, credit)}/allocations/${identifier.parse(allocation)}`, {
      ...request(scope, signal),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  if (row.recorded_by !== scope.subject || allocationIntent(row) !== allocationIntent(body))
    return mismatch();
  return row;
}

function checkedAllocationView(
  scope: PharmacyActorScope,
  credit: string,
  allocation: string,
  value: unknown,
) {
  const row = pharmacySupplierCreditAllocationViewSchema.parse(value);
  checkedAllocation(
    scope,
    credit,
    allocation,
    pharmacySupplierCreditAllocationReceiptSchema.strip().parse(row),
  );
  return row;
}
export async function readPharmacySupplierCreditAllocation(
  scope: PharmacyActorScope,
  credit: string,
  allocation: string,
  signal?: AbortSignal,
) {
  return checkedAllocationView(
    scope,
    credit,
    allocation,
    await apiFetch(
      `${root(scope, credit)}/allocations/${identifier.parse(allocation)}`,
      request(scope, signal),
    ),
  );
}
export async function recoverPharmacySupplierCreditAllocation(
  scope: PharmacyActorScope,
  credit: string,
  allocation: string,
  signal?: AbortSignal,
) {
  const row = checkedAllocation(
    scope,
    credit,
    allocation,
    await apiFetch(
      `${root(scope, credit)}/allocations/${identifier.parse(allocation)}/recovery`,
      request(scope, signal),
    ),
  );
  if (row.recorded_by !== scope.subject) return mismatch();
  return row;
}

export type PharmacySupplierCreditAllocationReversalInput = z.infer<
  typeof pharmacySupplierCreditAllocationReversalInputSchema
>;
export type PharmacySupplierCreditAllocationReversalReceipt = z.infer<
  typeof pharmacySupplierCreditAllocationReversalReceiptSchema
>;
function checkedAllocationReversal(
  scope: PharmacyActorScope,
  credit: string,
  allocation: string,
  value: unknown,
) {
  const row = pharmacySupplierCreditAllocationReversalReceiptSchema.parse(value);
  if (
    !sameID(row.credit_id, credit) ||
    !sameID(row.allocation_id, allocation) ||
    !sameID(row.outlet_id, scope.outletId) ||
    row.version !== row.expected_version + 1 ||
    row.payable_version !== row.expected_payable_version + 1 ||
    row.available_minor !== row.expected_available_minor + row.amount_minor ||
    row.outstanding_minor !== row.expected_outstanding_minor + row.amount_minor
  )
    return mismatch();
  return row;
}
export async function reversePharmacySupplierCreditAllocation(
  scope: PharmacyActorScope,
  credit: string,
  original: PharmacySupplierCreditAllocationReceipt,
  entry: string,
  input: PharmacySupplierCreditAllocationReversalInput,
  signal?: AbortSignal,
) {
  const allocation = checkedAllocation(
    scope,
    credit,
    original.id,
    pharmacySupplierCreditAllocationReceiptSchema.strip().parse(original),
  );
  const body = pharmacySupplierCreditAllocationReversalInputSchema.parse(input);
  if (
    body.expected_available_minor + allocation.amount_minor > 1000000000000 ||
    body.expected_outstanding_minor + allocation.amount_minor > 1000000000000
  )
    return mismatch();
  const row = checkedAllocationReversal(
    scope,
    credit,
    allocation.id,
    await apiFetch(
      `${root(scope, credit)}/allocations/${identifier.parse(allocation.id)}/reversals/${identifier.parse(entry)}`,
      {
        ...request(scope, signal),
        method: "PUT",
        body: JSON.stringify(body),
      },
    ),
  );
  if (
    !sameID(row.id, entry) ||
    row.recorded_by !== scope.subject ||
    !sameID(row.payable_id, allocation.payable_id) ||
    row.amount_minor !== allocation.amount_minor ||
    row.payable_reference !== allocation.payable_reference ||
    row.credit_document_reference !== allocation.credit_document_reference ||
    row.payee_name !== allocation.payee_name ||
    JSON.stringify(pharmacySupplierCreditAllocationReversalInputSchema.strip().parse(row)) !==
      JSON.stringify(body)
  )
    return mismatch();
  return row;
}

const allocationPage = creditPage.extend({
  data: z.array(pharmacySupplierCreditAllocationViewSchema).max(50),
});
export async function listPharmacySupplierCreditAllocations(
  scope: PharmacyActorScope,
  credit: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ limit: "50" });
  if (cursor !== null) query.set("cursor", cursorSchema.parse(cursor));
  const page = allocationPage.parse(
    await apiFetch(`${root(scope, credit)}/allocations?${query}`, request(scope, signal)),
  );
  checkCreditContinuation(page, cursor);
  for (const row of page.data) checkedAllocationView(scope, credit, row.id, row);
  return page;
}
export async function readPharmacySupplierCreditAllocationReversal(
  scope: PharmacyActorScope,
  credit: string,
  allocation: string,
  signal?: AbortSignal,
) {
  return checkedAllocationReversal(
    scope,
    credit,
    allocation,
    await apiFetch(
      `${root(scope, credit)}/allocations/${identifier.parse(allocation)}/reversal`,
      request(scope, signal),
    ),
  );
}
export async function recoverPharmacySupplierCreditAllocationReversal(
  scope: PharmacyActorScope,
  credit: string,
  allocation: string,
  entry: string,
  signal?: AbortSignal,
) {
  const row = checkedAllocationReversal(
    scope,
    credit,
    allocation,
    await apiFetch(
      `${root(scope, credit)}/allocations/${identifier.parse(allocation)}/reversals/${identifier.parse(entry)}/recovery`,
      request(scope, signal),
    ),
  );
  if (!sameID(row.id, entry) || row.recorded_by !== scope.subject) return mismatch();
  return row;
}

export type PharmacySupplierCreditContext = z.infer<typeof pharmacySupplierCreditContextSchema>;
export type PharmacySupplierCreditPayableChoice = z.infer<
  typeof pharmacySupplierCreditPayableChoiceSchema
>;
export type PharmacySupplierCreditReturnChoice = z.infer<
  typeof pharmacySupplierCreditReturnChoiceSchema
>;
function checkedCreditContext(scope: PharmacyActorScope, credit: string, value: unknown) {
  const context = pharmacySupplierCreditContextSchema.parse(value);
  checkedView(scope, credit, pharmacySupplierCreditDocumentViewSchema.strip().parse(context));
  const asOf = z.string().datetime({ offset: true }).parse(context.as_of);
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: context.timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(new Date(asOf));
  const part = (type: string) => parts.find((value) => value.type === type)?.value;
  if (context.local_date !== `${part("year")}-${part("month")}-${part("day")}`) return mismatch();
  return context;
}
export async function readPharmacySupplierCreditContext(
  scope: PharmacyActorScope,
  credit: string,
  signal?: AbortSignal,
) {
  return checkedCreditContext(
    scope,
    credit,
    await apiFetch(`${root(scope, credit)}/context`, request(scope, signal)),
  );
}
const payableChoicePage = creditPage.extend({
  data: z.array(pharmacySupplierCreditPayableChoiceSchema).max(50),
});
const returnChoicePage = creditPage.extend({
  data: z.array(pharmacySupplierCreditReturnChoiceSchema).max(50),
});
export async function listPharmacySupplierCreditPayableChoices(
  scope: PharmacyActorScope,
  credit: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ limit: "50" });
  if (cursor !== null) query.set("cursor", cursorSchema.parse(cursor));
  const page = payableChoicePage.parse(
    await apiFetch(`${root(scope, credit)}/eligible-payables?${query}`, request(scope, signal)),
  );
  checkCreditContinuation(page, cursor);
  for (const row of page.data)
    if (
      !sameID(row.outlet_id, scope.outletId) ||
      row.total_minor !== row.paid_minor + row.credit_minor + row.outstanding_minor
    )
      return mismatch();
  return page;
}
export async function listPharmacySupplierCreditReturnChoices(
  scope: PharmacyActorScope,
  credit: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ limit: "50" });
  if (cursor !== null) query.set("cursor", cursorSchema.parse(cursor));
  const page = returnChoicePage.parse(
    await apiFetch(`${root(scope, credit)}/eligible-returns?${query}`, request(scope, signal)),
  );
  checkCreditContinuation(page, cursor);
  for (const row of page.data) if (!sameID(row.outlet_id, scope.outletId)) return mismatch();
  return page;
}

export type PharmacySupplierCreditAllocationContext = z.infer<
  typeof pharmacySupplierCreditAllocationContextSchema
>;
export async function readPharmacySupplierCreditAllocationContext(
  scope: PharmacyActorScope,
  credit: string,
  allocation: string,
  signal?: AbortSignal,
) {
  const context = pharmacySupplierCreditAllocationContextSchema.parse(
    await apiFetch(
      `${root(scope, credit)}/allocations/${identifier.parse(allocation)}/context`,
      request(scope, signal),
    ),
  );
  const current = checkedCreditContext(scope, credit, context.credit);
  const original = checkedAllocationView(scope, credit, allocation, context.allocation);
  const bill = context.payable;
  const reversed = original.reversal_id !== null;
  if (
    !sameID(bill.id, original.payable_id) ||
    !sameID(bill.outlet_id, scope.outletId) ||
    bill.total_minor !== bill.paid_minor + bill.credit_minor + bill.outstanding_minor ||
    bill.currency !== current.currency ||
    bill.document_reference !== original.payable_reference ||
    current.document_reference !== original.credit_document_reference ||
    current.payee_name !== original.payee_name ||
    bill.version < original.payable_version + (reversed ? 1 : 0) ||
    current.version < original.version + (reversed ? 1 : 0) ||
    (!reversed &&
      (current.state !== "active" ||
        current.allocated_minor < original.amount_minor ||
        bill.credit_minor < original.amount_minor))
  )
    return mismatch();
  return context;
}
