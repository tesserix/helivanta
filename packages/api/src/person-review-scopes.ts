import { z } from "zod";
import { apiFetch } from "./client";
import { PersonReviewHospitalScopeContract } from "./person-review-contract";
import { personReviewRequest, type PersonReviewScope } from "./person-reviews";

const pageContract = z
  .object({
    data: z.array(PersonReviewHospitalScopeContract).max(20),
    page: z
      .object({
        has_more: z.boolean(),
        next_cursor: z.string().min(1).max(2048).nullable(),
      })
      .strict(),
  })
  .strict();

export async function listPersonReviewScopes(
  workspace: Pick<PersonReviewScope, "tenantId" | "subject">,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const query = new URLSearchParams({ limit: "20" });
  if (cursor !== null) query.set("cursor", z.string().min(1).max(2048).parse(cursor));
  const response = await apiFetch<unknown>(`/workforce/person-review-scopes?${query}`, {
    ...personReviewRequest(workspace),
    signal,
  });
  signal?.throwIfAborted();
  const parsed = pageContract.safeParse(response);
  if (
    !parsed.success ||
    parsed.data.page.has_more !== Boolean(parsed.data.page.next_cursor) ||
    parsed.data.data.some((row) => !row.can_submit && !row.can_review)
  )
    throw new Error(
      "The person-review hospital list could not be verified. Refresh before continuing.",
    );
  return parsed.data;
}
