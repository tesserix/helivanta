import { usePagedQuery } from "./paged";
import { z } from "zod";
import { apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import {
  pharmacyRoot,
  pharmacyPayableFormSchema,
  type PharmacyPayableInput,
  type PharmacyScope,
} from "./pharmacy";
import {
  AcquisitionReceiptContract,
  PayableReceiptMetadataContract,
} from "./pharmacy-accounts-contract";
export type { AcquisitionReceipt, AcquisitionSupplier } from "./pharmacy-accounts-contract";
export type AcquisitionScope = PharmacyScope & { subject: string };
const cursor = z.string().min(1).max(2048);
const response = z
  .object({
    data: z.array(AcquisitionReceiptContract).max(50),
    page: z.object({ has_more: z.boolean(), next_cursor: cursor.nullable() }).strict(),
  })
  .strict();
export async function listAcquisitionReceipts(
  scope: AcquisitionScope,
  after: string | null = null,
  signal?: AbortSignal,
) {
  const params = new URLSearchParams({ limit: "50" });
  if (after !== null) params.set("cursor", cursor.parse(after));
  const page = response.parse(
    await apiFetch(`${pharmacyRoot(scope)}/accounts/acquisition-receipts?${params}`, {
      ...membershipRequestOptions(scope),
      signal,
    }),
  );
  if (
    page.page.has_more !== Boolean(page.page.next_cursor) ||
    new Set(page.data.map((row) => row.id)).size !== page.data.length
  )
    throw new Error("The acquisition receipt page could not be continued safely.");
  for (const row of page.data) {
    if (
      row.owner_tenant_id !== scope.tenantId ||
      row.pharmacy_id !== scope.pharmacyId ||
      row.outlet_id !== scope.outletId ||
      (row.supplier &&
        (row.supplier.original_accepted_units !== row.quantity_units ||
          (row.supplier.receipt_version === 1 &&
            row.supplier.effective_accepted_units !== row.quantity_units)))
    )
      throw new Error(
        "The acquisition receipt does not match this outlet or its retained quantity evidence.",
      );
  }
  return page;
}
export function useAcquisitionReceipts(scope: AcquisitionScope | null) {
  const result = usePagedQuery(
    [
      "pharmacy",
      "acquisition-receipts",
      scope?.tenantId,
      scope?.subject,
      scope?.pharmacyId,
      scope?.outletId,
    ],
    (cursor, signal) => {
      if (!scope) throw new Error("Current financial account is unavailable.");
      return listAcquisitionReceipts(scope, cursor, signal);
    },
    { enabled: scope !== null, gcTime: 0, retry: false },
  );
  const duplicate = new Set(result.items.map((row) => row.id)).size !== result.items.length;
  const error = duplicate
    ? new Error("Receipt pages changed. Reload before choosing a receipt.")
    : result.error;
  return {
    ...result,
    items: error ? [] : result.items,
    isError: result.isError || duplicate,
    error,
  };
}

const payableReceipt = pharmacyPayableFormSchema.safeExtend(PayableReceiptMetadataContract.shape);
function billPath(scope: AcquisitionScope, bill: string) {
  const id = z.uuidv7().parse(bill);
  return `${pharmacyRoot(scope)}/accounts/payables/${id}`;
}
function canonicalBill(value: PharmacyPayableInput) {
  const parsed = pharmacyPayableFormSchema.parse(value);
  return {
    ...parsed,
    lines: parsed.lines.map((line) => ({ ...line, receipt_id: line.receipt_id ?? null })),
  };
}
function checkedBill(
  scope: AcquisitionScope,
  bill: string,
  input: PharmacyPayableInput,
  value: unknown,
) {
  const row = payableReceipt.parse(value);
  const original = canonicalBill(input);
  const fields = Object.fromEntries(
    Object.keys(original).map((key) => [key, row[key as keyof typeof row]]),
  );
  const expectedTotal = original.lines.reduce(
    (sum, line) => sum + line.net_minor + line.tax_minor + line.landed_minor,
    0,
  );
  if (
    row.id !== bill ||
    row.outlet_id !== scope.outletId ||
    row.actor_subject !== scope.subject ||
    JSON.stringify(canonicalBill(fields as PharmacyPayableInput)) !== JSON.stringify(original) ||
    row.line_count !== original.lines.length ||
    row.total_minor !== expectedTotal ||
    row.outstanding_minor !== row.total_minor - row.paid_minor - row.credit_minor
  )
    throw new Error(
      "The original bill could not be verified. Keep the submitted reference and reconcile before recording another bill.",
    );
  return row;
}
export async function recordAcquisitionBill(
  scope: AcquisitionScope,
  bill: string,
  input: PharmacyPayableInput,
) {
  const normalized = canonicalBill(input);
  const value = await apiFetch(billPath(scope, bill), {
    ...membershipRequestOptions(scope),
    method: "PUT",
    body: JSON.stringify(normalized),
  });
  return checkedBill(scope, bill, normalized, value);
}
export async function recoverAcquisitionBill(
  scope: AcquisitionScope,
  bill: string,
  input: PharmacyPayableInput,
) {
  return checkedBill(
    scope,
    bill,
    input,
    await apiFetch(billPath(scope, bill), membershipRequestOptions(scope)),
  );
}
