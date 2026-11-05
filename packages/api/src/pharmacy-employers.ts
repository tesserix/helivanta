import { z } from "zod";
import { ApiError, apiFetch } from "./client";
import {
  PharmacyEmployerContract,
  type PharmacyEmployer,
} from "./workforce-establishment-contract";

export type { PharmacyEmployer } from "./workforce-establishment-contract";

const pharmacyId = z
  .string()
  .regex(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i)
  .transform((value) => value.toLowerCase());

export function pharmacyEmployerPath(pharmacy: string): string {
  return `/workforce/pharmacies/${pharmacyId.parse(pharmacy)}`;
}

export function pharmacyEmployerKey(tenantId: string, pharmacy: string): unknown[] {
  return ["workforce", tenantId, "pharmacy-employer", pharmacy];
}

function verify(raw: unknown, pharmacy: string): PharmacyEmployer {
  const employer = PharmacyEmployerContract.parse(raw);
  if (employer.pharmacy_id.toLowerCase() !== pharmacyId.parse(pharmacy))
    throw new Error("The employer registration could not be verified.");
  return employer;
}

/** The Workforce employer registration, or null when the pharmacy is not registered. */
export async function getPharmacyEmployer(
  pharmacy: string,
  signal?: AbortSignal,
): Promise<PharmacyEmployer | null> {
  try {
    return verify(
      await apiFetch(pharmacyEmployerPath(pharmacy), { cache: "no-store", signal }),
      pharmacy,
    );
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) return null;
    throw error;
  }
}

/** Registers a standalone pharmacy as an employer; name and ownership come from Pharma. */
export async function registerPharmacyEmployer(
  pharmacy: string,
  signal?: AbortSignal,
): Promise<PharmacyEmployer> {
  return verify(
    await apiFetch(pharmacyEmployerPath(pharmacy), {
      method: "PUT",
      body: "{}",
      cache: "no-store",
      signal,
    }),
    pharmacy,
  );
}
