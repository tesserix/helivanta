import { z } from "zod";
import { apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import {
  OperatingDutyContract,
  OperatingDutyInputContract,
} from "./operating-assignments-contract";
import type { OperatingDutyStaffScope } from "./operating-duties";

export type OperatingDutyInboxScope = Omit<
  OperatingDutyStaffScope,
  "staffId" | "administration"
> & {
  administration: "governance";
};
const Scope = z
  .object({
    tenantId: z.guid().refine((id) => id !== "00000000-0000-0000-0000-000000000000"),
    subject: OperatingDutyInputContract.shape.subject,
    employerKind: z.enum(["pharmacy", "supplier"]),
    employerId: z.uuidv7(),
    administration: z.literal("governance"),
  })
  .strict();
const Page = z
  .object({
    data: z.array(OperatingDutyContract).max(20),
    page: z
      .object({ has_more: z.boolean(), next_cursor: z.string().min(1).max(2048).nullable() })
      .strict(),
  })
  .strict();
export const operatingDutyInboxKey = (
  scope: Pick<OperatingDutyStaffScope, "tenantId" | "subject" | "employerKind" | "employerId">,
) =>
  [
    "iam",
    "operating-duty-inbox",
    scope.tenantId,
    scope.subject,
    scope.employerKind,
    scope.employerId,
  ] as const;

export async function listOperatingDutyInbox(
  scope: OperatingDutyInboxScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const selected = Scope.parse(scope);
  signal?.throwIfAborted();
  const params = new URLSearchParams({ status: "all", limit: "20" });
  if (cursor !== null) {
    if (!cursor || cursor.length > 2048) throw new Error("Invalid retained-duty continuation.");
    params.set("cursor", cursor);
  }
  const response = await apiFetch<unknown>(
    `/iam/operating-governance-employers/${selected.employerKind}/${selected.employerId}/duty-inbox?${params}`,
    { ...membershipRequestOptions(selected), signal },
  );
  signal?.throwIfAborted();
  const page = Page.parse(response);
  if (
    page.page.has_more !== (page.page.next_cursor !== null) ||
    (page.page.has_more && page.data.length !== 20)
  )
    throw new Error("Invalid retained-duty page evidence.");
  const seen = new Set<string>();
  for (const duty of page.data) {
    if (
      duty.owner_tenant_id !== selected.tenantId ||
      duty.input.scope.employer_kind !== selected.employerKind ||
      duty.input.scope.employer_id !== selected.employerId ||
      seen.has(duty.id)
    )
      throw new Error("The retained-duty owner page changed scope.");
    seen.add(duty.id);
  }
  return page;
}
