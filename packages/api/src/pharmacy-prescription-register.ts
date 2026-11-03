import { apiFetchResponse } from "./client";
import { pharmacyRoot, type PharmacyScope } from "./pharmacy";

/** One supply on prescription in the register (#1062; Drugs Rules 1945, Rule 65(3)(1)). */
export type PharmacyPrescriptionRegisterEntry = {
  id: string;
  /** The invoice and line, from the invoice's gap-free series. */
  serial: string;
  supply_date: string;
  invoice_number: string;
  sale_id: string;
  line_number: number;
  regulatory_class: "G" | "H" | "H1";
  prescription_id: string;
  prescribed_on: string;
  prescriber_name: string;
  prescriber_registration: string;
  /** Null when the prescription did not record it (allowed for Schedule G). */
  prescriber_address: string | null;
  patient_name: string;
  patient_address: string | null;
  /** The registered patient the sale named, if any. */
  patient_id: string | null;
  product_id: string;
  sku: string;
  brand_name: string;
  generic_name: string;
  manufacturer: string;
  lot_number: string;
  expires_on: string;
  quantity_units: number;
  returned_units: number;
  pharmacist: string;
};

export type PharmacyPrescriptionRegisterFilter = {
  from: string;
  to: string;
  regulatoryClass?: "G" | "H" | "H1";
  productId?: string;
  patientId?: string;
};

function query(f: PharmacyPrescriptionRegisterFilter) {
  const q = new URLSearchParams({ from: f.from, to: f.to });
  if (f.regulatoryClass) q.set("class", f.regulatoryClass);
  if (f.productId) q.set("product_id", f.productId);
  if (f.patientId) q.set("patient_id", f.patientId);
  return q.toString();
}

export function pharmacyPrescriptionRegisterPath(
  scope: PharmacyScope,
  f: PharmacyPrescriptionRegisterFilter,
) {
  return `${pharmacyRoot(scope)}/registers/prescriptions?${query(f)}`;
}

/** The register for a filter as the server's audited CSV. */
export async function fetchPharmacyPrescriptionRegisterCSV(
  scope: PharmacyScope,
  f: PharmacyPrescriptionRegisterFilter,
  signal: AbortSignal,
): Promise<{ blob: Blob; filename: string }> {
  signal.throwIfAborted();
  const response = await apiFetchResponse(
    `${pharmacyRoot(scope)}/registers/prescriptions/export?${query(f)}`,
    {
      headers: { "X-Helivanta-Expected-Tenant": scope.tenantId },
      cache: "no-store",
      signal,
    },
  );
  const disposition = response.headers.get("Content-Disposition") ?? "";
  const filename = /^attachment; filename="(pharmacy-prescription-register-[a-z0-9-]+\.csv)"$/.exec(
    disposition,
  )?.[1];
  if (!filename || !/^text\/csv(?:;|$)/i.test(response.headers.get("Content-Type") ?? ""))
    throw new Error("The server did not return a valid register CSV. Try again.");
  const blob = await response.blob();
  signal.throwIfAborted();
  return { blob, filename };
}
