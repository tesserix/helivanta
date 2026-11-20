import { z } from "zod";
import { apiFetch } from "./client";
import { pharmacyRoot, type PharmacyScope } from "./pharmacy";
import {
  pharmacyStocktakeCountInputSchema,
  pharmacyStocktakeCountReceiptSchema,
  pharmacyStocktakeLineSchema,
  pharmacyStocktakeSchema,
  pharmacyStocktakeStartInputSchema,
} from "./pharmacy-contract";

export type PharmacyStocktake = z.infer<typeof pharmacyStocktakeSchema>;
export type PharmacyStocktakeLine = z.infer<typeof pharmacyStocktakeLineSchema>;
export type PharmacyStocktakeStartInput = z.infer<typeof pharmacyStocktakeStartInputSchema>;
export type PharmacyStocktakeCountInput = z.infer<typeof pharmacyStocktakeCountInputSchema>;
export type PharmacyStocktakeCountReceipt = z.infer<typeof pharmacyStocktakeCountReceiptSchema>;

const id = z.string().guid();
const continuation = z.object({ next_cursor: z.string().min(1).nullable(), has_more: z.boolean() });
const linePage = z.object({
  data: z.array(pharmacyStocktakeLineSchema).max(200),
  page: continuation,
});

function request(scope: PharmacyScope, signal?: AbortSignal): RequestInit {
  const timeout = AbortSignal.timeout(15000);
  return {
    headers: { "X-Helivanta-Expected-Tenant": id.parse(scope.tenantId) },
    signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
    cache: "no-store",
  };
}

function root(scope: PharmacyScope, take: string) {
  return `${pharmacyRoot(scope)}/stock/stocktakes/${id.parse(take)}`;
}

function countIntent(input: PharmacyStocktakeCountInput) {
  return JSON.stringify({
    expected_version: input.expected_version,
    reason: input.reason,
    counts: [...input.counts].sort((a, b) => a.line_id.localeCompare(b.line_id)),
  });
}

export async function readStocktake(scope: PharmacyScope, take: string, signal?: AbortSignal) {
  const result = pharmacyStocktakeSchema.parse(
    await apiFetch(root(scope, take), request(scope, signal)),
  );
  if (result.id !== take || result.outlet_id !== scope.outletId)
    throw new Error("The stocktake does not match this outlet.");
  if (result.counted_rows > result.snapshot_rows || result.difference_rows > result.counted_rows)
    throw new Error("The stocktake progress is inconsistent. Refresh before continuing.");
  return result;
}

export async function startStocktake(
  scope: PharmacyScope,
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
    receipt.selection !== intent.selection ||
    receipt.quarantine_location_id !== intent.quarantine_location_id ||
    receipt.reason !== intent.reason ||
    JSON.stringify([...receipt.product_ids].sort()) !==
      JSON.stringify([...intent.product_ids].sort())
  )
    throw new Error("The stocktake creation receipt does not match this outlet and command.");
  return receipt;
}

export async function recordStocktakeCount(
  scope: PharmacyScope,
  take: string,
  command: string,
  input: PharmacyStocktakeCountInput,
) {
  const intent = pharmacyStocktakeCountInputSchema.parse(input);
  const receipt = pharmacyStocktakeCountReceiptSchema.parse(
    await apiFetch(`${root(scope, take)}/counts/${id.parse(command)}`, {
      ...request(scope),
      method: "PUT",
      body: JSON.stringify(intent),
    }),
  );
  if (
    receipt.id !== command ||
    receipt.outlet_id !== scope.outletId ||
    receipt.stocktake_id !== take ||
    receipt.version !== receipt.expected_version + 1 ||
    countIntent(receipt) !== countIntent(intent)
  )
    throw new Error("The counted observation receipt does not match this outlet and command.");
  return receipt;
}

export async function fetchStocktakeLines(
  scope: PharmacyScope,
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
