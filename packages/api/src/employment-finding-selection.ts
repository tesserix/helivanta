import { z } from "zod";
import { apiFetch } from "./client";
import { personReviewRequest } from "./person-reviews";
import {
  EmploymentPersonFindingOptionContract,
  EmploymentPersonFindingSearchInputContract,
} from "./employment-review-contract";
import type { EmploymentPersonFindingSearchInput } from "./employment-review-contract";
import type { EmploymentReviewPreparationScope } from "./employment-reviews";
const pageContract = z
  .object({
    data: z.array(EmploymentPersonFindingOptionContract).max(20),
    page: z
      .object({ has_more: z.boolean(), next_cursor: z.string().min(1).max(2048).nullable() })
      .strict(),
  })
  .strict();
export async function listEmploymentPersonFindings(
  scope: EmploymentReviewPreparationScope,
  basis: EmploymentPersonFindingSearchInput,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  if (scope.hrIntake !== true) throw new Error("Verify the original HR preparation workspace.");
  const source = EmploymentPersonFindingSearchInputContract.parse(basis);
  const query = new URLSearchParams({
    hr_intake: "v1",
    staff_version: String(source.staff_version),
    employment_epoch: String(source.employment_epoch),
    limit: "20",
  });
  if (cursor !== null) query.set("cursor", z.string().min(1).max(2048).parse(cursor));
  const id = EmploymentPersonFindingOptionContract.shape.id;
  const response = await apiFetch<unknown>(
    `/workforce/hospitals/${id.parse(scope.hospitalId)}/staff/${id.parse(scope.staffId)}/employment-person-findings?${query}`,
    { ...personReviewRequest(scope), signal },
  );
  signal?.throwIfAborted();
  const parsed = pageContract.safeParse(response);
  if (
    !parsed.success ||
    parsed.data.page.has_more !== Boolean(parsed.data.page.next_cursor) ||
    new Set(parsed.data.data.map((row) => row.id)).size !== parsed.data.data.length ||
    parsed.data.data.some(
      (row) =>
        row.hospital_id !== scope.hospitalId ||
        row.staff_id !== scope.staffId ||
        row.employment_epoch !== source.employment_epoch ||
        row.version !== 2 ||
        (row.access_intent === "digital") !== Boolean(row.declared_subject),
    )
  )
    throw new Error(
      "The approved person findings could not be verified. Refresh before continuing.",
    );
  return parsed.data;
}
