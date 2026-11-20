import { apiFetchResponse } from "./client";
import { sha256Hex } from "./attachment-file";
import { pharmacyRoot, type PharmacyScope } from "./pharmacy";
import type { PharmacyStocktakeSummary } from "./pharmacy-stocktakes";

const byteLimit = 8 * 1024 * 1024;

export async function fetchStocktakeCSV(
  scope: PharmacyScope,
  take: Pick<PharmacyStocktakeSummary, "id" | "number" | "version" | "snapshot_rows">,
  signal: AbortSignal,
): Promise<{ blob: Blob; filename: string }> {
  const active = AbortSignal.any([signal, AbortSignal.timeout(15000)]);
  active.throwIfAborted();
  const response = await apiFetchResponse(
    `${pharmacyRoot(scope)}/stock/stocktakes/${encodeURIComponent(take.id)}/register.csv`,
    {
      headers: { "X-Helivanta-Expected-Tenant": scope.tenantId },
      cache: "no-store",
      signal: active,
    },
  );
  const filename = `stocktake-${take.number}-register.csv`;
  const headers = response.headers;
  const digest = headers.get("X-Export-Sha256") ?? "";
  const cache = headers
    .get("Cache-Control")
    ?.split(",")
    .map((part) => part.trim().toLowerCase());
  if (
    headers.get("X-Stocktake-Tenant-Id") !== scope.tenantId ||
    headers.get("X-Stocktake-Outlet-Id") !== scope.outletId ||
    headers.get("X-Stocktake-Id") !== take.id ||
    headers.get("X-Stocktake-Version") !== String(take.version) ||
    headers.get("X-Export-Row-Count") !== String(take.snapshot_rows) ||
    !/^[a-f0-9]{64}$/.test(digest) ||
    headers.get("Content-Disposition") !== `attachment; filename="${filename}"` ||
    !/^text\/csv(?:;|$)/i.test(headers.get("Content-Type") ?? "") ||
    !cache?.includes("private") ||
    !cache.includes("no-store") ||
    Number(headers.get("Content-Length")) > byteLimit ||
    !response.body
  ) {
    await response.body?.cancel();
    throw new Error(
      "The stocktake register does not match the displayed record. Refresh and try again.",
    );
  }
  const reader = response.body.getReader();
  const chunks: Uint8Array<ArrayBuffer>[] = [];
  let length = 0;
  let complete = false;
  const abort = () => {
    void reader.cancel().catch(() => undefined);
  };
  active.addEventListener("abort", abort, { once: true });
  try {
    active.throwIfAborted();
    while (true) {
      const { done, value } = await reader.read();
      active.throwIfAborted();
      if (done) {
        complete = true;
        break;
      }
      length += value.byteLength;
      if (length > byteLimit)
        throw new Error("The stocktake register exceeds the 8 MiB file limit.");
      chunks.push(new Uint8Array(value));
    }
  } finally {
    active.removeEventListener("abort", abort);
    if (!complete) await reader.cancel();
    reader.releaseLock();
  }
  const bytes = new Uint8Array(length);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  if ((await sha256Hex(bytes)) !== digest)
    throw new Error("The stocktake register file failed its integrity check. Try again.");
  active.throwIfAborted();
  return { blob: new Blob([bytes], { type: "text/csv;charset=utf-8" }), filename };
}
