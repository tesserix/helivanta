import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import {
  OperatingLocationChoiceContract,
  OperatingDutyScopeContract,
} from "./operating-assignments-contract";
import type { OperatingStaffChoiceScope } from "./operating-staff-choices";
export type OperatingLocationChoiceScope = OperatingStaffChoiceScope;
const identifier = z.uuidv7().refine((value) => value === value.toLowerCase());
const cursorContract = z.string().min(1).max(2048);
const pageContract = z
  .object({
    data: OperatingLocationChoiceContract.array().max(20),
    page: z.object({ has_more: z.boolean(), next_cursor: cursorContract.nullable() }).strict(),
  })
  .strict()
  .superRefine((value, context) => {
    if (
      value.page.has_more !== (value.page.next_cursor !== null) ||
      (value.page.has_more && value.data.length === 0) ||
      new Set(value.data.map((row) => row.location_id)).size !== value.data.length ||
      value.data.some((row) => !identifier.safeParse(row.location_id).success)
    )
      context.addIssue({
        code: "custom",
        message: "The current location choices could not be verified.",
      });
  });
export const operatingLocationChoicesKey = (scope: OperatingLocationChoiceScope) => [
  "iam",
  "operating-location-choices",
  scope.tenantId,
  scope.subject,
  scope.employerKind,
  scope.employerId,
];
export async function listOperatingLocationChoices(
  scope: OperatingLocationChoiceScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const kind = OperatingDutyScopeContract.shape.employer_kind.parse(scope.employerKind),
    employer = identifier.parse(scope.employerId);
  const query = new URLSearchParams({ country: "IN", limit: "20" });
  if (cursor !== null) query.set("cursor", cursorContract.parse(cursor));
  const raw = await apiFetch<unknown>(
    `/iam/operating-governance-employers/${kind}/${employer}/location-choices?${query}`,
    { ...membershipRequestOptions(scope), signal },
  );
  signal?.throwIfAborted();
  const page = pageContract.parse(raw);
  if (
    page.data.some(
      (row) =>
        row.owner_tenant_id !== scope.tenantId ||
        row.employer_kind !== scope.employerKind ||
        row.employer_id !== scope.employerId,
    )
  )
    throw new Error("The location choices belong to another workspace or employer.");
  return page;
}
export function useOperatingLocationChoices(
  scope: OperatingLocationChoiceScope,
  cursor: string | null,
  enabled = true,
) {
  return useQuery({
    queryKey: [...operatingLocationChoicesKey(scope), cursor],
    queryFn: ({ signal }) => listOperatingLocationChoices(scope, cursor, signal),
    enabled,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: true,
  });
}
