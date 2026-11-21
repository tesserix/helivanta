import { z } from "zod";
import { pharmacyActorHeaders, type PharmacyActorScope } from "./pharmacy-actor";
import type { PharmacySupplierReturn } from "./pharmacy-supplier-returns";
import { pharmacyRoot } from "./pharmacy";
import { apiFetchResponse } from "./client";
import { verifiedReviewEvidenceBlob } from "./review-evidence-content";

export async function fetchPharmacySupplierReturnNote(
  scope: PharmacyActorScope,
  note: Pick<PharmacySupplierReturn, "id" | "number" | "state" | "lines">,
  signal: AbortSignal,
): Promise<{ blob: Blob; filename: string }> {
  const active = AbortSignal.any([signal, AbortSignal.timeout(15000)]);
  active.throwIfAborted();
  const id = z.string().guid().parse(note.id);
  if (
    note.state !== "dispatched" ||
    !Number.isSafeInteger(note.number) ||
    note.number < 1 ||
    note.number > 1000000000000 ||
    note.lines.length < 1 ||
    note.lines.length > 100
  )
    throw new Error("Choose a complete dispatched return before downloading its note.");
  const response = await apiFetchResponse(
    `${pharmacyRoot(scope)}/accounts/supplier-returns/${id}/note.html`,
    {
      headers: pharmacyActorHeaders(scope),
      cache: "no-store",
      signal: active,
    },
  );
  const headers = response.headers;
  const filename = `supplier-return-${note.number}.html`;
  const digest = headers.get("X-Export-Sha256") ?? "";
  const byteCount = headers.get("X-Export-Byte-Count") ?? "";
  const size = Number(byteCount);
  const cache = headers
    .get("Cache-Control")
    ?.split(",")
    .map((part) => part.trim().toLowerCase());
  const policy = headers.get("Content-Security-Policy") ?? "";
  if (
    headers.get("X-Supplier-Return-Tenant-Id")?.toLowerCase() !== scope.tenantId.toLowerCase() ||
    headers.get("X-Supplier-Return-Outlet-Id")?.toLowerCase() !== scope.outletId.toLowerCase() ||
    headers.get("X-Supplier-Return-Id")?.toLowerCase() !== id.toLowerCase() ||
    headers.get("X-Supplier-Return-Number") !== String(note.number) ||
    headers.get("X-Export-Row-Count") !== String(note.lines.length) ||
    headers.get("Content-Disposition") !== `attachment; filename="${filename}"` ||
    !/^text\/html(?:;\s*charset=utf-8)?$/i.test(headers.get("Content-Type") ?? "") ||
    !cache?.includes("private") ||
    !cache.includes("no-store") ||
    !/(?:^|;)\s*sandbox\s*(?:;|$)/.test(policy) ||
    !policy.includes("default-src 'none'") ||
    !/^[a-f0-9]{64}$/.test(digest) ||
    !/^[1-9]\d*$/.test(byteCount) ||
    !Number.isSafeInteger(size) ||
    size > 1024 * 1024
  ) {
    await response.body?.cancel();
    throw new Error(
      "The commercial note does not match the displayed return. No file was released.",
    );
  }
  const blob = await verifiedReviewEvidenceBlob(
    response,
    { content_type: "text/html", size, sha256: digest },
    active,
  );
  active.throwIfAborted();
  return { blob, filename };
}
