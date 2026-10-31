import { z } from "zod";
import { apiFetch } from "./client";
import {
  IdentitySourceContract,
  IdentitySourceProjectionQuery,
  IdentitySourceDraftProjection,
} from "./identity-checks-contract";
import {
  PersonReviewEvidenceSelectionInputContract,
  PersonReviewEvidenceOptionContract,
  type PersonReviewEvidenceSelectionInput,
} from "./person-review-contract";
import { personReviewRequest, type PersonReviewScope } from "./person-reviews";

const evidencePage = z
  .object({
    data: z.array(PersonReviewEvidenceOptionContract).max(200),
    page: z
      .object({ has_more: z.boolean(), next_cursor: z.string().min(1).max(2048).nullable() })
      .strict(),
  })
  .strict();
const identifier = PersonReviewEvidenceOptionContract.shape.staff_id;

export async function searchPersonReviewEvidence(
  scope: PersonReviewScope,
  source: PersonReviewEvidenceSelectionInput,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const input = PersonReviewEvidenceSelectionInputContract.parse(source);
  const query = new URLSearchParams({ limit: "50" });
  if (cursor !== null) query.set("cursor", z.string().min(1).max(2048).parse(cursor));
  const response = await apiFetch<unknown>(
    `/workforce/hospitals/${identifier.parse(scope.hospitalId)}/identity-staff/${identifier.parse(scope.staffId)}/person-review-evidence/search?${query}`,
    {
      ...personReviewRequest(scope),
      method: "POST",
      body: JSON.stringify(input),
      signal,
    },
  );
  signal?.throwIfAborted();
  const parsed = evidencePage.safeParse(response);
  if (
    !parsed.success ||
    parsed.data.page.has_more !== Boolean(parsed.data.page.next_cursor) ||
    parsed.data.data.some(
      (file) =>
        file.hospital_id !== scope.hospitalId ||
        file.staff_id !== scope.staffId ||
        file.record_version !== input.staff_version,
    )
  ) {
    throw new Error(
      "The identity evidence list could not be verified. Refresh Staff details before continuing.",
    );
  }
  return parsed.data;
}

export async function getPersonReviewPreparationSource(
  scope: PersonReviewScope,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const response = await apiFetch<unknown>(
    `/workforce/hospitals/${identifier.parse(scope.hospitalId)}/identity-staff/${identifier.parse(scope.staffId)}?${IdentitySourceProjectionQuery}=${IdentitySourceDraftProjection}`,
    { ...personReviewRequest(scope), signal },
  );
  signal?.throwIfAborted();
  const parsed = IdentitySourceContract.safeParse(response);
  if (
    !parsed.success ||
    parsed.data.hospital_id !== scope.hospitalId ||
    parsed.data.staff_id !== scope.staffId ||
    parsed.data.manage_checks_allowed !== true
  )
    throw new Error(
      "The identity preparation basis could not be verified. Refresh access before continuing.",
    );
  return parsed.data;
}
