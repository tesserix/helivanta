import { z } from "zod";
import { apiFetch } from "./client";
import { personReviewRequest } from "./person-reviews";
import {
  StaffPostingHospitalScopeContract,
  StaffPostingReviewHospitalScopeContract,
  StaffPostingTargetContract,
  StaffPostingCutoffHospitalScopeContract,
} from "./staff-postings-contract";
import type { StaffPostingScope } from "./staff-postings";

const cursorContract = z.string().min(1).max(2048);
const continuation = z
  .object({ has_more: z.boolean(), next_cursor: cursorContract.nullable() })
  .strict();
const hospitalPage = z
  .object({ data: z.array(StaffPostingHospitalScopeContract).max(20), page: continuation })
  .strict();
const reviewHospitalPage = z
  .object({ data: z.array(StaffPostingReviewHospitalScopeContract).max(20), page: continuation })
  .strict();
const cutoffHospitalPage = z
  .object({ data: z.array(StaffPostingCutoffHospitalScopeContract).max(20), page: continuation })
  .strict();
const targetPage = z
  .object({ data: z.array(StaffPostingTargetContract).max(50), page: continuation })
  .strict();
function query(limit: number, cursor: string | null) {
  const value = new URLSearchParams({ limit: String(limit) });
  if (cursor !== null) value.set("cursor", cursorContract.parse(cursor));
  return value;
}
function verifyPage<T>(
  page: { data: T[]; page: z.infer<typeof continuation> },
  cursor: string | null,
  key: (row: T) => string,
) {
  if (
    page.page.has_more !== Boolean(page.page.next_cursor) ||
    (cursor !== null && page.page.next_cursor === cursor) ||
    new Set(page.data.map(key)).size !== page.data.length
  )
    throw new Error("The posting workspace page could not be verified.");
  return page;
}
export async function listStaffPostingScopes(
  scope: Pick<StaffPostingScope, "tenantId" | "subject">,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const result = await apiFetch(`/workforce/posting-scopes?${query(20, cursor)}`, {
    ...personReviewRequest(scope),
    signal,
  });
  signal?.throwIfAborted();
  return verifyPage(hospitalPage.parse(result), cursor, (row) => row.hospital_id.toLowerCase());
}
export async function listStaffPostingTargets(
  scope: Pick<StaffPostingScope, "tenantId" | "subject" | "hospitalId">,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const hospital = z
    .guid()
    .refine((value) => value !== "00000000-0000-0000-0000-000000000000")
    .parse(scope.hospitalId)
    .toLowerCase();
  signal?.throwIfAborted();
  const result = await apiFetch(
    `/workforce/hospitals/${hospital}/posting-targets?${query(50, cursor)}`,
    { ...personReviewRequest(scope), signal },
  );
  signal?.throwIfAborted();
  return verifyPage(targetPage.parse(result), cursor, (row) => row.id.toLowerCase());
}

export async function listStaffPostingReviewScopes(
  scope: Pick<StaffPostingScope, "tenantId" | "subject">,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const result = await apiFetch(`/workforce/posting-review-scopes?${query(20, cursor)}`, {
    ...personReviewRequest(scope),
    signal,
  });
  signal?.throwIfAborted();
  return verifyPage(reviewHospitalPage.parse(result), cursor, (row) =>
    row.hospital_id.toLowerCase(),
  );
}

export async function listStaffPostingCutoffScopes(
  scope: Pick<StaffPostingScope, "tenantId" | "subject">,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const result = await apiFetch(`/workforce/posting-cutoff-scopes?${query(20, cursor)}`, {
    ...personReviewRequest(scope),
    signal,
  });
  signal?.throwIfAborted();
  return verifyPage(cutoffHospitalPage.parse(result), cursor, (row) =>
    row.hospital_id.toLowerCase(),
  );
}
