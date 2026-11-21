import { z } from "zod";
import { apiFetch } from "./client";
import { pharmacyKey, pharmacyRoot } from "./pharmacy";
import { pharmacyActorHeaders, type PharmacyActorScope } from "./pharmacy-actor";
import {
  pharmacyStocktakeCountInputSchema,
  pharmacyStocktakeCountReceiptSchema,
  pharmacyStocktakeDecisionInputSchema,
  pharmacyStocktakeDecisionReceiptSchema,
  pharmacyStocktakeLineSchema,
  pharmacyStocktakeSchema,
  pharmacyStocktakeSummarySchema,
  pharmacyStocktakeStartInputSchema,
} from "./pharmacy-contract";

export type PharmacyStocktake = z.infer<typeof pharmacyStocktakeSchema>;
export type PharmacyStocktakeScope = PharmacyActorScope;
export function stocktakeKey(scope: PharmacyStocktakeScope, ...parts: unknown[]) {
  return pharmacyKey(scope, "stocktake-actor", scope.subject, ...parts);
}
export function stocktakeHeaders(scope: PharmacyStocktakeScope) {
  return pharmacyActorHeaders(scope);
}
export type PharmacyStocktakeSummary = z.infer<typeof pharmacyStocktakeSummarySchema>;
export type PharmacyStocktakeLine = z.infer<typeof pharmacyStocktakeLineSchema>;
export type PharmacyStocktakeStartInput = z.infer<typeof pharmacyStocktakeStartInputSchema>;
export type PharmacyStocktakeCountInput = z.infer<typeof pharmacyStocktakeCountInputSchema>;
export type PharmacyStocktakeCountReceipt = z.infer<typeof pharmacyStocktakeCountReceiptSchema>;
export type PharmacyStocktakeDecisionInput = z.infer<typeof pharmacyStocktakeDecisionInputSchema>;
export type PharmacyStocktakeDecisionReceipt = z.infer<
  typeof pharmacyStocktakeDecisionReceiptSchema
>;

const id = z.string().guid();
const continuation = z.object({ next_cursor: z.string().min(1).nullable(), has_more: z.boolean() });
const linePage = z.object({
  data: z.array(pharmacyStocktakeLineSchema).max(200),
  page: continuation,
});
const takePage = z.object({
  data: z.array(pharmacyStocktakeSummarySchema).max(200),
  page: continuation,
});

export async function fetchStocktakes(
  scope: PharmacyStocktakeScope,
  cursor: string | null,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams();
  if (cursor) query.set("cursor", cursor);
  const page = takePage.parse(
    await apiFetch(`${pharmacyRoot(scope)}/stock/stocktakes?${query}`, request(scope, signal)),
  );
  if (page.data.some((take) => take.outlet_id !== scope.outletId))
    throw new Error("The stocktake page does not match this outlet.");
  if (page.page.has_more !== Boolean(page.page.next_cursor))
    throw new Error("The stocktake page has an invalid continuation.");
  return page;
}

function request(scope: PharmacyStocktakeScope, signal?: AbortSignal): RequestInit {
  const timeout = AbortSignal.timeout(15000);
  return {
    headers: stocktakeHeaders(scope),
    signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
    cache: "no-store",
  };
}

function root(scope: PharmacyStocktakeScope, take: string) {
  return `${pharmacyRoot(scope)}/stock/stocktakes/${id.parse(take)}`;
}

function countIntent(input: PharmacyStocktakeCountInput) {
  return JSON.stringify({
    expected_version: input.expected_version,
    reason: input.reason,
    counts: [...input.counts].sort((a, b) => a.line_id.localeCompare(b.line_id)),
  });
}

function checkedCount(
  scope: PharmacyStocktakeScope,
  take: string,
  command: string,
  value: unknown,
) {
  const receipt = pharmacyStocktakeCountReceiptSchema.parse(value);
  if (
    receipt.id !== command ||
    receipt.outlet_id !== scope.outletId ||
    receipt.stocktake_id !== take ||
    receipt.version !== receipt.expected_version + 1
  )
    throw new Error("The counted observation receipt does not match this outlet and command.");
  return receipt;
}

export async function readStocktakeCount(
  scope: PharmacyStocktakeScope,
  take: string,
  command: string,
  signal?: AbortSignal,
) {
  return checkedCount(
    scope,
    take,
    command,
    await apiFetch(`${root(scope, take)}/counts/${id.parse(command)}`, request(scope, signal)),
  );
}

const decisionStates = {
  submit: "submitted",
  approve: "posted",
  reject: "rejected",
  cancel: "cancelled",
} as const;

function checkedDecision(
  scope: PharmacyStocktakeScope,
  take: string,
  command: string,
  value: unknown,
) {
  const result = pharmacyStocktakeDecisionReceiptSchema.parse(value);
  if (
    result.id !== command ||
    result.stocktake_id !== take ||
    result.outlet_id !== scope.outletId ||
    result.version !== result.expected_version + 1 ||
    result.state !== decisionStates[result.decision] ||
    (result.decision !== "approve" &&
      (result.adjustment_count !== 0 || result.extra_units_disposition != null)) ||
    (result.decision === "approve" &&
      (result.extra_units_disposition == null ||
        (result.adjustment_count === 0 && result.extra_units_disposition !== "no_extra_units")))
  )
    throw new Error("The review receipt does not match this stocktake and command.");
  return result;
}

function decisionIntent(input: PharmacyStocktakeDecisionInput) {
  return JSON.stringify({
    decision: input.decision,
    expected_version: input.expected_version,
    reason: input.reason,
    extra_units_disposition: input.extra_units_disposition ?? null,
  });
}

export async function recordStocktakeDecision(
  scope: PharmacyStocktakeScope,
  take: string,
  command: string,
  input: PharmacyStocktakeDecisionInput,
) {
  const intent = pharmacyStocktakeDecisionInputSchema.parse(input);
  const collection = intent.decision === "cancel" ? "cancellations" : "decisions";
  const result = checkedDecision(
    scope,
    take,
    command,
    await apiFetch(`${root(scope, take)}/${collection}/${id.parse(command)}`, {
      ...request(scope),
      method: "PUT",
      body: JSON.stringify(intent),
    }),
  );
  if (result.recorded_by !== scope.subject || decisionIntent(result) !== decisionIntent(intent))
    throw new Error("The review receipt does not match the submitted decision.");
  return result;
}

export async function readStocktakeDecision(
  scope: PharmacyStocktakeScope,
  take: string,
  command: string,
  signal?: AbortSignal,
) {
  return checkedDecision(
    scope,
    take,
    command,
    await apiFetch(`${root(scope, take)}/decisions/${id.parse(command)}`, request(scope, signal)),
  );
}

export async function readStocktake(
  scope: PharmacyStocktakeScope,
  take: string,
  signal?: AbortSignal,
) {
  const result = pharmacyStocktakeSchema.parse(
    await apiFetch(root(scope, take), request(scope, signal)),
  );
  if (result.id !== take || result.outlet_id !== scope.outletId)
    throw new Error("The stocktake does not match this outlet.");
  if (
    result.counted_rows > result.snapshot_rows ||
    result.difference_rows > result.counted_rows ||
    result.positive_difference_rows > result.difference_rows
  )
    throw new Error("The stocktake progress is inconsistent. Refresh before continuing.");
  return result;
}

function matchesStart(
  receipt: PharmacyStocktake,
  intent: PharmacyStocktakeStartInput,
  subject: string,
) {
  return (
    receipt.review_relationship === "starter" &&
    receipt.started_by === subject &&
    receipt.selection === intent.selection &&
    receipt.quarantine_location_id === intent.quarantine_location_id &&
    receipt.reason === intent.reason &&
    JSON.stringify([...receipt.product_ids].sort()) ===
      JSON.stringify([...intent.product_ids].sort())
  );
}

export async function recoverStocktakeStart(
  scope: PharmacyStocktakeScope,
  take: string,
  input: PharmacyStocktakeStartInput,
  signal?: AbortSignal,
) {
  const intent = pharmacyStocktakeStartInputSchema.parse(input);
  const current = await readStocktake(scope, take, signal);
  if (!matchesStart(current, intent, scope.subject))
    throw new Error("The recovered stocktake does not match this start and its original actor.");
  return current;
}

export async function startStocktake(
  scope: PharmacyStocktakeScope,
  take: string,
  input: PharmacyStocktakeStartInput,
) {
  const intent = pharmacyStocktakeStartInputSchema.parse(input);
  const receipt = pharmacyStocktakeSchema.parse(
    await apiFetch(root(scope, take), {
      ...request(scope),
      method: "PUT",
      body: JSON.stringify(intent),
    }),
  );
  if (
    receipt.id !== take ||
    receipt.outlet_id !== scope.outletId ||
    receipt.state !== "counting" ||
    receipt.version !== 0 ||
    receipt.counted_rows !== 0 ||
    receipt.difference_rows !== 0 ||
    receipt.positive_difference_rows !== 0 ||
    !matchesStart(receipt, intent, scope.subject)
  )
    throw new Error("The stocktake creation receipt does not match this outlet and command.");
  return receipt;
}

export async function recordStocktakeCount(
  scope: PharmacyStocktakeScope,
  take: string,
  command: string,
  input: PharmacyStocktakeCountInput,
) {
  const intent = pharmacyStocktakeCountInputSchema.parse(input);
  const receipt = checkedCount(
    scope,
    take,
    command,
    await apiFetch(`${root(scope, take)}/counts/${id.parse(command)}`, {
      ...request(scope),
      method: "PUT",
      body: JSON.stringify(intent),
    }),
  );
  if (receipt.recorded_by !== scope.subject || countIntent(receipt) !== countIntent(intent))
    throw new Error("The counted observation receipt does not match this outlet and command.");
  return receipt;
}

export async function fetchStocktakeLines(
  scope: PharmacyStocktakeScope,
  take: string,
  cursor: string | null,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams();
  if (cursor) query.set("cursor", cursor);
  const page = linePage.parse(
    await apiFetch(`${root(scope, take)}/lines?${query}`, request(scope, signal)),
  );
  if (page.data.some((line) => line.stocktake_id !== take))
    throw new Error("The counted lines do not match this stocktake.");
  if (page.page.has_more !== Boolean(page.page.next_cursor))
    throw new Error("The stocktake page has an invalid continuation.");
  return page;
}
