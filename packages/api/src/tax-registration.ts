import type { TaxScheme } from "./country-profiles";

// Mirrors backend/pkg/tax/gstin.go so the form refuses what the server
// would. A pass proves the identifier is well formed, not registered.
const GSTIN_SHAPE = /^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$/;
const BASE36 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ";

export type TaxRegistrationError = "format" | "checksum" | "jurisdiction";

export function normalizeTaxRegistration(identifier: string): string {
  return identifier.trim().toUpperCase();
}

function gstinCheck(body: string): string {
  let sum = 0;
  for (let i = 0; i < body.length; i++) {
    const v = BASE36.indexOf(body[i]!) * (1 + (i % 2));
    sum += Math.floor(v / 36) + (v % 36);
  }
  return BASE36[(36 - (sum % 36)) % 36]!;
}

export function validateTaxRegistration(
  scheme: TaxScheme,
  jurisdiction: string,
  identifier: string,
): TaxRegistrationError | null {
  const id = normalizeTaxRegistration(identifier);
  if (!GSTIN_SHAPE.test(id)) return "format";
  if (id[14] !== gstinCheck(id.slice(0, 14))) return "checksum";
  if (!scheme.jurisdictions.some((j) => j.code === jurisdiction) || id.slice(0, 2) !== jurisdiction)
    return "jurisdiction";
  return null;
}
