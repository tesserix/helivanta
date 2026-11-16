import { z } from "zod";
import { apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import { DepartmentOutputContract } from "./organization-structure-contract";
import { ProposalInputContract } from "./staff-access-contract";
import type { GovernanceAssignmentScope } from "./governance-assignments";

const hospitalId = z
  .guid()
  .refine(
    (value) => value === value.toLowerCase() && value !== "00000000-0000-0000-0000-000000000000",
  );
const nodeId = z.uuidv7().refine((value) => value === value.toLowerCase());
const cursorContract = z.string().min(1).max(2048);
const choicePage = z
  .object({
    data: DepartmentOutputContract.array().max(20),
    page: z.object({ has_more: z.boolean(), next_cursor: cursorContract.nullable() }).strict(),
  })
  .strict()
  .superRefine((value, context) => {
    if (
      value.page.has_more !== (value.page.next_cursor !== null) ||
      (value.page.has_more && value.data.length !== 20) ||
      new Set(value.data.map((row) => row.id)).size !== value.data.length ||
      value.data.some(
        (row) =>
          row.status !== "active" ||
          !nodeId.safeParse(row.id).success ||
          !nodeId.safeParse(row.revision_id).success ||
          (row.kind === "department"
            ? row.parent_id != null
            : !nodeId.safeParse(row.parent_id).success),
      )
    )
      context.addIssue({
        code: "custom",
        message: "Current department choices could not be verified.",
      });
  });

export const assignmentDepartmentChoicesKey = (scope: GovernanceAssignmentScope) => [
  "organization",
  "assignment-department-choices",
  scope.tenantId,
  scope.subject,
  scope.hospitalId,
];

export async function listAssignmentDepartmentChoices(
  scope: GovernanceAssignmentScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const hospital = hospitalId.parse(scope.hospitalId);
  const account = {
    tenantId: hospitalId.parse(scope.tenantId),
    subject: ProposalInputContract.shape.subject.parse(scope.subject),
  };
  const query = new URLSearchParams({ limit: "20" });
  if (cursor !== null) query.set("cursor", cursorContract.parse(cursor));
  const raw = await apiFetch<unknown>(
    `/organization/hospitals/${hospital}/assignment-department-choices?${query}`,
    { ...membershipRequestOptions(account), signal },
  );
  signal?.throwIfAborted();
  const page = choicePage.parse(raw);
  if (page.data.some((row) => row.hospital_id !== hospital))
    throw new Error("The department choices belong to another hospital.");
  return page;
}
