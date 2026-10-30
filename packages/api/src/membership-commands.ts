import { z } from "zod";
import { useInfiniteQuery } from "@tanstack/react-query";
import { apiFetch } from "./client";
import {
  MembershipCommandInputContract,
  MembershipCommandContract,
  MembershipCandidateContract,
  MembershipCommandHistoryContract,
  ProposalInputContract,
  type MembershipCommandInput,
} from "./staff-access-contract";
import type { StaffAssignmentScope } from "./staff-assignments";

export type {
  MembershipCommandInput,
  MembershipCommand,
  MembershipCandidate,
  MembershipCommandHistory,
} from "./staff-access-contract";
export { v7 as newMembershipCommandId } from "uuid";
export const membershipsKey = (scope: StaffAssignmentScope) => [
  "iam",
  "memberships",
  scope.tenantId,
  scope.subject,
];
export const membershipCommandInputContract = MembershipCommandInputContract.superRefine(
  (value, ctx) => {
    if (!value.reason.trim())
      ctx.addIssue({ code: "custom", path: ["reason"], message: "Enter a reason." });
    if (!ProposalInputContract.shape.subject.safeParse(value.subject).success)
      ctx.addIssue({ code: "custom", path: ["subject"], message: "Use a valid account ID." });
    if (value.operation === "recover" && value.role_key !== "staff_member")
      ctx.addIssue({
        code: "custom",
        path: ["role_key"],
        message: "Recovery creates ordinary membership only.",
      });
  },
);
const identifier = MembershipCommandContract.shape.id;
function options(scope: StaffAssignmentScope): RequestInit {
  return {
    cache: "no-store",
    headers: {
      "X-Helivanta-Expected-Tenant": identifier.parse(scope.tenantId),
      "X-Helivanta-Expected-Subject": ProposalInputContract.shape.subject.parse(scope.subject),
    },
  };
}
export async function submitMembershipCommand(
  scope: StaffAssignmentScope,
  id: string,
  input: MembershipCommandInput,
) {
  identifier.refine((value) => value[14] === "7" && value === value.toLowerCase()).parse(id);
  const body = membershipCommandInputContract.parse(input);
  const result = MembershipCommandContract.parse(
    await apiFetch(`/iam/membership-commands/${id}`, {
      ...options(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  if (
    result.id !== id ||
    result.actor_subject !== scope.subject ||
    Object.entries(body).some(([key, value]) => result[key as keyof typeof result] !== value) ||
    (body.operation === "remove" &&
      (result.result_membership_id !== body.basis_id ||
        ![body.expected_membership_epoch, body.expected_membership_epoch + 1].includes(
          result.result_membership_epoch,
        ))) ||
    (body.operation === "recover" &&
      (result.result_membership_id === body.basis_id ||
        result.result_membership_epoch !== body.expected_membership_epoch))
  )
    throw new Error("The receipt did not match the submitted membership command.");
  return result;
}
const pageInfo = z.object({ has_more: z.boolean(), next_cursor: z.string().nullable() }).strict();
const candidatesPage = z
  .object({ data: z.array(MembershipCandidateContract), page: pageInfo })
  .strict();
const historyPage = z
  .object({ data: z.array(MembershipCommandHistoryContract), page: pageInfo })
  .strict();
export function useMembershipCandidates(scope: StaffAssignmentScope) {
  return useInfiniteQuery({
    queryKey: [...membershipsKey(scope), "candidates"],
    initialPageParam: null as string | null,
    gcTime: 0,
    staleTime: 0,
    queryFn: async ({ pageParam, signal }) =>
      candidatesPage.parse(
        await apiFetch(
          `/iam/membership-candidates?limit=50${pageParam ? `&cursor=${encodeURIComponent(pageParam)}` : ""}`,
          { ...options(scope), signal },
        ),
      ),
    getNextPageParam: (last) => (last.page.has_more ? last.page.next_cursor : undefined),
  });
}
export function useMembershipHistory(scope: StaffAssignmentScope) {
  return useInfiniteQuery({
    queryKey: [...membershipsKey(scope), "history"],
    initialPageParam: null as string | null,
    gcTime: 0,
    staleTime: 0,
    queryFn: async ({ pageParam, signal }) =>
      historyPage.parse(
        await apiFetch(
          `/iam/membership-commands?limit=50${pageParam ? `&cursor=${encodeURIComponent(pageParam)}` : ""}`,
          { ...options(scope), signal },
        ),
      ),
    getNextPageParam: (last) => (last.page.has_more ? last.page.next_cursor : undefined),
  });
}
