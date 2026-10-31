import { z } from "zod";
import { apiFetch } from "./client";
import {
  PersonReviewContract,
  PersonReviewTargetSearchInputContract,
  PersonReviewTargetContract,
  PersonReviewSummaryContract,
} from "./person-review-contract";
import { personReviewRequest, type PersonReviewScope } from "./person-reviews";

export type PersonReviewHospitalWorkspace = Pick<
  PersonReviewScope,
  "tenantId" | "subject" | "hospitalId"
>;
const pageInfo = z
  .object({
    has_more: z.boolean(),
    next_cursor: z.string().min(1).max(2048).nullable(),
  })
  .strict();
const targetPage = z
  .object({ data: z.array(PersonReviewTargetContract).max(200), page: pageInfo })
  .strict();
const inboxPage = z
  .object({ data: z.array(PersonReviewSummaryContract).max(200), page: pageInfo })
  .strict();
const identifier = PersonReviewContract.shape.id;
const browseError = "The person-review list could not be verified. Refresh before continuing.";

function browseQuery(cursor: string | null) {
  const query = new URLSearchParams({ limit: "50" });
  if (cursor !== null) query.set("cursor", z.string().min(1).max(2048).parse(cursor));
  return query;
}

export async function searchPersonReviewTargets(
  workspace: PersonReviewHospitalWorkspace,
  employeeCode: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const input = PersonReviewTargetSearchInputContract.parse({ employee_code: employeeCode });
  const response = await apiFetch<unknown>(
    `/workforce/hospitals/${identifier.parse(workspace.hospitalId)}/person-review-targets/search?${browseQuery(cursor)}`,
    {
      ...personReviewRequest(workspace),
      method: "POST",
      body: JSON.stringify(input),
      signal,
    },
  );
  signal?.throwIfAborted();
  const parsed = targetPage.safeParse(response);
  if (
    !parsed.success ||
    parsed.data.page.has_more !== Boolean(parsed.data.page.next_cursor) ||
    parsed.data.data.some(
      (row) =>
        row.hospital_id !== workspace.hospitalId ||
        (input.employee_code !== "" && row.employee_code !== input.employee_code.toUpperCase()),
    )
  )
    throw new Error(browseError);
  return parsed.data;
}

export async function listPersonReviewInbox(
  workspace: PersonReviewHospitalWorkspace,
  status: z.infer<typeof PersonReviewSummaryContract>["status"] = "pending",
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const query = browseQuery(cursor);
  const filter = PersonReviewSummaryContract.shape.status.parse(status);
  query.set("status", filter);
  const response = await apiFetch<unknown>(
    `/workforce/hospitals/${identifier.parse(workspace.hospitalId)}/person-review-inbox?${query}`,
    {
      ...personReviewRequest(workspace),
      signal,
    },
  );
  signal?.throwIfAborted();
  const parsed = inboxPage.safeParse(response);
  if (
    !parsed.success ||
    parsed.data.page.has_more !== Boolean(parsed.data.page.next_cursor) ||
    parsed.data.data.some(
      (row) => row.hospital_id !== workspace.hospitalId || row.status !== filter,
    )
  )
    throw new Error(browseError);
  return parsed.data;
}
