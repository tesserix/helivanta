import { useInfiniteQuery } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetch, POLL_INTERVAL_MS } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import type { StaffAssignmentScope } from "./staff-assignments";
import {
  PartnerGrantDraftContract,
  PartnerGrantDraftInputContract,
  partnerGrantUUIDInputFields,
  partnerGrantTimestampNanos,
  type PartnerGrantDraftInput,
} from "./partner-grant-contract";

export { v7 as newPartnerGrantDraftId } from "uuid";
export type PartnerGrantDraftScope = StaffAssignmentScope;
const base = "/iam/partner-grant-drafts";
const identifier = z
  .uuid()
  .regex(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
export const partnerGrantDraftsKey = (scope: PartnerGrantDraftScope) => [
  "iam",
  "partner-grant-drafts",
  scope.tenantId,
  scope.subject,
];

function owned(scope: PartnerGrantDraftScope, response: unknown, id?: string) {
  const value = PartnerGrantDraftContract.parse(response);
  if (value.owner_tenant_id !== scope.tenantId || (id !== undefined && value.id !== id))
    throw new Error("The supplier access draft belongs to a different workspace or proposal.");
  return value;
}

function submitted(
  scope: PartnerGrantDraftScope,
  input: PartnerGrantDraftInput,
  value: ReturnType<typeof owned>,
) {
  if (value.prepared_by !== scope.subject)
    throw new Error("The recovered proposal was prepared by another account.");
  for (const key of Object.keys(input) as (keyof PartnerGrantDraftInput)[]) {
    const same =
      key === "valid_from" || key === "expires_at"
        ? partnerGrantTimestampNanos(value.input[key]) === partnerGrantTimestampNanos(input[key])
        : partnerGrantUUIDInputFields.has(key) &&
            typeof value.input[key] === "string" &&
            typeof input[key] === "string"
          ? value.input[key].toLowerCase() === input[key].toLowerCase()
          : value.input[key] === input[key];
    if (!same)
      throw new Error("The recovered supplier access draft did not match the retained intent.");
  }
  if (Object.keys(value.input).some((key) => !Object.hasOwn(input, key)))
    throw new Error("The recovered proposal contains a different recipient or scope.");
  return value;
}

export async function preparePartnerGrantDraft(
  scope: PartnerGrantDraftScope,
  id: string,
  input: PartnerGrantDraftInput,
) {
  const body = PartnerGrantDraftInputContract.parse(input);
  const result = await apiFetch(`${base}/${identifier.parse(id)}`, {
    ...membershipRequestOptions(scope),
    method: "PUT",
    body: JSON.stringify(body),
  });
  return submitted(scope, body, owned(scope, result, id));
}

export async function getPartnerGrantDraft(
  scope: PartnerGrantDraftScope,
  id: string,
  signal?: AbortSignal,
) {
  return owned(
    scope,
    await apiFetch(`${base}/${identifier.parse(id)}`, {
      ...membershipRequestOptions(scope),
      signal,
    }),
    id,
  );
}

export async function recoverPartnerGrantDraft(
  scope: PartnerGrantDraftScope,
  id: string,
  input: PartnerGrantDraftInput,
) {
  return submitted(
    scope,
    PartnerGrantDraftInputContract.parse(input),
    await getPartnerGrantDraft(scope, id),
  );
}

const page = z
  .object({
    data: z.array(PartnerGrantDraftContract),
    page: z.object({ has_more: z.boolean(), next_cursor: z.string().nullable() }).strict(),
  })
  .strict()
  .superRefine((value, ctx) => {
    if (
      value.page.has_more !==
        (typeof value.page.next_cursor === "string" && value.page.next_cursor.length > 0) ||
      new Set(value.data.map((d) => d.id)).size !== value.data.length
    )
      ctx.addIssue({
        code: "custom",
        path: ["page"],
        message: "The supplier access continuation is inconsistent.",
      });
  });

export function usePartnerGrantDrafts(scope: PartnerGrantDraftScope) {
  return useInfiniteQuery({
    queryKey: partnerGrantDraftsKey(scope),
    initialPageParam: null as string | null,
    gcTime: 0,
    staleTime: 0,
    refetchOnWindowFocus: true,
    refetchInterval: POLL_INTERVAL_MS,
    queryFn: async ({ pageParam, signal }) => {
      const query = new URLSearchParams({ limit: "25" });
      if (pageParam) query.set("cursor", pageParam);
      const result = page.parse(
        await apiFetch(`${base}?${query}`, { ...membershipRequestOptions(scope), signal }),
      );
      return { ...result, data: result.data.map((value) => owned(scope, value)) };
    },
    getNextPageParam: (last) => (last.page.has_more ? last.page.next_cursor : undefined),
  });
}
