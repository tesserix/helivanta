import { listStaffActorHospitalChoices } from "./staff-actor-scopes";
import { z } from "zod";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import type { PartnerGroupScope } from "./partner-groups";
import {
  PartnerGroupContract,
  PartnerMemberCapabilitiesContract,
  PartnerMemberCandidateContract,
  PartnerGroupMemberProposalContract,
  PartnerMemberProposalInputContract,
  PartnerMemberCommandInputContract,
  PartnerMemberCommandReceiptContract,
  type PartnerMemberProposalInput,
  type PartnerMemberCommandInput,
} from "./partner-group-contract";
const identifier = PartnerGroupMemberProposalContract.shape.id;
const hospitalIdentifier = PartnerGroupMemberProposalContract.shape.hospital_id;
function path(hospital: string, group: string, member: string) {
  return `/iam/hospitals/${hospitalIdentifier.parse(hospital)}/partner-groups/${identifier.parse(group)}/member-proposals/${identifier.parse(member)}`;
}
function owned(hospital: string, group: string, member: string, response: unknown) {
  const value = PartnerGroupMemberProposalContract.parse(response);
  if (value.hospital_id !== hospital || value.group_id !== group || value.id !== member)
    throw new Error("The membership response did not match the selected hospital and team.");
  return value;
}
function prepared(
  scope: PartnerGroupScope,
  input: PartnerMemberProposalInput,
  value: ReturnType<typeof owned>,
) {
  if (
    value.prepared_by !== scope.subject ||
    Object.entries(input).some(([key, expected]) => {
      const actual = value.input[key as keyof PartnerMemberProposalInput];
      return key === "valid_from" || key === "expires_at"
        ? Date.parse(String(actual)) !== Date.parse(String(expected))
        : actual !== expected;
    })
  )
    throw new Error("The membership evidence did not match the submitted nomination.");
  return value;
}
export async function preparePartnerMember(
  scope: PartnerGroupScope,
  hospital: string,
  group: string,
  member: string,
  input: PartnerMemberProposalInput,
) {
  const body = PartnerMemberProposalInputContract.parse(input);
  return prepared(
    scope,
    body,
    owned(
      hospital,
      group,
      member,
      await apiFetch(path(hospital, group, member), {
        ...membershipRequestOptions(scope),
        method: "PUT",
        body: JSON.stringify(body),
      }),
    ),
  );
}
export async function getPartnerMember(
  scope: PartnerGroupScope,
  hospital: string,
  group: string,
  member: string,
  signal?: AbortSignal,
) {
  return owned(
    hospital,
    group,
    member,
    await apiFetch(path(hospital, group, member), { ...membershipRequestOptions(scope), signal }),
  );
}
export async function recoverPreparedPartnerMember(
  scope: PartnerGroupScope,
  hospital: string,
  group: string,
  member: string,
  input: PartnerMemberProposalInput,
) {
  return prepared(
    scope,
    PartnerMemberProposalInputContract.parse(input),
    await getPartnerMember(scope, hospital, group, member),
  );
}
function commandReceipt(
  scope: PartnerGroupScope,
  hospital: string,
  group: string,
  member: string,
  command: string,
  input: PartnerMemberCommandInput,
  response: unknown,
) {
  const value = PartnerMemberCommandReceiptContract.parse(response);
  owned(hospital, group, member, value.member);
  const statuses = {
    approve: "approved",
    reject: "rejected",
    withdraw: "withdrawn",
    suspend: "suspended",
  };
  if (
    value.id !== command ||
    value.member_id !== member ||
    value.actor_subject !== scope.subject ||
    JSON.stringify(value.input) !== JSON.stringify(input) ||
    value.result_version !== input.expected_version + 1 ||
    value.member.version !== value.result_version ||
    value.member.status !== statuses[input.operation] ||
    (input.operation === "approve" && !value.reviewed_source_use_id)
  )
    throw new Error("The membership command evidence did not match the retained decision.");
  return value;
}
export async function decidePartnerMember(
  scope: PartnerGroupScope,
  hospital: string,
  group: string,
  member: string,
  command: string,
  input: PartnerMemberCommandInput,
) {
  const body = PartnerMemberCommandInputContract.parse(input);
  return commandReceipt(
    scope,
    hospital,
    group,
    member,
    command,
    body,
    await apiFetch(`${path(hospital, group, member)}/commands/${identifier.parse(command)}`, {
      ...membershipRequestOptions(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}
export async function recoverPartnerMemberCommand(
  scope: PartnerGroupScope,
  hospital: string,
  group: string,
  member: string,
  command: string,
  input: PartnerMemberCommandInput,
) {
  const body = PartnerMemberCommandInputContract.parse(input);
  return commandReceipt(
    scope,
    hospital,
    group,
    member,
    command,
    body,
    await apiFetch(
      `${path(hospital, group, member)}/commands/${identifier.parse(command)}`,
      membershipRequestOptions(scope),
    ),
  );
}

function pageContract<T extends z.ZodType>(item: T) {
  return z
    .object({
      data: z.array(item),
      page: z.object({ has_more: z.boolean(), next_cursor: z.string().nullable() }).strict(),
    })
    .strict()
    .superRefine((value, ctx) => {
      if (value.page.has_more !== Boolean(value.page.next_cursor))
        ctx.addIssue({
          code: "custom",
          path: ["page"],
          message: "The membership continuation is inconsistent.",
        });
    });
}
const groupsPage = pageContract(PartnerGroupContract);
const membersPage = pageContract(PartnerGroupMemberProposalContract);
const candidatesPage = pageContract(PartnerMemberCandidateContract);
const query = (cursor: string | null) => {
  const params = new URLSearchParams({ limit: "50" });
  if (cursor) params.set("cursor", cursor);
  return params;
};
export async function listHospitalPartnerGroups(
  scope: PartnerGroupScope,
  hospital: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const value = groupsPage.parse(
    await apiFetch(
      `/iam/hospitals/${hospitalIdentifier.parse(hospital)}/partner-groups?${query(cursor)}`,
      { ...membershipRequestOptions(scope), signal },
    ),
  );
  if (value.data.some((row) => row.hospital_id !== hospital))
    throw new Error("The group page did not match the hospital.");
  return value;
}
export async function listPartnerMembers(
  scope: PartnerGroupScope,
  hospital: string,
  group: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const value = membersPage.parse(
    await apiFetch(
      `/iam/hospitals/${hospitalIdentifier.parse(hospital)}/partner-groups/${identifier.parse(group)}/member-proposals?${query(cursor)}`,
      { ...membershipRequestOptions(scope), signal },
    ),
  );
  for (const row of value.data) owned(hospital, group, row.id, row);
  if (new Set(value.data.map((row) => row.id)).size !== value.data.length)
    throw new Error("The membership page repeats a record.");
  return value;
}
export async function listPartnerMemberCandidates(
  scope: PartnerGroupScope,
  hospital: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const value = candidatesPage.parse(
    await apiFetch(
      `/iam/hospitals/${hospitalIdentifier.parse(hospital)}/partner-member-candidates?${query(cursor)}`,
      { ...membershipRequestOptions(scope), signal },
    ),
  );
  if (
    value.data.some((row) => row.hospital_id !== hospital || row.subject === scope.subject) ||
    new Set(value.data.map((row) => row.id)).size !== value.data.length
  )
    throw new Error("The candidate page did not match the hospital and requester.");
  return value;
}
export const hospitalPartnerGroupsKey = (scope: PartnerGroupScope, hospital: string) => [
  "iam",
  "hospital-partner-groups",
  scope.tenantId,
  scope.subject,
  hospital,
];
export const partnerMembersKey = (scope: PartnerGroupScope, hospital: string, group: string) => [
  ...hospitalPartnerGroupsKey(scope, hospital),
  group,
  "members",
];
export function useHospitalPartnerGroups(scope: PartnerGroupScope, hospital: string) {
  return useInfiniteQuery({
    queryKey: hospitalPartnerGroupsKey(scope, hospital),
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) =>
      listHospitalPartnerGroups(scope, hospital, pageParam, signal),
    getNextPageParam: (last) => (last.page.has_more ? last.page.next_cursor : undefined),
    gcTime: 0,
    staleTime: 0,
    retry: false,
  });
}
export function usePartnerMembers(scope: PartnerGroupScope, hospital: string, group: string) {
  return useInfiniteQuery({
    queryKey: partnerMembersKey(scope, hospital, group),
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) =>
      listPartnerMembers(scope, hospital, group, pageParam, signal),
    getNextPageParam: (last) => (last.page.has_more ? last.page.next_cursor : undefined),
    gcTime: 0,
    staleTime: 0,
    retry: false,
  });
}
export function usePartnerMemberCandidates(scope: PartnerGroupScope, hospital: string) {
  return useInfiniteQuery({
    queryKey: [...hospitalPartnerGroupsKey(scope, hospital), "candidates"],
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) =>
      listPartnerMemberCandidates(scope, hospital, pageParam, signal),
    getNextPageParam: (last) => (last.page.has_more ? last.page.next_cursor : undefined),
    gcTime: 0,
    staleTime: 0,
    retry: false,
  });
}

export function usePartnerMemberHospitals(scope: PartnerGroupScope) {
  return useInfiniteQuery({
    queryKey: ["iam", "partner-member-hospitals", scope.tenantId, scope.subject],
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) => listStaffActorHospitalChoices(scope, pageParam, signal),
    getNextPageParam: (last) => (last.page.has_more ? last.page.next_cursor : undefined),
    gcTime: 0,
    staleTime: 0,
    retry: false,
  });
}

export async function getPartnerMemberCapabilities(
  scope: PartnerGroupScope,
  hospital: string,
  signal?: AbortSignal,
) {
  const value = PartnerMemberCapabilitiesContract.parse(
    await apiFetch(
      `/iam/hospitals/${hospitalIdentifier.parse(hospital)}/partner-member-capabilities`,
      { ...membershipRequestOptions(scope), signal },
    ),
  );
  if (value.hospital_id !== hospital)
    throw new Error("The membership capabilities did not match the hospital.");
  return value;
}
export function usePartnerMemberCapabilities(scope: PartnerGroupScope, hospital: string) {
  return useQuery({
    queryKey: [...hospitalPartnerGroupsKey(scope, hospital), "capabilities"],
    queryFn: ({ signal }) => getPartnerMemberCapabilities(scope, hospital, signal),
    gcTime: 0,
    staleTime: 0,
    retry: false,
  });
}
