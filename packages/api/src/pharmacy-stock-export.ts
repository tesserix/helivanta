import { apiFetchResponse } from "./client";
import { pharmacyRoot, type PharmacyScope } from "./pharmacy";

export async function fetchPharmacyStockCSV(
  scope: PharmacyScope,
  filters: string,
  signal: AbortSignal,
): Promise<{ blob: Blob; filename: string }> {
  signal.throwIfAborted();
  const response = await apiFetchResponse(`${pharmacyRoot(scope)}/stock/positions.csv?${filters}`, {
    headers: { "X-Helivanta-Expected-Tenant": scope.tenantId },
    cache: "no-store",
    signal,
  });
  const disposition = response.headers.get("Content-Disposition") ?? "";
  const filename = /^attachment; filename="(pharmacy-stock-[a-z0-9-]+\.csv)"$/.exec(
    disposition,
  )?.[1];
  if (!filename || !/^text\/csv(?:;|$)/i.test(response.headers.get("Content-Type") ?? ""))
    throw new Error("The server did not return a valid stock CSV. Try again.");
  const blob = await response.blob();
  signal.throwIfAborted();
  return { blob, filename };
}
