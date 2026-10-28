import { apiFetchResponse } from "./client";
import { pharmacyRoot, type PharmacyScope } from "./pharmacy";

/** One Schedule H1 supply in the register (#1386 C-2; Drugs Rules 1945, Rule 65(3)(h)). */
export type PharmacyH1Entry = {
  id: string;
  supply_date: string;
  invoice_number: string;
  sale_id: string;
  line_number: number;
  prescription_id: string;
  prescribed_on: string;
  prescriber_name: string;
  prescriber_registration: string;
  prescriber_address: string;
  patient_name: string;
  patient_address: string;
  product_id: string;
  sku: string;
  brand_name: string;
  generic_name: string;
  lot_number: string;
  expires_on: string;
  quantity_units: number;
  returned_units: number;
  pharmacist: string;
};

export function pharmacyH1RegisterPath(scope: PharmacyScope, from: string, to: string) {
  return `${pharmacyRoot(scope)}/registers/h1?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`;
}

/** The register for a period as the server's audited CSV. */
export async function fetchPharmacyH1RegisterCSV(
  scope: PharmacyScope,
  from: string,
  to: string,
  signal: AbortSignal,
): Promise<{ blob: Blob; filename: string }> {
  signal.throwIfAborted();
  const response = await apiFetchResponse(
    `${pharmacyRoot(scope)}/registers/h1/export?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`,
    {
      headers: { "X-Helivanta-Expected-Tenant": scope.tenantId },
      cache: "no-store",
      signal,
    },
  );
  const disposition = response.headers.get("Content-Disposition") ?? "";
  const filename = /^attachment; filename="(pharmacy-h1-register-[a-z0-9-]+\.csv)"$/.exec(
    disposition,
  )?.[1];
  if (!filename || !/^text\/csv(?:;|$)/i.test(response.headers.get("Content-Type") ?? ""))
    throw new Error("The server did not return a valid register CSV. Try again.");
  const blob = await response.blob();
  signal.throwIfAborted();
  return { blob, filename };
}
