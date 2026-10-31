import { useInfiniteQuery } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetch } from "./client";
import {
  MembershipEnrollmentInputContract,
  MembershipEnrollmentContract,
  ProposalInputContract,
  type MembershipEnrollmentInput,
} from "./staff-access-contract";
import type { StaffAssignmentScope } from "./staff-assignments";
import { membershipRequestOptions, membershipsKey } from "./membership-commands";

export const staffMembershipSubjectContract = ProposalInputContract.shape.subject;
export const staffMembershipInputContract = MembershipEnrollmentInputContract.superRefine(
  (value, ctx) => {
    if (!value.reason.trim())
      ctx.addIssue({ code: "custom", path: ["reason"], message: "Enter an enrollment reason." });
    if (!value.subject.trim() || /[:#*\s]/u.test(value.subject))
      ctx.addIssue({ code: "custom", path: ["subject"], message: "Use a valid account ID." });
  },
);
export type StaffMembershipInput = MembershipEnrollmentInput;
const identifier = MembershipEnrollmentContract.shape.id;
export async function enrollStaffMember(
  scope: StaffAssignmentScope,
  id: string,
  input: MembershipEnrollmentInput,
) {
  identifier.refine((value) => value[14] === "7" && value === value.toLowerCase()).parse(id);
  const body = staffMembershipInputContract.parse(input);
  const receipt = MembershipEnrollmentContract.parse(
    await apiFetch(`/iam/membership-enrollments/${id}`, {
      ...membershipRequestOptions(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  if (
    receipt.id !== id ||
    receipt.actor_subject !== scope.subject ||
    receipt.subject !== body.subject ||
    receipt.reason !== body.reason ||
    receipt.membership_id[14] !== "7"
  )
    throw new Error("The enrollment receipt did not match the submitted command.");
  return receipt;
}
const page = z
  .object({
    data: z.array(MembershipEnrollmentContract),
    page: z.object({ has_more: z.boolean(), next_cursor: z.string().nullable() }).strict(),
  })
  .strict();
export function useStaffEnrollmentHistory(scope: StaffAssignmentScope) {
  return useInfiniteQuery({
    queryKey: [...membershipsKey(scope), "enrollments"],
    initialPageParam: null as string | null,
    gcTime: 0,
    staleTime: 0,
    queryFn: async ({ pageParam, signal }) =>
      page.parse(
        await apiFetch(
          `/iam/membership-enrollments?limit=50${pageParam ? `&cursor=${encodeURIComponent(pageParam)}` : ""}`,
          { ...membershipRequestOptions(scope), signal },
        ),
      ),
    getNextPageParam: (last) => (last.page.has_more ? last.page.next_cursor : undefined),
  });
}
