import { z } from "zod";
import { staffSchema } from "./workspace";
import {
  staffDirectoryReadMetadataContract,
  hrIntakeScopeContract,
} from "./staff-directory-contract";
import { apiFetch } from "./client";
import { enrollmentRequestOptions, type EnrollmentRequestScope } from "./enrollment";

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
