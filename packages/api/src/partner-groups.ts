import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetch, POLL_INTERVAL_MS } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import type { StaffAssignmentScope } from "./staff-assignments";
import {
  PartnerGroupContract,
  PartnerGroupProposalInputContract,
  PartnerGroupCommandInputContract,
  PartnerGroupCommandReceiptContract,
  type PartnerGroupProposalInput,
  type PartnerGroupCommandInput,
} from "./partner-group-contract";
export { v7 as newPartnerGroupId } from "uuid";
export type PartnerGroupScope = StaffAssignmentScope;
export const partnerGroupProposalContract = PartnerGroupProposalInputContract;
export const partnerGroupDecisionContract = PartnerGroupCommandInputContract;
const identifier = PartnerGroupContract.shape.id.regex(
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
  "Choose a canonical group or command.",
);
const base = "/iam/partner-groups";
const pageContract = z
  .object({
    data: z.array(PartnerGroupContract),
    page: z.object({ has_more: z.boolean(), next_cursor: z.string().nullable() }).strict(),
  })
  .strict()
  .superRefine((value, ctx) => {
    if (
      value.page.has_more !==
      (typeof value.page.next_cursor === "string" && value.page.next_cursor.length > 0)
    )
      ctx.addIssue({
        code: "custom",
        path: ["page"],
        message: "The group continuation is inconsistent.",
      });
    if (new Set(value.data.map((group) => group.id)).size !== value.data.length)
      ctx.addIssue({
        code: "custom",
        path: ["data"],
        message: "The group page contains repeated records.",
      });
  });
export const partnerGroupsKey = (scope: PartnerGroupScope) => [
  "iam",
  "partner-groups",
  scope.tenantId,
  scope.subject,
];
function owned(id: string, response: unknown) {
  const value = PartnerGroupContract.parse(response);
  if (value.id !== id) throw new Error("The group response did not match the selected team.");
  return value;
}
export async function preparePartnerGroup(
  scope: PartnerGroupScope,
  id: string,
  input: PartnerGroupProposalInput,
) {
  const body = PartnerGroupProposalInputContract.parse(input);
  const value = owned(
    id,
    await apiFetch(`${base}/${identifier.parse(id)}`, {
      ...membershipRequestOptions(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  return verifyPreparedGroup(scope, body, value);
}
function verifyPreparedGroup(
  scope: PartnerGroupScope,
  body: PartnerGroupProposalInput,
  value: ReturnType<typeof owned>,
) {
  if (
    value.proposed_by !== scope.subject ||
    value.hospital_id !== body.hospital_id ||
    value.name !== body.name ||
    value.purpose !== body.purpose ||
    value.reason !== body.reason ||
    Date.parse(value.valid_from) !== Date.parse(body.valid_from) ||
    Date.parse(value.expires_at) !== Date.parse(body.expires_at)
  )
    throw new Error("The prepared group did not match the submitted intent.");
  return value;
}
export async function recoverPreparedPartnerGroup(
  scope: PartnerGroupScope,
  id: string,
  input: PartnerGroupProposalInput,
) {
  const body = PartnerGroupProposalInputContract.parse(input);
  return verifyPreparedGroup(scope, body, await getPartnerGroup(scope, id));
}

export async function getPartnerGroup(scope: PartnerGroupScope, id: string, signal?: AbortSignal) {
  return owned(
    id,
    await apiFetch(`${base}/${identifier.parse(id)}`, {
      ...membershipRequestOptions(scope),
      signal,
    }),
  );
}
function commandReceipt(id: string, command: string, response: unknown) {
  const value = PartnerGroupCommandReceiptContract.parse(response);
  const statuses = {
    approve: "approved",
    reject: "rejected",
    withdraw: "withdrawn",
    suspend: "suspended",
  };
  if (
    value.id !== command ||
    value.group_id !== id ||
    value.group.id !== id ||
    value.result_version !== value.expected_version + 1 ||
    value.group.version !== value.result_version ||
    value.group.status !== statuses[value.operation]
  )
    throw new Error("The group command evidence did not match its record.");
  return value;
}
export async function decidePartnerGroup(
  scope: PartnerGroupScope,
  id: string,
  command: string,
  input: PartnerGroupCommandInput,
) {
  const body = PartnerGroupCommandInputContract.parse(input);
  const value = commandReceipt(
    id,
    command,
    await apiFetch(`${base}/${identifier.parse(id)}/commands/${identifier.parse(command)}`, {
      ...membershipRequestOptions(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  if (
    value.actor_subject !== scope.subject ||
    value.operation !== body.operation ||
    value.expected_version !== body.expected_version ||
    value.reason !== body.reason
  )
    throw new Error("The group decision receipt did not match the submitted command.");
  return value;
}
export async function getPartnerGroupCommand(
  scope: PartnerGroupScope,
  id: string,
  command: string,
  signal?: AbortSignal,
) {
  return commandReceipt(
    id,
    command,
    await apiFetch(`${base}/${identifier.parse(id)}/commands/${identifier.parse(command)}`, {
      ...membershipRequestOptions(scope),
      signal,
    }),
  );
}
export async function listPartnerGroups(
  scope: PartnerGroupScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const params = new URLSearchParams({ limit: "50" });
  if (cursor) params.set("cursor", cursor);
  return pageContract.parse(
    await apiFetch(`${base}?${params}`, { ...membershipRequestOptions(scope), signal }),
  );
}
export function usePartnerGroups(scope: PartnerGroupScope) {
  return useInfiniteQuery({
    queryKey: partnerGroupsKey(scope),
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) => listPartnerGroups(scope, pageParam, signal),
    getNextPageParam: (last) => (last.page.has_more ? last.page.next_cursor : undefined),
    gcTime: 0,
    staleTime: 0,
    retry: false,
  });
}
export function usePartnerGroup(scope: PartnerGroupScope, id: string, busy = false) {
  return useQuery({
    queryKey: [...partnerGroupsKey(scope), id],
    queryFn: ({ signal }) => getPartnerGroup(scope, id, signal),
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
    refetchInterval: (query) =>
      !busy && query.state.data?.status === "pending_scope" ? POLL_INTERVAL_MS : false,
  });
}
