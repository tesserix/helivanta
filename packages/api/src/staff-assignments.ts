import { z } from "zod";
import { useInfiniteQuery } from "@tanstack/react-query";
import { apiFetch } from "./client";
import type { staffAccessTemplates } from "./staff-access-templates";
import {
  ProposalInputContract,
  ProposalOutputContract,
  ProposalHistoryContract,
  ProposalDecisionInputContract,
  ProposalDecisionRecordContract,
  type ProposalInput,
  type ProposalOutput,
  type ProposalDecisionInput,
} from "./staff-access-contract";

export { v7 as newStaffAssignmentId } from "uuid";
export type StaffAssignment = ProposalOutput;
export type StaffAssignmentInput = ProposalInput;
export type StaffAssignmentDecision = ProposalDecisionInput;
export type StaffAssignmentScope = { tenantId: string; subject: string };
export const staffAssignmentInputContract = ProposalInputContract.superRefine((value, ctx) => {
  const duration = Date.parse(value.expires_at) - Date.parse(value.valid_from);
  if (duration <= 0 || duration > 366 * 86400000)
    ctx.addIssue({
      code: "custom",
      path: ["expires_at"],
      message: "Choose an end after the start, within 366 days.",
    });
  if (Date.parse(value.expires_at) <= Date.now())
    ctx.addIssue({ code: "custom", path: ["expires_at"], message: "Choose a future expiry." });
  if (!value.reason.trim())
    ctx.addIssue({ code: "custom", path: ["reason"], message: "Enter a reason." });
});
export const staffAssignmentDecisionContract = ProposalDecisionInputContract;
const identifier = ProposalOutputContract.shape.id;
const pageInfo = z.object({ has_more: z.boolean(), next_cursor: z.string().nullable() }).strict();
const assignmentPage = z.object({ data: z.array(ProposalOutputContract), page: pageInfo }).strict();
const historyPage = z.object({ data: z.array(ProposalHistoryContract), page: pageInfo }).strict();
const decisionPage = z
  .object({ data: z.array(ProposalDecisionRecordContract), page: pageInfo })
  .strict();
export type StaffAssignmentFamily = (typeof staffAccessTemplates)[number]["family"];

type PageOptions = {
  family?: StaffAssignmentFamily;
  cursor?: string;
  signal?: AbortSignal;
};
const base = "/iam/assignment-proposals";
const assignmentLists: Record<StaffAssignmentFamily, string> = {
  leave: base,
  identity: "/iam/identity-assignment-proposals",
  intake: "/iam/intake-assignment-proposals",
  employment: "/iam/employment-assignment-proposals",
};
export const staffAssignmentsKey = (scope: StaffAssignmentScope) => [
  "iam",
  "staff-assignments",
  scope.tenantId,
  scope.subject,
];
function options(scope: StaffAssignmentScope): RequestInit {
  return {
    cache: "no-store",
    headers: {
      "X-Helivanta-Expected-Tenant": identifier.parse(scope.tenantId),
      "X-Helivanta-Expected-Subject": scope.subject,
    },
  };
}
function query(page: PageOptions) {
  const params = new URLSearchParams({ limit: "50" });
  if (page.cursor) params.set("cursor", page.cursor);
  return params.toString();
}
export async function listStaffAssignments(scope: StaffAssignmentScope, page: PageOptions = {}) {
  return assignmentPage.parse(
    await apiFetch(`${assignmentLists[page.family ?? "leave"]}?${query(page)}`, {
      ...options(scope),
      signal: page.signal,
    }),
  );
}
function owned(id: string, response: unknown) {
  const value = ProposalOutputContract.parse(response);
  if (value.id !== id) throw new Error("The assignment response did not match the proposal.");
  return value;
}
export async function proposeStaffAssignment(
  scope: StaffAssignmentScope,
  id: string,
  input: StaffAssignmentInput,
) {
  const body = staffAssignmentInputContract.parse(input);
  return owned(
    id,
    await apiFetch(`${base}/${identifier.parse(id)}`, {
      ...options(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}
export async function decideStaffAssignment(
  scope: StaffAssignmentScope,
  id: string,
  input: StaffAssignmentDecision,
) {
  const body = ProposalDecisionInputContract.parse(input);
  return owned(
    id,
    await apiFetch(`${base}/${identifier.parse(id)}/decision`, {
      ...options(scope),
      method: "POST",
      body: JSON.stringify(body),
    }),
  );
}
export function useStaffAssignments(
  scope: StaffAssignmentScope,
  family: StaffAssignmentFamily = "leave",
) {
  return useInfiniteQuery({
    queryKey: [...staffAssignmentsKey(scope), family],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      listStaffAssignments(scope, { cursor: pageParam, signal, family }),
    getNextPageParam: (last) =>
      last.page.has_more ? (last.page.next_cursor ?? undefined) : undefined,
    gcTime: 0,
  });
}
export function useStaffAssignmentHistory(scope: StaffAssignmentScope, id: string) {
  return useInfiniteQuery({
    queryKey: [...staffAssignmentsKey(scope), id, "history"],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam, signal }) =>
      historyPage.parse(
        await apiFetch(`${base}/${identifier.parse(id)}/history?${query({ cursor: pageParam })}`, {
          ...options(scope),
          signal,
        }),
      ),
    getNextPageParam: (last) =>
      last.page.has_more ? (last.page.next_cursor ?? undefined) : undefined,
    gcTime: 0,
  });
}
export function useStaffAssignmentDecisions(scope: StaffAssignmentScope, id: string) {
  return useInfiniteQuery({
    queryKey: [...staffAssignmentsKey(scope), id, "decisions"],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam, signal }) =>
      decisionPage.parse(
        await apiFetch(
          `${base}/${identifier.parse(id)}/decisions?${query({ cursor: pageParam })}`,
          { ...options(scope), signal },
        ),
      ),
    getNextPageParam: (last) =>
      last.page.has_more ? (last.page.next_cursor ?? undefined) : undefined,
    gcTime: 0,
  });
}
