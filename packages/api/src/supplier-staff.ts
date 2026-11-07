import { z } from "zod";
import { apiFetch } from "./client";
import { enrollmentRequestOptions, type EnrollmentRequestScope } from "./enrollment";
import {
  STAFF_DIRECTORY_CONTEXT,
  staffDirectoryReadMetadataContract,
} from "./staff-directory-contract";
import {
  SupplierEmployerContract,
  SupplierStaffContract,
  SupplierStaffSourceContract,
  type SupplierEmployer,
} from "./workforce-establishment-contract";
import type { StaffInput } from "./workspace";

export type {
  SupplierEmployer,
  SupplierStaff,
  SupplierStaffSource,
  SupplierAccess,
} from "./workforce-establishment-contract";

const supplierEmployerPath = (supplier: string) =>
  `/workforce/suppliers/${z
    .string()
    .regex(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    .parse(supplier)}`;

const cursorSchema = z.string().min(1).max(2048);
const pageShape = z
  .object({ has_more: z.boolean(), next_cursor: cursorSchema.nullable() })
  .strict();
const sourcesPage = z
  .object({ data: z.array(SupplierStaffSourceContract).max(200), page: pageShape })
  .strict();
const staffPage = z
  .object({ data: z.array(SupplierStaffContract).max(200), page: pageShape })
  .strict();
export const supplierStaffRecordSchema = SupplierStaffContract.extend(
  staffDirectoryReadMetadataContract.shape,
).strict();
export type SupplierStaffRecord = z.infer<typeof supplierStaffRecordSchema>;

function firstPageQuery(cursor: string | null): URLSearchParams {
  const params = new URLSearchParams({ limit: "50" });
  if (cursor !== null) params.set("cursor", cursorSchema.parse(cursor));
  return params;
}

function lower(value: string): string {
  return value.toLowerCase();
}

/** The suppliers whose Staff directory the caller may read, each with the caller's access level. */
export async function listSupplierStaffSourcesPage(
  scope: EnrollmentRequestScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const page = sourcesPage.parse(
    await apiFetch(`/workforce/suppliers?${firstPageQuery(cursor)}`, {
      ...enrollmentRequestOptions(scope),
      signal,
    }),
  );
  if (
    new Set(page.data.map((row) => lower(row.supplier_id))).size !== page.data.length ||
    page.page.has_more !== Boolean(page.page.next_cursor)
  )
    throw new Error("The supplier Staff source list could not be continued safely.");
  return page;
}

export async function listSupplierStaffPage(
  scope: EnrollmentRequestScope,
  supplier: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const root = supplierEmployerPath(supplier);
  const requested = lower(supplier);
  const page = staffPage.parse(
    await apiFetch(`${root}/staff?${firstPageQuery(cursor)}`, {
      ...enrollmentRequestOptions(scope),
      signal,
    }),
  );
  if (
    page.data.some((row) => lower(row.supplier_id) !== requested) ||
    new Set(page.data.map((row) => lower(row.id))).size !== page.data.length
  )
    throw new Error("The Staff source does not match the requested directory.");
  if (page.page.has_more !== Boolean(page.page.next_cursor))
    throw new Error("The Staff directory could not be continued safely.");
  return page;
}

export function supplierStaffRecordPath(supplier: string, id: string): string {
  return `${supplierEmployerPath(supplier)}/staff/${z.string().guid().parse(id).toLowerCase()}?${STAFF_DIRECTORY_CONTEXT.query}=${STAFF_DIRECTORY_CONTEXT.version}`;
}

function sameRecord(record: SupplierStaffRecord, supplier: string, id: string): boolean {
  return lower(record.supplier_id) === lower(supplier) && lower(record.id) === lower(id);
}

export async function getSupplierStaffRecord(
  scope: EnrollmentRequestScope,
  supplier: string,
  id: string,
  signal?: AbortSignal,
): Promise<SupplierStaffRecord> {
  const record = supplierStaffRecordSchema.parse(
    await apiFetch(supplierStaffRecordPath(supplier, id), {
      ...enrollmentRequestOptions(scope),
      signal,
    }),
  );
  if (!sameRecord(record, supplier, id))
    throw new Error("Could not verify the current Staff record in this supplier.");
  return record;
}

export async function saveSupplierStaffRecord(
  scope: EnrollmentRequestScope,
  supplier: string,
  id: string,
  value: StaffInput,
  expectedVersion: number,
): Promise<SupplierStaffRecord> {
  const stored = supplierStaffRecordSchema.safeParse(
    await apiFetch(supplierStaffRecordPath(supplier, id), {
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
    !sameRecord(stored.data, supplier, id) ||
    stored.data.version < expectedVersion ||
    (Object.keys(value) as Array<keyof StaffInput>).some((key) => stored.data[key] !== value[key])
  )
    throw new Error(
      "Could not verify the saved Staff record. Your draft has been kept. Reload to check the current record.",
    );
  return stored.data;
}

export async function registerSupplierEmployer(
  scope: EnrollmentRequestScope,
  supplier: string,
): Promise<SupplierEmployer> {
  const result = SupplierEmployerContract.parse(
    await apiFetch(supplierEmployerPath(supplier), {
      ...enrollmentRequestOptions(scope),
      method: "PUT",
      body: "{}",
    }),
  );
  if (result.supplier_id !== supplier)
    throw new Error("The supplier employer registration could not be verified.");
  return result;
}

export async function getSupplierEmployer(
  scope: EnrollmentRequestScope,
  supplier: string,
  signal?: AbortSignal,
): Promise<SupplierEmployer> {
  const result = SupplierEmployerContract.parse(
    await apiFetch(supplierEmployerPath(supplier), { ...enrollmentRequestOptions(scope), signal }),
  );
  if (result.supplier_id !== supplier)
    throw new Error("The supplier employer registration could not be verified.");
  return result;
}
