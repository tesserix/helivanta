import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import { OperatingGovernanceStaffChoiceContract } from "./staff-access-contract";
import { OperatingDutyScopeContract } from "./operating-assignments-contract";
import type { OperatingDutyStaffScope } from "./operating-duties";
export type OperatingStaffChoiceScope = Pick<
  OperatingDutyStaffScope,
  "tenantId" | "subject" | "employerKind" | "employerId"
>;
export type { OperatingGovernanceStaffChoice } from "./staff-access-contract";
const identifier = z.uuidv7().refine((value) => value === value.toLowerCase());
const cursorContract = z.string().min(1).max(2048);
const pageContract = z
  .object({
    data: OperatingGovernanceStaffChoiceContract.array().max(20),
    page: z.object({ has_more: z.boolean(), next_cursor: cursorContract.nullable() }).strict(),
  })
  .strict()
  .superRefine((value, context) => {
    if (
      value.page.has_more !== (value.page.next_cursor !== null) ||
      ["staff_id", "subject", "link_id"].some(
        (key) =>
          new Set(value.data.map((row) => row[key as "staff_id" | "subject" | "link_id"])).size !==
          value.data.length,
      ) ||
      value.data.some((row) => !identifier.safeParse(row.link_id).success)
    )
      context.addIssue({
        code: "custom",
        message: "The current Staff choices could not be verified.",
      });
  });
export const operatingStaffChoicesKey = (scope: OperatingStaffChoiceScope) => [
  "workforce",
  "operating-staff-choices",
  scope.tenantId,
  scope.subject,
  scope.employerKind,
  scope.employerId,
];
export async function listOperatingStaffChoices(
  scope: OperatingStaffChoiceScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const kind = OperatingDutyScopeContract.shape.employer_kind.parse(scope.employerKind);
  const employer = identifier.parse(scope.employerId);
  const query = new URLSearchParams({ status: "active", limit: "20" });
  if (cursor !== null) query.set("cursor", cursorContract.parse(cursor));
  const result = await apiFetch<unknown>(
    `/workforce/operating-governance-employers/${kind}/${employer}/staff-choices?${query}`,
    { ...membershipRequestOptions(scope), signal },
  );
  signal?.throwIfAborted();
  return pageContract.parse(result);
}
export function useOperatingStaffChoices(
  scope: OperatingStaffChoiceScope,
  cursor: string | null,
  enabled = true,
) {
  return useQuery({
    queryKey: [...operatingStaffChoicesKey(scope), cursor],
    queryFn: ({ signal }) => listOperatingStaffChoices(scope, cursor, signal),
    enabled,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: true,
  });
}
