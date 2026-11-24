import { z } from "zod";
import { apiFetch } from "./client";
import type { Page } from "./paged";
import { pharmacyRoot, type PharmacyScope } from "./pharmacy";
import {
  pharmacyLowStockProductSchema,
  pharmacyOverviewStockSchema,
  pharmacyReorderLevelInputSchema,
  pharmacyReorderLevelReceiptSchema,
  pharmacyReorderProductSchema,
  pharmacyLowStockOverviewSchema,
} from "./pharmacy-contract";

export type PharmacyOverviewStock = z.infer<typeof pharmacyOverviewStockSchema>;
export type PharmacyReorderLevelInput = z.infer<typeof pharmacyReorderLevelInputSchema>;
export type PharmacyReorderLevelReceipt = z.infer<typeof pharmacyReorderLevelReceiptSchema>;
export type PharmacyLowStockProduct = z.infer<typeof pharmacyLowStockProductSchema>;
export type PharmacyReorderProduct = z.infer<typeof pharmacyReorderProductSchema>;
export type PharmacyLowStockOverview = z.infer<typeof pharmacyLowStockOverviewSchema>;
const id = z.string().guid();
const pageSchema = z.object({
  data: z.array(pharmacyLowStockProductSchema).max(200),
  page: z.object({ next_cursor: z.string().nullable(), has_more: z.boolean() }),
});
const productsPageSchema = pageSchema.extend({
  data: z.array(pharmacyReorderProductSchema).max(200),
});

export async function fetchReorderProductsPage(
  scope: PharmacyScope,
  cursor: string | null,
  signal?: AbortSignal,
): Promise<Page<PharmacyReorderProduct>> {
  const query = new URLSearchParams();
  if (cursor) query.set("cursor", cursor);
  const page = productsPageSchema.parse(
    await apiFetch(
      `${pharmacyRoot(scope)}/catalogue/reorder-products?${query}`,
      request(scope, signal),
    ),
  );
  if (page.data.some((row) => row.outlet_id !== scope.outletId || row.status !== "active"))
    throw new Error("Reorder products do not match this active outlet selection.");
  if (
    new Set(page.data.map((row) => row.id)).size !== page.data.length ||
    page.page.has_more !== Boolean(page.page.next_cursor) ||
    (cursor !== null && page.page.next_cursor === cursor)
  )
    throw new Error("Reorder products have an invalid continuation.");
  return page;
}

export async function readReorderProduct(
  scope: PharmacyScope,
  product: string,
  signal?: AbortSignal,
) {
  const row = pharmacyReorderProductSchema.parse(
    await apiFetch(
      `${pharmacyRoot(scope)}/catalogue/reorder-products/${id.parse(product)}`,
      request(scope, signal),
    ),
  );
  if (row.id !== product || row.outlet_id !== scope.outletId)
    throw new Error("The product does not match this outlet.");
  return row;
}

export async function readLowStockOverview(scope: PharmacyScope, signal?: AbortSignal) {
  const row = pharmacyLowStockOverviewSchema.parse(
    await apiFetch(`${pharmacyRoot(scope)}/stock/low-products/overview`, request(scope, signal)),
  );
  if (row.outlet_id !== scope.outletId)
    throw new Error("The low-stock count does not match this outlet.");
  if (
    new Intl.DateTimeFormat("en-CA", { timeZone: row.time_zone }).format(
      new Date(row.observed_at),
    ) !== row.local_date
  )
    throw new Error("The low-stock observation date could not be verified.");
  return row;
}
function request(scope: PharmacyScope, signal?: AbortSignal): RequestInit {
  const timeout = AbortSignal.timeout(15000);
  return {
    headers: { "X-Helivanta-Expected-Tenant": id.parse(scope.tenantId) },
    signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
    cache: "no-store",
  };
}
function productRoot(scope: PharmacyScope, product: string) {
  return `${pharmacyRoot(scope)}/catalogue/products/${id.parse(product)}`;
}
function checkedReceipt(
  scope: PharmacyScope,
  product: string,
  command: string | null,
  body: unknown,
) {
  const receipt = pharmacyReorderLevelReceiptSchema.parse(body);
  if (
    receipt.outlet_id !== scope.outletId ||
    receipt.product_id !== product ||
    (command !== null && receipt.id !== command) ||
    receipt.version !== receipt.expected_version + 1
  )
    throw new Error("The reorder receipt does not match this outlet, product and command.");
  return receipt;
}
export async function readReorderLevel(
  scope: PharmacyScope,
  product: string,
  signal?: AbortSignal,
) {
  const body = z
    .object({ level: pharmacyReorderLevelReceiptSchema.nullable() })
    .parse(await apiFetch(`${productRoot(scope, product)}/reorder-level`, request(scope, signal)));
  return body.level === null ? null : checkedReceipt(scope, product, null, body.level);
}
export async function readReorderReceipt(scope: PharmacyScope, product: string, command: string) {
  return checkedReceipt(
    scope,
    product,
    command,
    await apiFetch(
      `${productRoot(scope, product)}/reorder-levels/${id.parse(command)}`,
      request(scope),
    ),
  );
}
export async function saveReorderLevel(
  scope: PharmacyScope,
  product: string,
  command: string,
  input: PharmacyReorderLevelInput,
) {
  return checkedReceipt(
    scope,
    product,
    command,
    await apiFetch(`${productRoot(scope, product)}/reorder-levels/${id.parse(command)}`, {
      ...request(scope),
      method: "PUT",
      body: JSON.stringify(pharmacyReorderLevelInputSchema.parse(input)),
    }),
  );
}
export async function fetchLowStockPage(
  scope: PharmacyScope,
  cursor: string | null,
  signal?: AbortSignal,
): Promise<Page<PharmacyLowStockProduct>> {
  const query = new URLSearchParams();
  if (cursor) query.set("cursor", cursor);
  const page = pageSchema.parse(
    await apiFetch(`${pharmacyRoot(scope)}/stock/low-products?${query}`, request(scope, signal)),
  );
  if (page.data.some((row) => row.outlet_id !== scope.outletId))
    throw new Error("Low-stock results do not match this outlet.");
  if (page.data.some((row) => BigInt(row.available_units) > BigInt(row.minimum_units)))
    throw new Error("Low-stock quantities are inconsistent. Refresh before using this result.");
  if (page.page.has_more !== Boolean(page.page.next_cursor))
    throw new Error("Low-stock results have an invalid continuation.");
  return page;
}
