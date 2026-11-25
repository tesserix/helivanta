import { z } from "zod";
import { readAttachmentFile, sha256Hex } from "./attachment-file";
import { apiFetch, apiFetchResponse } from "./client";
import { pharmacyKey, pharmacyRoot } from "./pharmacy";
import { pharmacyActorHeaders, type PharmacyActorScope } from "./pharmacy-actor";
import {
  pharmacyTallyPurchaseExportPreviewSchema,
  pharmacyTallyLedgerMapEntryInputSchema,
  pharmacyTallyLedgerMapInputSchema,
  pharmacyTallyLedgerMapReceiptSchema,
} from "./pharmacy-contract";

export type PharmacyTallyPreview = z.infer<typeof pharmacyTallyPurchaseExportPreviewSchema>;
export type PharmacyTallyLedgerMapInput = z.infer<typeof pharmacyTallyLedgerMapInputSchema>;
export type PharmacyTallyLedgerMapReceipt = z.infer<typeof pharmacyTallyLedgerMapReceiptSchema>;
export type PharmacyTallyPeriod = { from: string; to: string };

// Tally matches ledger names exactly, so the server refuses rather than trims (ADR-0076 §2).
export const pharmacyTallyLedgerNameSchema =
  pharmacyTallyLedgerMapEntryInputSchema.shape.ledger_name
    .refine(
      (name) => name.trim() === name,
      "Remove the spaces before or after the name. Tally matches ledger names exactly.",
    )
    .refine(
      (name) => !/[\p{Cc}\p{Cs}\uFFFE\uFFFF]/u.test(name),
      "Remove control characters from the name.",
    );

const ledgerMapInput = pharmacyTallyLedgerMapInputSchema.extend({
  entries: z
    .array(
      pharmacyTallyLedgerMapEntryInputSchema.extend({ ledger_name: pharmacyTallyLedgerNameSchema }),
    )
    .min(1)
    .max(1000),
});
const id = z.string().guid();
const day = z.string().date();

function sameID(a: string | null, b: string | null) {
  return a === null || b === null ? a === b : a.toLowerCase() === b.toLowerCase();
}
function distinct(heads: { head: string }[]) {
  return new Set(heads.map((entry) => entry.head)).size === heads.length;
}
function root(scope: PharmacyActorScope) {
  return `${pharmacyRoot(scope)}/accounts/tally`;
}
function periodQuery(period: PharmacyTallyPeriod) {
  return new URLSearchParams({
    from: day.parse(period.from),
    to: day.parse(period.to),
    purchase_bills: "true",
  });
}
function read(scope: PharmacyActorScope, signal: AbortSignal): RequestInit {
  return {
    headers: {
      "X-Helivanta-Expected-Tenant": pharmacyActorHeaders(scope)["X-Helivanta-Expected-Tenant"],
    },
    cache: "no-store",
    signal,
  };
}

export function pharmacyTallyKey(scope: PharmacyActorScope, ...parts: unknown[]) {
  return pharmacyKey(scope, "tally-export", scope.subject, ...parts);
}

export async function readPharmacyTallyPreview(
  scope: PharmacyActorScope,
  period: PharmacyTallyPeriod,
  signal: AbortSignal,
): Promise<PharmacyTallyPreview> {
  const parsed = pharmacyTallyPurchaseExportPreviewSchema.safeParse(
    await apiFetch(`${root(scope)}/preview?${periodQuery(period)}`, read(scope, signal)),
  );
  if (
    !parsed.success ||
    !sameID(parsed.data.outlet_id, scope.outletId) ||
    parsed.data.from !== period.from ||
    parsed.data.to !== period.to ||
    !distinct(parsed.data.heads)
  )
    throw new Error("The Tally preview could not be verified. Reload and try again.");
  return parsed.data;
}

// The receipt is the complete merged map; it must hold every name this request set.
export async function savePharmacyTallyLedgerMap(
  scope: PharmacyActorScope,
  mapID: string,
  input: PharmacyTallyLedgerMapInput,
  signal: AbortSignal,
): Promise<PharmacyTallyLedgerMapReceipt> {
  const body = ledgerMapInput.parse(input);
  const parsed = pharmacyTallyLedgerMapReceiptSchema.safeParse(
    await apiFetch(`${root(scope)}/ledger-maps/${id.parse(mapID)}`, {
      headers: pharmacyActorHeaders(scope),
      cache: "no-store",
      signal,
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  const names = parsed.success
    ? new Map(parsed.data.entries.map((entry) => [entry.head, entry.ledger_name]))
    : null;
  if (
    !parsed.success ||
    !names ||
    !sameID(parsed.data.id, mapID) ||
    !sameID(parsed.data.outlet_id, scope.outletId) ||
    !sameID(parsed.data.replaces, body.replaces) ||
    parsed.data.recorded_by !== scope.subject ||
    names.size !== parsed.data.entries.length ||
    body.entries.some((entry) => names.get(entry.head) !== entry.ledger_name)
  )
    throw new Error("The ledger map receipt could not be verified. Reload before saving again.");
  return parsed.data;
}

// The file must be the one audited for this period and previewed map, byte for byte.
export async function fetchPharmacyTallyVouchers(
  scope: PharmacyActorScope,
  period: PharmacyTallyPeriod,
  ledgerMapID: string | null,
  signal: AbortSignal,
): Promise<{ blob: Blob; filename: string; vouchers: number }> {
  signal.throwIfAborted();
  const query = periodQuery(period);
  const map = ledgerMapID === null ? "" : id.parse(ledgerMapID).toLowerCase();
  query.set("ledger_map", map);
  const response = await apiFetchResponse(
    `${root(scope)}/vouchers.xml?${query}`,
    read(scope, signal),
  );
  const outlet = id.parse(scope.outletId).toLowerCase();
  const filename = `pharmacy-tally-vouchers-${outlet}-${period.from}-${period.to}.xml`;
  const vouchers = response.headers.get("X-Export-Voucher-Count") ?? "";
  const digest = response.headers.get("X-Export-Sha256") ?? "";
  if (
    response.headers.get("Content-Disposition") !== `attachment; filename="${filename}"` ||
    !/^application\/xml(?:;|$)/i.test(response.headers.get("Content-Type") ?? "") ||
    !/^(?:0|[1-9]\d{0,3})$/.test(vouchers) ||
    Number(vouchers) > 5000 ||
    response.headers.get("X-Export-Ledger-Map") !== map ||
    response.headers.get("X-Export-Purchase-Bills") !== "true" ||
    !/^[0-9a-f]{64}$/.test(digest)
  )
    throw new Error("The server did not return a valid Tally file. Try again.");
  const blob = await response.blob();
  const actual = await sha256Hex(await readAttachmentFile(blob, signal));
  signal.throwIfAborted();
  if (actual !== digest) throw new Error("The Tally file could not be verified. Try again.");
  return { blob, filename, vouchers: Number(vouchers) };
}
