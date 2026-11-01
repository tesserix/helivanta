import { z } from "zod";
import { apiFetch } from "./client";
import { EmploymentReviewSummaryContract } from "./employment-review-contract";
import { personReviewRequest, type PersonReviewScope } from "./person-reviews";
export type EmploymentReviewHospitalWorkspace = Pick<
  PersonReviewScope,
  "tenantId" | "subject" | "hospitalId"
> & { mode: "prepare" | "review" };
const pageContract = z
  .object({
    data: z.array(EmploymentReviewSummaryContract).max(200),
    page: z
      .object({ has_more: z.boolean(), next_cursor: z.string().min(1).max(2048).nullable() })
      .strict(),
  })
  .strict();
export async function listEmploymentReviewInbox(
  workspace: EmploymentReviewHospitalWorkspace,
  status: z.infer<typeof EmploymentReviewSummaryContract>["status"] = "pending",
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const mode = z.enum(["prepare", "review"]).parse(workspace.mode);
  const filter = EmploymentReviewSummaryContract.shape.status.parse(status);
  const query = new URLSearchParams(
    mode === "review" ? { employment_review: "v1" } : { hr_intake: "v1" },
  );
  query.set("limit", "50");
  query.set("status", filter);
  if (cursor !== null) query.set("cursor", z.string().min(1).max(2048).parse(cursor));
  const response = await apiFetch<unknown>(
    `/workforce/hospitals/${EmploymentReviewSummaryContract.shape.hospital_id.parse(workspace.hospitalId)}/employment-review-inbox?${query}`,
    { ...personReviewRequest(workspace), signal },
  );
  signal?.throwIfAborted();
  const parsed = pageContract.safeParse(response);
  if (
    !parsed.success ||
    parsed.data.page.has_more !== Boolean(parsed.data.page.next_cursor) ||
    new Set(parsed.data.data.map((row) => row.id)).size !== parsed.data.data.length ||
    parsed.data.data.some(
      (row) => row.hospital_id !== workspace.hospitalId || row.status !== filter,
    )
  )
    throw new Error(
      "The employment-review inbox could not be verified. Refresh before continuing.",
    );
  return parsed.data;
}
