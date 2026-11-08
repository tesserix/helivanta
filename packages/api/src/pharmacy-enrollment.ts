import { z } from "zod";
import { apiFetch } from "./client";
import {
  enrollmentRequestOptions,
  type EnrollmentInput,
  type EnrollmentRequestScope,
} from "./enrollment";
import {
  ENROLLMENT_REQUEST_CONTEXT_PROJECTION,
  enrollmentAddressSchema,
  enrollmentContactSchema,
  enrollmentCredentialSchema,
  enrollmentExperienceSchema,
  enrollmentProfileSchema,
  enrollmentQualificationSchema,
} from "./enrollment-fields";
import { pharmacyEmployerPath } from "./pharmacy-employers";
import { PharmacyStaffContract } from "./workforce-establishment-contract";

/** Strict, so a hospital_id or any HR masters marker is refused: a pharmacy has no masters (ADR-0063). */
export const pharmacyEnrollmentSchema = z
  .object({
    staff: PharmacyStaffContract,
    version: z.number().int().min(1),
    request_context_version: z.literal(ENROLLMENT_REQUEST_CONTEXT_PROJECTION.version),
    profile: enrollmentProfileSchema,
    addresses: z.array(enrollmentAddressSchema).max(2),
    emergency_contacts: z.array(enrollmentContactSchema).max(5),
    credentials: z.array(enrollmentCredentialSchema).max(20),
    qualifications: z.array(enrollmentQualificationSchema).max(20),
    experiences: z.array(enrollmentExperienceSchema).max(20),
  })
  .strict();
export type PharmacyEnrollment = z.infer<typeof pharmacyEnrollmentSchema>;

const staffId = z
  .string()
  .guid()
  .refine((value) => value !== "00000000-0000-0000-0000-000000000000")
  .transform((value) => value.toLowerCase());

function pharmacyEnrollmentPath(pharmacy: string, id: string): string {
  return `${pharmacyEmployerPath(pharmacy)}/staff/${staffId.parse(id)}/enrollment?${ENROLLMENT_REQUEST_CONTEXT_PROJECTION.query}=${ENROLLMENT_REQUEST_CONTEXT_PROJECTION.version}`;
}

function belongs(document: PharmacyEnrollment, pharmacy: string, id: string): boolean {
  return (
    document.staff.pharmacy_id.toLowerCase() === pharmacy.toLowerCase() &&
    document.staff.id.toLowerCase() === id.toLowerCase()
  );
}

export async function getPharmacyEnrollment(
  scope: EnrollmentRequestScope,
  pharmacy: string,
  id: string,
  signal?: AbortSignal,
): Promise<PharmacyEnrollment> {
  const document = pharmacyEnrollmentSchema.safeParse(
    await apiFetch(pharmacyEnrollmentPath(pharmacy, id), {
      ...enrollmentRequestOptions(scope),
      signal,
    }),
  );
  if (!document.success || !belongs(document.data, pharmacy, id))
    throw new Error("Could not verify this Staff enrollment in this pharmacy.");
  return document.data;
}

export async function savePharmacyEnrollment(
  scope: EnrollmentRequestScope,
  pharmacy: string,
  id: string,
  input: EnrollmentInput,
): Promise<PharmacyEnrollment> {
  const stored = pharmacyEnrollmentSchema.safeParse(
    await apiFetch(pharmacyEnrollmentPath(pharmacy, id), {
      ...enrollmentRequestOptions(scope),
      method: "PUT",
      body: JSON.stringify({
        ...input,
        request_context_version: ENROLLMENT_REQUEST_CONTEXT_PROJECTION.version,
      }),
    }),
  );
  if (
    !stored.success ||
    !belongs(stored.data, pharmacy, id) ||
    stored.data.version < input.expected_version ||
    (Object.keys(input.staff) as Array<keyof EnrollmentInput["staff"]>).some(
      (key) => stored.data.staff[key] !== input.staff[key],
    )
  )
    throw new Error(
      "Could not verify the saved enrollment. Your draft has been kept. Reload to check the current record.",
    );
  return stored.data;
}
