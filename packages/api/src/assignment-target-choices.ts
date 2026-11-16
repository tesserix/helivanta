import { z } from "zod";
import { apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import { AssignmentTargetChoiceContract, ProposalInputContract } from "./staff-access-contract";
import type { GovernanceAssignmentScope } from "./governance-assignments";

export type { AssignmentTargetChoice } from "./staff-access-contract";

const resourceId = z
  .guid()
  .refine(
    (value) => value === value.toLowerCase() && value !== "00000000-0000-0000-0000-000000000000",
  );
const cursorContract = z.string().min(1).max(2048);
const choicesContract = z
  .object({
    data: AssignmentTargetChoiceContract.array().max(20),
    page: z.object({ has_more: z.boolean(), next_cursor: cursorContract.nullable() }).strict(),
  })
  .strict()
  .superRefine((value, context) => {
    if (
      value.page.has_more !== (value.page.next_cursor !== null) ||
      new Set(value.data.map((row) => row.staff_id)).size !== value.data.length ||
      new Set(value.data.map((row) => row.subject)).size !== value.data.length ||
      value.data.some(
        (row) =>
          !resourceId.safeParse(row.hospital_id).success ||
          !resourceId.safeParse(row.staff_id).success,
      )
    )
      context.addIssue({ code: "custom", message: "Current Staff choices could not be verified." });
  });

export const assignmentTargetChoicesKey = (scope: GovernanceAssignmentScope) => [
  "iam",
  "assignment-target-choices",
  scope.tenantId,
  scope.subject,
  scope.hospitalId,
];

export async function listAssignmentTargetChoices(
  scope: GovernanceAssignmentScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const hospital = resourceId.parse(scope.hospitalId);
  const account = {
    tenantId: resourceId.parse(scope.tenantId),
    subject: ProposalInputContract.shape.subject.parse(scope.subject),
  };
  const query = new URLSearchParams({ status: "eligible", limit: "20" });
  if (cursor !== null) query.set("cursor", cursorContract.parse(cursor));
  const raw = await apiFetch<unknown>(
    `/iam/hospitals/${hospital}/assignment-target-choices?${query}`,
    { ...membershipRequestOptions(account), signal },
  );
  signal?.throwIfAborted();
  const page = choicesContract.parse(raw);
  if (page.data.some((row) => row.hospital_id !== hospital))
    throw new Error("The Staff choices belong to another hospital.");
  return page;
}
