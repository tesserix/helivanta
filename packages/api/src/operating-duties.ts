import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetch, POLL_INTERVAL_MS } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import {
  OperatingDutyContract,
  OperatingDutyInputContract,
  OperatingDutyDecisionInputContract,
  OperatingDutyDecisionContract,
  type OperatingDutyInput,
  type OperatingDutyDecisionInput,
} from "./operating-assignments-contract";
export * from "./operating-assignments-contract";
export { v7 as newOperatingDutyId } from "uuid";
export type OperatingDutyStaffScope = {
  administration?: "legacy" | "governance";
  tenantId: string;
  subject: string;
  employerKind: "supplier" | "pharmacy";
  employerId: string;
  staffId: string;
};
const id = z.uuid().regex(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
const staffId = z
  .string()
  .guid()
  .refine((v) => v !== "00000000-0000-0000-0000-000000000000" && v === v.toLowerCase());
const administration = (s: OperatingDutyStaffScope) =>
  z.enum(["legacy", "governance"]).default("legacy").parse(s.administration);
const employer = (s: OperatingDutyStaffScope) =>
  `${z.enum(["supplier", "pharmacy"]).parse(s.employerKind)}/${id.parse(s.employerId)}`;
const base = (s: OperatingDutyStaffScope, duty: string) =>
  administration(s) === "governance"
    ? `/iam/operating-governance-employers/${employer(s)}/duties/${id.parse(duty)}`
    : `/iam/operating-duties/${id.parse(duty)}`;
export const operatingDutiesKey = (s: OperatingDutyStaffScope) => [
  "iam",
  "operating-duties",
  s.tenantId,
  s.subject,
  s.employerKind,
  s.employerId,
  s.staffId,
  administration(s),
];
function within(s: OperatingDutyStaffScope, input: OperatingDutyInput) {
  if (
    input.scope.employer_kind !== s.employerKind ||
    input.scope.employer_id !== s.employerId ||
    input.identity.staff_id !== s.staffId
  )
    throw new Error("The duty belongs to another employer or Staff record.");
}
function owned(s: OperatingDutyStaffScope, value: unknown, duty?: string) {
  const row = OperatingDutyContract.parse(value);
  if (row.owner_tenant_id !== s.tenantId || (duty !== undefined && row.id !== duty))
    throw new Error("The duty belongs to another workspace or record.");
  within(s, row.input);
  return row;
}
function instant(value: string) {
  const fraction = value.match(/\.(\d+)/)?.[1] ?? "";
  return (
    BigInt(Math.floor(Date.parse(value) / 1000)) * 1000000n +
    BigInt(fraction.slice(0, 6).padEnd(6, "0"))
  );
}
function canonical(input: OperatingDutyInput) {
  return JSON.stringify({
    ...input,
    valid_from: String(instant(input.valid_from)),
    expires_at: String(instant(input.expires_at)),
  });
}
function proposal(
  s: OperatingDutyStaffScope,
  duty: string,
  input: OperatingDutyInput,
  value: unknown,
) {
  const row = owned(s, value, duty);
  if (
    row.prepared_by !== s.subject ||
    row.status !== "pending_review" ||
    row.version !== 1 ||
    canonical(row.input) !== canonical(input)
  )
    throw new Error("The recovered duty does not match the original proposal and account.");
  return row;
}
export async function prepareOperatingDuty(
  s: OperatingDutyStaffScope,
  duty: string,
  input: OperatingDutyInput,
) {
  const body = OperatingDutyInputContract.parse(input);
  within(s, body);
  return proposal(
    s,
    duty,
    body,
    await apiFetch(base(s, duty), {
      ...membershipRequestOptions(s),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}
export async function recoverOperatingDutyProposal(
  s: OperatingDutyStaffScope,
  duty: string,
  input: OperatingDutyInput,
) {
  const body = OperatingDutyInputContract.parse(input);
  within(s, body);
  return proposal(
    s,
    duty,
    body,
    await apiFetch(`${base(s, duty)}/proposal-receipt`, membershipRequestOptions(s)),
  );
}
export async function getOperatingDuty(
  s: OperatingDutyStaffScope,
  duty: string,
  signal?: AbortSignal,
) {
  return owned(s, await apiFetch(base(s, duty), { ...membershipRequestOptions(s), signal }), duty);
}
function decision(
  s: OperatingDutyStaffScope,
  duty: string,
  command: string,
  input: OperatingDutyDecisionInput,
  value: unknown,
) {
  const row = OperatingDutyDecisionContract.parse(value);
  const result = owned(s, row.result, duty);
  const status = {
    approve: "approved_pending_activation",
    reject: "rejected",
    withdraw: "withdrawn",
    revoke: "revoked",
  }[input.decision];
  if (
    row.id !== command ||
    row.duty_id !== duty ||
    row.actor_subject !== s.subject ||
    JSON.stringify(row.input) !== JSON.stringify(input) ||
    result.version !== input.expected_version + 1 ||
    result.status !== status
  )
    throw new Error("The recovered decision does not match the original review and account.");
  return row;
}
export async function decideOperatingDuty(
  s: OperatingDutyStaffScope,
  duty: string,
  command: string,
  input: OperatingDutyDecisionInput,
) {
  const body = OperatingDutyDecisionInputContract.parse(input);
  return decision(
    s,
    duty,
    command,
    body,
    await apiFetch(`${base(s, duty)}/commands/${id.parse(command)}`, {
      ...membershipRequestOptions(s),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}
export async function recoverOperatingDutyCommand(
  s: OperatingDutyStaffScope,
  duty: string,
  command: string,
  input: OperatingDutyDecisionInput,
) {
  const body = OperatingDutyDecisionInputContract.parse(input);
  return decision(
    s,
    duty,
    command,
    body,
    await apiFetch(`${base(s, duty)}/commands/${id.parse(command)}`, membershipRequestOptions(s)),
  );
}
const pageContract = z
  .object({
    data: z.array(OperatingDutyContract).max(25),
    page: z
      .object({ has_more: z.boolean(), next_cursor: z.string().min(1).max(4096).nullable() })
      .strict(),
  })
  .strict()
  .superRefine((v, c) => {
    if (
      v.page.has_more !== (v.page.next_cursor !== null) ||
      new Set(v.data.map((r) => r.id)).size !== v.data.length
    )
      c.addIssue({ code: "custom", message: "The duty page is inconsistent." });
  });
export async function listOperatingDuties(
  s: OperatingDutyStaffScope,
  cursor: string | null,
  signal?: AbortSignal,
) {
  const params = new URLSearchParams({ limit: "25" });
  if (cursor) params.set("cursor", cursor);
  const segment =
    administration(s) === "governance" ? "operating-governance-employers" : "operating-employers";
  const path = `/iam/${segment}/${employer(s)}/staff/${staffId.parse(s.staffId)}/duties`;
  const page = pageContract.parse(
    await apiFetch(`${path}?${params}`, { ...membershipRequestOptions(s), signal }),
  );
  return { ...page, data: page.data.map((r) => owned(s, r)) };
}
export function useOperatingDuties(
  s: OperatingDutyStaffScope,
  cursor: string | null,
  enabled: boolean,
) {
  return useQuery({
    queryKey: [...operatingDutiesKey(s), cursor],
    queryFn: ({ signal }) => listOperatingDuties(s, cursor, signal),
    enabled,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: true,
    refetchInterval: POLL_INTERVAL_MS,
  });
}
