import { z } from "zod";
import { apiFetch } from "./client";
import { enrollmentRequestOptions } from "./enrollment";
import { tenantTransitionPending } from "./tenant-transition";
import {
  DepartmentLinkInputContract,
  DepartmentLinkOutputContract,
  type DepartmentLinkInput,
} from "./organization-structure-contract";
import type { OrganizationScope } from "./organization-directory";

export type { DepartmentLinkInput, DepartmentLinkOutput } from "./organization-structure-contract";
export const departmentLinkSchema = DepartmentLinkInputContract;
const id = z
  .string()
  .guid()
  .refine((value) => value !== "00000000-0000-0000-0000-000000000000")
  .transform((value) => value.toLowerCase());
const command = id.refine((value) =>
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value),
);
const cursorSchema = z.string().min(1).max(2048);
const pageSchema = z
  .object({
    data: z.array(DepartmentLinkOutputContract).max(50),
    page: z.object({ has_more: z.boolean(), next_cursor: cursorSchema.nullable() }).strict(),
  })
  .strict();
function options(scope: OrganizationScope) {
  id.parse(scope.tenantId);
  z.string()
    .min(1)
    .max(200)
    .refine((value) => value === value.trim() && !/\p{Cc}/u.test(value))
    .parse(scope.subject);
  if (tenantTransitionPending()) throw new Error("Wait for your workspace to finish switching.");
  return enrollmentRequestOptions(scope);
}
function root(hospital: string, master: string) {
  return `/workforce/hospitals/${id.parse(hospital)}/hr/masters/${id.parse(master)}/department-links`;
}
function verify(raw: unknown, hospital: string, master: string) {
  const row = DepartmentLinkOutputContract.parse(raw);
  if (
    id.parse(row.hospital_id) !== id.parse(hospital) ||
    id.parse(row.master_id) !== id.parse(master) ||
    (row.status === "pending") !== !row.observed_at
  )
    throw new Error("The department handoff source could not be verified.");
  command.parse(row.request_id);
  command.parse(row.department_id);
  command.parse(row.department_revision_id);
  id.parse(row.master_revision_id);
  return row;
}
export async function recordDepartmentLink(
  scope: OrganizationScope,
  hospital: string,
  master: string,
  request: string,
  input: DepartmentLinkInput,
  signal?: AbortSignal,
) {
  const body = DepartmentLinkInputContract.parse(input);
  id.parse(body.master_revision_id);
  command.parse(body.department_id);
  command.parse(body.department_revision_id);
  const row = verify(
    await apiFetch(`${root(hospital, master)}/${command.parse(request)}`, {
      ...options(scope),
      method: "PUT",
      body: JSON.stringify(body),
      signal,
    }),
    hospital,
    master,
  );
  if (
    id.parse(row.request_id) !== id.parse(request) ||
    id.parse(row.master_revision_id) !== id.parse(body.master_revision_id) ||
    id.parse(row.department_id) !== id.parse(body.department_id) ||
    id.parse(row.department_revision_id) !== id.parse(body.department_revision_id) ||
    row.version !== body.expected_version + 1
  )
    throw new Error("The retained department handoff receipt could not be verified.");
  return row;
}
export async function listDepartmentLinks(
  scope: OrganizationScope,
  hospital: string,
  master: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ limit: "50" });
  if (cursor !== null) query.set("cursor", cursorSchema.parse(cursor));
  const page = pageSchema.parse(
    await apiFetch(`${root(hospital, master)}?${query}`, { ...options(scope), signal }),
  );
  page.data.forEach((row) => verify(row, hospital, master));
  if (
    new Set(page.data.map((row) => id.parse(row.request_id))).size !== page.data.length ||
    page.page.has_more !== Boolean(page.page.next_cursor) ||
    (cursor !== null && page.page.next_cursor === cursor)
  )
    throw new Error("The department handoff history could not be verified.");
  return page;
}
