import { z } from "zod";
import { apiFetch } from "./client";
import { enrollmentRequestOptions } from "./enrollment";
import { tenantTransitionPending } from "./tenant-transition";
import {
  DepartmentCommandContract,
  DepartmentOutputContract,
  OrganizationRegistrationInputContract,
  OrganizationRegistrationOutputContract,
  OrganizationHospitalHeadContract,
  type DepartmentCommand,
} from "./organization-structure-contract";

export { v7 as newOrganizationId } from "uuid";
export type OrganizationScope = { tenantId: string; subject: string };
export type {
  DepartmentOutput as OrganizationDepartment,
  DepartmentCommand as OrganizationDepartmentCommand,
} from "./organization-structure-contract";

const identifier = z
  .string()
  .guid()
  .refine((value) => value !== "00000000-0000-0000-0000-000000000000")
  .transform((value) => value.toLowerCase());
const commandId = identifier.refine((value) =>
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value),
);
const cursorContract = z.string().min(1).max(2048);
const pageMetadata = z
  .object({ has_more: z.boolean(), next_cursor: cursorContract.nullable() })
  .strict();
const departmentPage = z
  .object({ data: z.array(DepartmentOutputContract).max(50), page: pageMetadata })
  .strict();
const hospitalPage = z
  .object({ data: z.array(OrganizationHospitalHeadContract).max(50), page: pageMetadata })
  .strict();
function scopedRequest(scope: OrganizationScope) {
  identifier.parse(scope.tenantId);
  z.string()
    .min(1)
    .max(200)
    .refine((value) => value === value.trim() && !/\p{Cc}/u.test(value))
    .parse(scope.subject);
  if (tenantTransitionPending()) throw new Error("Wait for your workspace to finish switching.");
  return enrollmentRequestOptions(scope);
}
function pageQuery(cursor: string | null) {
  const params = new URLSearchParams({ limit: "50" });
  if (cursor !== null) params.set("cursor", cursorContract.parse(cursor));
  return params;
}
function safePage<T>(
  page: { data: T[]; page: z.infer<typeof pageMetadata> },
  key: (row: T) => string,
) {
  if (
    new Set(page.data.map(key)).size !== page.data.length ||
    page.page.has_more !== Boolean(page.page.next_cursor)
  )
    throw new Error("The organization page could not be verified.");
  return page;
}
function departmentRoot(hospitalId: string) {
  return `/organization/hospitals/${identifier.parse(hospitalId)}/departments`;
}
function matchingHospital(actual: string, expected: string) {
  if (identifier.parse(actual) !== identifier.parse(expected))
    throw new Error("The organization source could not be verified.");
}
function verifyDepartment(row: z.infer<typeof DepartmentOutputContract>, hospital: string) {
  matchingHospital(row.hospital_id, hospital);
  commandId.parse(row.id);
  commandId.parse(row.revision_id);
  return row;
}
export async function listOrganizationHospitals(
  scope: OrganizationScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const page = hospitalPage.parse(
    await apiFetch(`/organization/hospitals?${pageQuery(cursor)}`, {
      ...scopedRequest(scope),
      signal,
    }),
  );
  page.data.forEach((row) => commandId.parse(row.request_id));
  return safePage(page, (row) => identifier.parse(row.hospital_id));
}
export async function registerOrganizationHospital(
  scope: OrganizationScope,
  hospitalId: string,
  requestId: string,
  reason: string,
  signal?: AbortSignal,
) {
  const body = OrganizationRegistrationInputContract.parse({ reason: reason.trim() });
  const result = OrganizationRegistrationOutputContract.parse(
    await apiFetch(
      `/organization/hospitals/${identifier.parse(hospitalId)}/registrations/${commandId.parse(requestId)}`,
      { ...scopedRequest(scope), method: "PUT", body: JSON.stringify(body), signal },
    ),
  );
  matchingHospital(result.hospital_id, hospitalId);
  if (identifier.parse(result.request_id) !== commandId.parse(requestId))
    throw new Error("The hospital registration receipt could not be verified.");
  return result;
}
export async function listOrganizationDepartments(
  scope: OrganizationScope,
  hospitalId: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const page = departmentPage.parse(
    await apiFetch(`${departmentRoot(hospitalId)}?${pageQuery(cursor)}`, {
      ...scopedRequest(scope),
      signal,
    }),
  );
  page.data.forEach((row) => verifyDepartment(row, hospitalId));
  return safePage(page, (row) => identifier.parse(row.id));
}
export async function listPostingDepartmentChoices(
  scope: OrganizationScope,
  hospitalId: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const page = departmentPage.parse(
    await apiFetch(
      `/organization/hospitals/${identifier.parse(hospitalId)}/posting-department-choices?${pageQuery(cursor)}`,
      {
        ...scopedRequest(scope),
        signal,
      },
    ),
  );
  page.data.forEach((row) => {
    verifyDepartment(row, hospitalId);
    if (row.status !== "active") throw new Error("The posting department is no longer available.");
  });
  if (cursor !== null && page.page.next_cursor === cursor)
    throw new Error("The posting department page could not be verified.");
  return safePage(page, (row) => identifier.parse(row.id));
}
export async function listOrganizationDepartmentRevisions(
  scope: OrganizationScope,
  hospitalId: string,
  nodeId: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const node = commandId.parse(nodeId);
  const page = departmentPage.parse(
    await apiFetch(`${departmentRoot(hospitalId)}/${node}/revisions?${pageQuery(cursor)}`, {
      ...scopedRequest(scope),
      signal,
    }),
  );
  page.data.forEach((row) => {
    verifyDepartment(row, hospitalId);
    if (identifier.parse(row.id) !== node)
      throw new Error("The department history could not be verified.");
  });
  return safePage(page, (row) => identifier.parse(row.revision_id));
}
export async function recordOrganizationDepartment(
  scope: OrganizationScope,
  hospitalId: string,
  nodeId: string,
  revisionId: string,
  input: DepartmentCommand,
  signal?: AbortSignal,
) {
  const body = DepartmentCommandContract.parse({
    ...input,
    code: input.code.trim().toUpperCase(),
    parent_id: input.parent_id ? identifier.parse(input.parent_id) : input.parent_id,
    name: input.name.trim(),
    reason: input.reason.trim().replace(/\s+/g, " "),
  });
  const node = commandId.parse(nodeId),
    revision = commandId.parse(revisionId);
  const result = verifyDepartment(
    DepartmentOutputContract.parse(
      await apiFetch(`${departmentRoot(hospitalId)}/${node}/revisions/${revision}`, {
        ...scopedRequest(scope),
        method: "PUT",
        body: JSON.stringify(body),
        signal,
      }),
    ),
    hospitalId,
  );
  if (
    identifier.parse(result.id) !== node ||
    identifier.parse(result.revision_id) !== revision ||
    result.version !== body.expected_version + 1 ||
    result.code !== body.code ||
    result.name !== body.name ||
    result.kind !== body.kind ||
    result.status !== body.status ||
    (result.parent_id ?? null) !== (body.parent_id ?? null)
  )
    throw new Error("The department revision receipt could not be verified.");
  return result;
}

export const organizationDepartmentEditorContract = DepartmentCommandContract.superRefine(
  (value, ctx) => {
    if (value.kind === "sub_unit" && !value.parent_id)
      ctx.addIssue({
        code: "custom",
        path: ["parent_id"],
        message: "Choose the parent department or sub-unit.",
      });
    if (value.kind === "department" && value.parent_id)
      ctx.addIssue({
        code: "custom",
        path: ["parent_id"],
        message: "A department belongs directly to its hospital.",
      });
    if (/\p{Cc}/u.test(value.name))
      ctx.addIssue({
        code: "custom",
        path: ["name"],
        message: "Remove control characters from the name.",
      });
  },
);
