import { z } from "zod";
import { apiFetch } from "./client";
import { enrollmentRequestOptions, type EnrollmentRequestScope } from "./enrollment";
import { pharmacyEmployerPath } from "./pharmacy-employers";
import {
  STAFF_DIRECTORY_CONTEXT,
  staffDirectoryReadMetadataContract,
} from "./staff-directory-contract";
import {
  PharmacyAccessContract,
  PharmacyStaffContract,
  PharmacyStaffSourceContract,
  type PharmacyAccess,
} from "./workforce-establishment-contract";
import type { StaffInput } from "./workspace";

export type {
  PharmacyStaff,
  PharmacyStaffSource,
  PharmacyAccess,
} from "./workforce-establishment-contract";

const cursorSchema = z.string().min(1).max(2048);
const pageShape = z
  .object({ has_more: z.boolean(), next_cursor: cursorSchema.nullable() })
  .strict();
const sourcesPage = z
  .object({ data: z.array(PharmacyStaffSourceContract).max(200), page: pageShape })
  .strict();
const staffPage = z
  .object({ data: z.array(PharmacyStaffContract).max(200), page: pageShape })
  .strict();
export const pharmacyStaffRecordSchema = PharmacyStaffContract.extend(
  staffDirectoryReadMetadataContract.shape,
).strict();
export type PharmacyStaffRecord = z.infer<typeof pharmacyStaffRecordSchema>;

function firstPageQuery(cursor: string | null): URLSearchParams {
  const params = new URLSearchParams({ limit: "50" });
  if (cursor !== null) params.set("cursor", cursorSchema.parse(cursor));
  return params;
}

function lower(value: string): string {
  return value.toLowerCase();
}

/** The pharmacies whose Staff directory the caller may read, each with the caller's access level. */
export async function listPharmacyStaffSourcesPage(
  scope: EnrollmentRequestScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const page = sourcesPage.parse(
    await apiFetch(`/workforce/pharmacies?${firstPageQuery(cursor)}`, {
      ...enrollmentRequestOptions(scope),
      signal,
    }),
  );
  if (
    new Set(page.data.map((row) => lower(row.pharmacy_id))).size !== page.data.length ||
    page.page.has_more !== Boolean(page.page.next_cursor)
  )
    throw new Error("The pharmacy Staff source list could not be continued safely.");
  return page;
}

export async function listPharmacyStaffPage(
  scope: EnrollmentRequestScope,
  pharmacy: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const root = pharmacyEmployerPath(pharmacy);
  const requested = lower(pharmacy);
  const page = staffPage.parse(
    await apiFetch(`${root}/staff?${firstPageQuery(cursor)}`, {
      ...enrollmentRequestOptions(scope),
      signal,
    }),
  );
  if (
    page.data.some((row) => lower(row.pharmacy_id) !== requested) ||
    new Set(page.data.map((row) => lower(row.id))).size !== page.data.length
  )
    throw new Error("The Staff source does not match the requested directory.");
  if (page.page.has_more !== Boolean(page.page.next_cursor))
    throw new Error("The Staff directory could not be continued safely.");
  return page;
}

export function pharmacyStaffRecordPath(pharmacy: string, id: string): string {
  return `${pharmacyEmployerPath(pharmacy)}/staff/${z.string().guid().parse(id).toLowerCase()}?${STAFF_DIRECTORY_CONTEXT.query}=${STAFF_DIRECTORY_CONTEXT.version}`;
}

function sameRecord(record: PharmacyStaffRecord, pharmacy: string, id: string): boolean {
  return lower(record.pharmacy_id) === lower(pharmacy) && lower(record.id) === lower(id);
}

export async function getPharmacyStaffRecord(
  scope: EnrollmentRequestScope,
  pharmacy: string,
  id: string,
  signal?: AbortSignal,
): Promise<PharmacyStaffRecord> {
  const record = pharmacyStaffRecordSchema.parse(
    await apiFetch(pharmacyStaffRecordPath(pharmacy, id), {
      ...enrollmentRequestOptions(scope),
      signal,
    }),
  );
  if (!sameRecord(record, pharmacy, id))
    throw new Error("Could not verify the current Staff record in this pharmacy.");
  return record;
}

export async function savePharmacyStaffRecord(
  scope: EnrollmentRequestScope,
  pharmacy: string,
  id: string,
  value: StaffInput,
  expectedVersion: number,
): Promise<PharmacyStaffRecord> {
  const stored = pharmacyStaffRecordSchema.safeParse(
    await apiFetch(pharmacyStaffRecordPath(pharmacy, id), {
      ...enrollmentRequestOptions(scope),
      method: "PUT",
      body: JSON.stringify({
        ...value,
        expected_version: expectedVersion,
        directory_context_version: STAFF_DIRECTORY_CONTEXT.version,
      }),
    }),
  );
  if (
    !stored.success ||
    !sameRecord(stored.data, pharmacy, id) ||
    stored.data.version < expectedVersion ||
    (Object.keys(value) as Array<keyof StaffInput>).some((key) => stored.data[key] !== value[key])
  )
    throw new Error(
      "Could not verify the saved Staff record. Your draft has been kept. Reload to check the current record.",
    );
  return stored.data;
}

/** Grants the signed-in registrant manager access; repeating it is harmless. */
export async function grantPharmacyManagerAccess(
  pharmacy: string,
  subject: string,
): Promise<PharmacyAccess> {
  const grant = PharmacyAccessContract.safeParse(
    await apiFetch(`${pharmacyEmployerPath(pharmacy)}/access/${encodeURIComponent(subject)}`, {
      method: "PUT",
      body: JSON.stringify({ level: "manager" }),
      cache: "no-store",
    }),
  );
  if (
    !grant.success ||
    lower(grant.data.pharmacy_id) !== lower(pharmacy) ||
    grant.data.subject !== subject ||
    grant.data.level !== "manager"
  )
    throw new Error("The Staff access grant could not be verified.");
  return grant.data;
}
