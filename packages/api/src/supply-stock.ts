import { z } from "zod";
import { useInfiniteQuery } from "@tanstack/react-query";
import { apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import type { SupplyScope } from "./supply";
import { ReferenceContract } from "./stock-product-contract";
import {
  WholesaleReceiptContract,
  WholesaleReceiptInputContract,
  type WholesaleReceiptInput,
} from "./supply-stock-contract";

export type WholesaleStockScope = SupplyScope & { partyId: string; premiseId: string };
export { v7 as newWholesaleReceiptId } from "uuid";
const id = z.uuid().regex(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
export const wholesaleStockKey = (scope: WholesaleStockScope) => [
  "supply-stock",
  scope.tenantId,
  scope.subject,
  scope.partyId,
  scope.premiseId,
];
const base = (scope: WholesaleStockScope) =>
  `/supply/parties/${id.parse(scope.partyId)}/operating-locations/${id.parse(scope.premiseId)}/stock/receipts`;

function owned(scope: WholesaleStockScope, value: unknown, receiptId: string) {
  const receipt = WholesaleReceiptContract.parse(value);
  if (
    receipt.id !== receiptId ||
    receipt.owner_tenant_id.toLowerCase() !== scope.tenantId.toLowerCase() ||
    receipt.party_id !== scope.partyId ||
    receipt.premise_id !== scope.premiseId
  )
    throw new Error("The stock receipt belongs to another workspace or location.");
  return receipt;
}
function exact(
  scope: WholesaleStockScope,
  input: WholesaleReceiptInput,
  receipt: ReturnType<typeof owned>,
) {
  if (
    receipt.actor_subject !== scope.subject ||
    Object.entries(input).some(([key, value]) =>
      key === "product_version_id"
        ? receipt.input.product_version_id.toLowerCase() !== (value as string).toLowerCase()
        : receipt.input[key as keyof WholesaleReceiptInput] !== value,
    )
  )
    throw new Error("The stock receipt does not match the original count or account.");
  return receipt;
}
export async function recordWholesaleReceipt(
  scope: WholesaleStockScope,
  receiptId: string,
  input: WholesaleReceiptInput,
) {
  const body = WholesaleReceiptInputContract.parse(input);
  const result = await apiFetch(`${base(scope)}/${id.parse(receiptId)}`, {
    ...membershipRequestOptions(scope),
    method: "PUT",
    body: JSON.stringify(body),
  });
  return exact(scope, body, owned(scope, result, receiptId));
}
export async function getWholesaleReceipt(
  scope: WholesaleStockScope,
  receiptId: string,
  signal?: AbortSignal,
) {
  const result = await apiFetch(`${base(scope)}/${id.parse(receiptId)}`, {
    ...membershipRequestOptions(scope),
    signal,
  });
  return owned(scope, result, receiptId);
}
export async function recoverWholesaleReceipt(
  scope: WholesaleStockScope,
  receiptId: string,
  input: WholesaleReceiptInput,
) {
  return exact(
    scope,
    WholesaleReceiptInputContract.parse(input),
    await getWholesaleReceipt(scope, receiptId),
  );
}

const stockPage = z
  .object({
    data: z.array(WholesaleReceiptContract).max(25),
    page: z
      .object({ has_more: z.boolean(), next_cursor: z.string().min(1).max(4096).nullable() })
      .strict(),
  })
  .strict()
  .superRefine((value, ctx) => {
    if (
      value.page.has_more !== (value.page.next_cursor !== null) ||
      (value.page.has_more && value.data.length === 0) ||
      new Set(value.data.map((row) => row.id)).size !== value.data.length
    )
      ctx.addIssue({ code: "custom", message: "The stock receipt page is inconsistent." });
  });
async function listWholesaleReceipts(
  scope: WholesaleStockScope,
  cursor: string | null,
  signal?: AbortSignal,
) {
  const params = new URLSearchParams({ limit: "25" });
  if (cursor) params.set("cursor", cursor);
  const page = stockPage.parse(
    await apiFetch(`${base(scope)}?${params}`, { ...membershipRequestOptions(scope), signal }),
  );
  return { ...page, data: page.data.map((row) => owned(scope, row, row.id)) };
}
export function useWholesaleReceipts(scope: WholesaleStockScope, enabled: boolean) {
  return useInfiniteQuery({
    queryKey: wholesaleStockKey(scope),
    enabled,
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: true,
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) => listWholesaleReceipts(scope, pageParam, signal),
    getNextPageParam: (last) => (last.page.has_more ? last.page.next_cursor : undefined),
  });
}
export async function getStockProductReference(
  scope: WholesaleStockScope,
  barcode: string,
  signal?: AbortSignal,
) {
  if (!/^(?:[0-9]{8}|[0-9]{12,14})$/.test(barcode))
    throw new Error("Scan a valid product barcode.");
  return ReferenceContract.parse(
    await apiFetch(`${base(scope).replace(/\/receipts$/, "")}/products/by-gtin/${barcode}`, {
      ...membershipRequestOptions(scope),
      signal,
    }),
  );
}
