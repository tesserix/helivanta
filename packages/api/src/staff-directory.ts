import { z } from "zod";
import { staffSchema } from "./workspace";
import {
  staffDirectoryReadMetadataContract,
  hrIntakeScopeContract,
} from "./staff-directory-contract";
import { apiFetch } from "./client";
import {
  enrollmentRequestOptions,
  withStaffIntakeScope,
  type EnrollmentRequestScope,
} from "./enrollment";

export const staffDirectoryViewSchema = staffSchema
  .extend({
    id: z.string().guid(),
    hospital_id: z.string().guid(),
    created_at: z.string().datetime({ offset: true }),
    updated_at: z.string().datetime({ offset: true }),
    ...staffDirectoryReadMetadataContract.shape,
  })
  .strict();
export type StaffDirectoryView = z.infer<typeof staffDirectoryViewSchema>;

const intakePage = z
  .object({
    data: z.array(hrIntakeScopeContract).max(20),
    page: z
      .object({ has_more: z.boolean(), next_cursor: z.string().min(1).max(2048).nullable() })
      .strict(),
  })
  .strict();
export async function listStaffIntakeScopes(
  scope: EnrollmentRequestScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ limit: "20" });
  if (cursor !== null) query.set("cursor", z.string().min(1).max(2048).parse(cursor));
  const value = intakePage.parse(
    await apiFetch(`/workforce/hr-intake-scopes?${query}`, {
      ...enrollmentRequestOptions(scope),
      signal,
    }),
  );
  if (
    value.data.some(
      (capabilities) => !capabilities.can_prepare_staff || !capabilities.can_manage_staff_documents,
    )
  )
    throw new Error("The Staff onboarding capabilities could not be verified.");
  if (value.page.has_more !== Boolean(value.page.next_cursor))
    throw new Error("The Staff onboarding hospital list could not be continued safely.");
  return value;
}

const directoryIdentifier = z
  .string()
  .guid()
  .refine((value) => value !== "00000000-0000-0000-0000-000000000000")
  .transform((value) => value.toLowerCase());
const directoryStaff = staffSchema
  .extend({
    id: directoryIdentifier,
    hospital_id: directoryIdentifier,
    created_at: z.string().datetime({ offset: true }),
    updated_at: z.string().datetime({ offset: true }),
  })
  .strict();
const directoryPage = z
  .object({
    data: z.array(directoryStaff).max(200),
    page: z
      .object({ has_more: z.boolean(), next_cursor: z.string().min(1).max(2048).nullable() })
      .strict(),
  })
  .strict();

export async function listStaffDirectoryPage(
  scope: EnrollmentRequestScope,
  hospitalId: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  validateDirectoryScope(scope);
  const requestedHospitalId = directoryIdentifier.parse(hospitalId);
  const params = new URLSearchParams({ limit: "50" });
  if (cursor !== null) params.set("cursor", z.string().min(1).max(2048).parse(cursor));
  const page = directoryPage.parse(
    await apiFetch(
      withStaffIntakeScope(`/workforce/hospitals/${requestedHospitalId}/staff?${params}`, scope),
      { ...enrollmentRequestOptions(scope), signal },
    ),
  );
  if (
    page.data.some((row) => row.hospital_id !== requestedHospitalId) ||
    new Set(page.data.map((row) => row.id)).size !== page.data.length
  )
    throw new Error("The Staff source does not match the requested directory.");
  if (page.page.has_more !== Boolean(page.page.next_cursor))
    throw new Error("The Staff directory could not be continued safely.");
  return page;
}

function validateDirectoryScope(scope: EnrollmentRequestScope) {
  directoryIdentifier.parse(scope.tenantId);
  z.string()
    .min(1)
    .max(200)
    .refine((value) => value === value.trim() && !value.includes("\0"))
    .parse(scope.subject);
}

const sourceHospitalPage = z
  .object({
    data: z
      .array(
        z.object({
          id: directoryIdentifier,
          name: z.string().min(1).max(150),
          access_level: z.enum(["reader", "manager"]),
        }),
      )
      .max(200),
    page: directoryPage.shape.page,
  })
  .strict();

export async function listStaffHospitalsPage(
  scope: EnrollmentRequestScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  validateDirectoryScope(scope);
  const params = new URLSearchParams({ limit: "50" });
  if (cursor !== null) params.set("cursor", z.string().min(1).max(2048).parse(cursor));
  const page = sourceHospitalPage.parse(
    await apiFetch(`/workforce/hospitals?${params}`, {
      ...enrollmentRequestOptions(scope),
      signal,
    }),
  );
  if (
    new Set(page.data.map((row) => row.id)).size !== page.data.length ||
    page.page.has_more !== Boolean(page.page.next_cursor)
  )
    throw new Error("The Staff source hospital list could not be continued safely.");
  return page;
}
