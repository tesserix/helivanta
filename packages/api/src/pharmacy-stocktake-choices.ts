import { z } from "zod";
import { apiFetch } from "./client";
import {
  PharmacyReceivingProductContract,
  PharmacyReceivingLocationContract,
} from "./pharmacy-receiving";
import {
  stocktakeAuthority,
  stocktakeCollectionRoot,
  stocktakeHeaders,
  type PharmacyStocktakeScope,
} from "./pharmacy-stocktakes";

export type StocktakeProductChoice = z.infer<typeof PharmacyReceivingProductContract>;
export type StocktakeQuarantineChoice = z.infer<typeof PharmacyReceivingLocationContract>;
const cursorContract = z.string().min(1).max(2048);
const quarantineContract = PharmacyReceivingLocationContract.refine(
  (row) => row.stock_status === "quarantine",
  "Choose a quarantine location.",
);

async function choices<T extends { id: string; outlet_id: string }>(
  scope: PharmacyStocktakeScope,
  collection: "product-choices" | "quarantine-choices",
  row: z.ZodType<T>,
  cursor: string | null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  if (stocktakeAuthority(scope) === "legacy")
    throw new Error("A current reviewed stocktake scope is required.");
  const query = new URLSearchParams({ limit: "100" });
  if (cursor !== null) query.set("cursor", cursorContract.parse(cursor));
  const timeout = AbortSignal.timeout(15000);
  const page = z
    .object({
      data: z.array(row).max(200),
      page: z.object({ has_more: z.boolean(), next_cursor: cursorContract.nullable() }).strict(),
    })
    .strict()
    .parse(
      await apiFetch<unknown>(`${stocktakeCollectionRoot(scope)}/${collection}?${query}`, {
        headers: stocktakeHeaders(scope),
        cache: "no-store",
        signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
      }),
    );
  if (
    page.data.some((value) => value.outlet_id !== scope.outletId) ||
    new Set(page.data.map((value) => value.id)).size !== page.data.length ||
    page.page.has_more !== (page.page.next_cursor !== null) ||
    (page.page.has_more && page.page.next_cursor === cursor)
  )
    throw new Error("The named stocktake choices could not be verified for this outlet.");
  return page;
}

export function fetchStocktakeProductChoices(
  scope: PharmacyStocktakeScope,
  cursor: string | null,
  signal?: AbortSignal,
) {
  return choices(scope, "product-choices", PharmacyReceivingProductContract, cursor, signal);
}

export function fetchStocktakeQuarantineChoices(
  scope: PharmacyStocktakeScope,
  cursor: string | null,
  signal?: AbortSignal,
) {
  return choices(scope, "quarantine-choices", quarantineContract, cursor, signal);
}
