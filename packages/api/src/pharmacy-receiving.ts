import { z } from "zod";
import { useInfiniteQuery } from "@tanstack/react-query";
import { ApiError, apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import type { StaffAssignmentScope } from "./staff-assignments";
import {
  pharmacyBatchInputSchema,
  pharmacyIngredientSchema,
  pharmacyMovementInputSchema,
  pharmacyStorageAddressSchema,
} from "./pharmacy-contract";

/** One reviewed receiving duty at one exact outlet (ADR-0064); the API, not this scope, admits each request. */
export type PharmacyReceivingScope = StaffAssignmentScope & {
  pharmacyId: string;
  outletId: string;
};
export { v7 as newPharmacyReceivingId } from "uuid";

const id = z
  .string()
  .regex(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
const instant = z.string().datetime({ offset: true });
export const isPharmacyReceivingTarget = (pharmacyId: string, outletId: string) =>
  id.safeParse(pharmacyId).success && id.safeParse(outletId).success;

export const pharmacyReceivingKey = (scope: PharmacyReceivingScope, ...parts: string[]) => [
  "pharmacy-receiving",
  scope.tenantId,
  scope.subject,
  scope.pharmacyId,
  scope.outletId,
  ...parts,
];
const root = (scope: PharmacyReceivingScope) =>
  `/pharmacy/pharmacies/${id.parse(scope.pharmacyId)}/operating-locations/${id.parse(scope.outletId)}/stock`;

export const PharmacyReceivingProductContract = z
  .object({
    id,
    outlet_id: id,
    sku: z.string().min(1).max(40),
    brand_name: z.string().min(1).max(200),
    generic_name: z.string().min(1).max(200),
    dosage_form: z.string().min(1).max(80),
    ingredients: z.array(pharmacyIngredientSchema).min(1).max(20),
    pack_size: z.number().int().min(1),
    unit_of_issue: z.string().min(1).max(40),
    currency: z.string().regex(/^[A-Z]{3}$/),
    version: z.number().int().min(1),
    created_at: instant,
  })
  .strict();
export const PharmacyReceivingLocationContract = z
  .object({
    id,
    outlet_id: id,
    code: z.string().min(1).max(40),
    name: z.string().min(1).max(200),
    storage_address: pharmacyStorageAddressSchema,
    stock_status: z.enum(["quarantine", "sellable"]),
    created_at: instant,
  })
  .strict();
export const PharmacyReceivingBatchContract = pharmacyBatchInputSchema
  .extend({
    id,
    outlet_id: id,
    manufactured_on: z.string().date().nullable(),
    purchase_cost_minor: z.number().int().min(0).nullable(),
    product_version: z.number().int().min(1),
    created_at: instant,
  })
  .strict();
export const PharmacyReceiptInputContract = pharmacyMovementInputSchema
  .extend({ kind: z.literal("receipt"), quantity_units: z.number().int().min(1).max(1000000000) })
  .strict();
export const PharmacyReceiptContract = PharmacyReceiptInputContract.extend({
  id,
  outlet_id: id,
  actor_subject: z.string().min(1).max(200),
  created_at: instant,
}).strict();
export type PharmacyReceivingProduct = z.infer<typeof PharmacyReceivingProductContract>;
export type PharmacyReceivingLocation = z.infer<typeof PharmacyReceivingLocationContract>;
export type PharmacyReceivingBatch = z.infer<typeof PharmacyReceivingBatchContract>;
export type PharmacyReceivingReceipt = z.infer<typeof PharmacyReceiptContract>;
export type PharmacyReceivingBatchInput = z.input<typeof pharmacyBatchInputSchema>;
export type PharmacyReceiptInput = z.input<typeof PharmacyReceiptInputContract>;

/** True when the duty no longer admits this caller here; only reopening the duty can recover. */
export function isReceivingAccessChanged(error: unknown) {
  return error instanceof ApiError && (error.status === 404 || error.code === "session_changed");
}

function atOutlet<T extends { id: string; outlet_id: string }>(
  scope: PharmacyReceivingScope,
  row: T,
  expected: string,
) {
  if (row.outlet_id !== scope.outletId || row.id !== expected)
    throw new Error("The stock record belongs to another outlet.");
  return row;
}

function pageOf<T extends { id: string; outlet_id: string }>(row: z.ZodType<T>) {
  return z
    .object({
      data: z.array(row).max(100),
      page: z
        .object({ has_more: z.boolean(), next_cursor: z.string().min(1).max(4096).nullable() })
        .strict(),
    })
    .strict()
    .superRefine((value, ctx) => {
      if (
        value.page.has_more !== (value.page.next_cursor !== null) ||
        (value.page.has_more && value.data.length === 0) ||
        new Set(value.data.map((entry) => entry.id)).size !== value.data.length
      )
        ctx.addIssue({ code: "custom", message: "The stock page is inconsistent." });
    });
}

function useReceivingPages<T extends { id: string; outlet_id: string }>(
  scope: PharmacyReceivingScope,
  key: string[],
  path: string,
  filter: Record<string, string>,
  row: z.ZodType<T>,
  enabled = true,
) {
  const page = pageOf(row);
  return useInfiniteQuery({
    queryKey: pharmacyReceivingKey(scope, ...key),
    enabled,
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: true,
    initialPageParam: null as string | null,
    queryFn: async ({ pageParam, signal }) => {
      const params = new URLSearchParams({ ...filter, limit: "50" });
      if (pageParam) params.set("cursor", pageParam);
      const result = page.parse(
        await apiFetch(`${root(scope)}${path}?${params}`, {
          ...membershipRequestOptions(scope),
          signal,
        }),
      );
      return { ...result, data: result.data.map((entry) => atOutlet(scope, entry, entry.id)) };
    },
    getNextPageParam: (last) => (last.page.has_more ? last.page.next_cursor : undefined),
  });
}

export const usePharmacyReceivingProducts = (scope: PharmacyReceivingScope) =>
  useReceivingPages(scope, ["products"], "/products", {}, PharmacyReceivingProductContract);
export const usePharmacyReceivingLocations = (scope: PharmacyReceivingScope) =>
  useReceivingPages(scope, ["locations"], "/locations", {}, PharmacyReceivingLocationContract);
export const usePharmacyReceipts = (scope: PharmacyReceivingScope) =>
  useReceivingPages(scope, ["receipts"], "/receipts", {}, PharmacyReceiptContract);
export const usePharmacyReceivingBatches = (scope: PharmacyReceivingScope, productId: string) =>
  useReceivingPages(
    scope,
    ["batches", productId],
    "/batches",
    productId ? { product_id: id.parse(productId) } : {},
    PharmacyReceivingBatchContract,
    productId !== "",
  );

function exactBatch(input: PharmacyReceivingBatchInput, batch: PharmacyReceivingBatch) {
  const body = pharmacyBatchInputSchema.parse(input);
  if (
    batch.product_id !== body.product_id ||
    batch.lot_number !== body.lot_number ||
    batch.expires_on !== body.expires_on ||
    batch.mrp_minor !== body.mrp_minor ||
    batch.manufactured_on !== (body.manufactured_on ?? null) ||
    batch.purchase_cost_minor !== (body.purchase_cost_minor ?? null)
  )
    throw new Error("The recorded lot does not match the label that was entered.");
  return batch;
}
function exactReceipt(
  scope: PharmacyReceivingScope,
  input: PharmacyReceiptInput,
  receipt: PharmacyReceivingReceipt,
) {
  const body = PharmacyReceiptInputContract.parse(input);
  if (
    receipt.actor_subject !== scope.subject ||
    (Object.keys(body) as (keyof typeof body)[]).some((key) => receipt[key] !== body[key])
  )
    throw new Error("The recorded receipt does not match the count that was entered.");
  return receipt;
}

async function readOne<T extends { id: string; outlet_id: string }>(
  scope: PharmacyReceivingScope,
  path: string,
  recordId: string,
  row: z.ZodType<T>,
  init: RequestInit = {},
) {
  const result = await apiFetch(`${root(scope)}${path}/${id.parse(recordId)}`, {
    ...membershipRequestOptions(scope),
    ...init,
  });
  return atOutlet(scope, row.parse(result), recordId);
}

/** Records or replays one lot; the stored lot must equal what was sent. */
export async function recordPharmacyReceivingBatch(
  scope: PharmacyReceivingScope,
  batchId: string,
  input: PharmacyReceivingBatchInput,
) {
  const body = pharmacyBatchInputSchema.parse(input);
  return exactBatch(
    body,
    await readOne(scope, "/batches", batchId, PharmacyReceivingBatchContract, {
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}
/** Confirms a lot whose PUT outcome was lost, without writing again. */
export async function recoverPharmacyReceivingBatch(
  scope: PharmacyReceivingScope,
  batchId: string,
  input: PharmacyReceivingBatchInput,
) {
  return exactBatch(
    input,
    await readOne(scope, "/batches", batchId, PharmacyReceivingBatchContract),
  );
}

/** Records or replays one quarantined receipt; the stored count and account must equal what was sent. */
export async function recordPharmacyReceipt(
  scope: PharmacyReceivingScope,
  receiptId: string,
  input: PharmacyReceiptInput,
) {
  const body = PharmacyReceiptInputContract.parse(input);
  return exactReceipt(
    scope,
    body,
    await readOne(scope, "/receipts", receiptId, PharmacyReceiptContract, {
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}
/** Confirms a receipt whose PUT outcome was lost, without writing again. */
export async function recoverPharmacyReceipt(
  scope: PharmacyReceivingScope,
  receiptId: string,
  input: PharmacyReceiptInput,
) {
  return exactReceipt(
    scope,
    input,
    await readOne(scope, "/receipts", receiptId, PharmacyReceiptContract),
  );
}
