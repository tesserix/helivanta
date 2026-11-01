import { z } from "zod";
import { apiFetch, apiFetchResponse } from "./client";
import { verifiedReviewEvidenceBlob } from "./review-evidence-content";
import type { IdentityCaseScope } from "./identity-checks";
import {
  PersonReviewContract,
  PersonReviewViewContract,
  PersonReviewSubmissionInputContract,
  PersonReviewDecisionInputContract,
  PersonReviewActionInputContract,
  type PersonReview,
  type PersonReviewSubmissionInput,
  type PersonReviewDecisionInput,
  type PersonReviewActionInput,
  type PersonReviewView,
} from "./person-review-contract";

export { v7 as newPersonReviewId } from "uuid";
export type PersonReviewScope = IdentityCaseScope;
const identifier = PersonReviewContract.shape.id;
const receiptError =
  "Could not verify the person-review receipt. Check history before starting another command.";

export const personReviewsKey = (scope: PersonReviewScope) => [
  "workforce",
  "person-reviews",
  scope.tenantId,
  scope.subject,
  scope.hospitalId,
  scope.staffId,
];

export const personReviewsPath = (scope: PersonReviewScope) =>
  `/workforce/hospitals/${identifier.parse(scope.hospitalId)}/staff/${identifier.parse(scope.staffId)}/person-reviews`;

export function personReviewRequest(
  scope: Pick<PersonReviewScope, "tenantId" | "subject">,
): RequestInit {
  if (!scope.subject || scope.subject.length > 200 || scope.subject.trim() !== scope.subject)
    throw new Error("Verify the original Staff workspace before continuing.");
  return {
    cache: "no-store",
    headers: {
      "X-Helivanta-Expected-Tenant": identifier.parse(scope.tenantId),
      "X-Helivanta-Expected-Subject": scope.subject,
    },
  };
}

function owned(scope: PersonReviewScope, id: string, value: unknown): PersonReview {
  const parsed = PersonReviewContract.safeParse(value);
  if (
    !parsed.success ||
    parsed.data.id !== id ||
    parsed.data.hospital_id !== scope.hospitalId ||
    parsed.data.staff_id !== scope.staffId ||
    (parsed.data.status === "approved" &&
      (!parsed.data.identity_examined ||
        !parsed.data.known_associations_disclosed ||
        !parsed.data.reviewer_identity_examined ||
        !parsed.data.independent_person_confirmed ||
        parsed.data.actor_subject === parsed.data.proposed_by_subject ||
        !parsed.data.decision_reason.trim()))
  )
    throw new Error(receiptError);
  return parsed.data;
}

function commandReceipt(
  scope: PersonReviewScope,
  id: string,
  value: unknown,
  command: string,
  version: number,
  status: PersonReview["status"],
): PersonReview {
  const row = owned(scope, id, value);
  if (
    row.revision_id !== command ||
    row.version !== version ||
    row.status !== status ||
    row.actor_subject !== scope.subject
  )
    throw new Error(receiptError);
  return row;
}

export async function submitPersonReview(
  scope: PersonReviewScope,
  id: string,
  input: PersonReviewSubmissionInput,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const command = PersonReviewSubmissionInputContract.parse(input);
  if (
    !command.identity_examined ||
    !command.known_associations_disclosed ||
    (command.access_intent === "non_digital"
      ? command.declared_subject !== ""
      : command.declared_subject === "")
  )
    throw new Error("Complete the human examination and account intent before submitting.");
  const value = await apiFetch<unknown>(`${personReviewsPath(scope)}/${identifier.parse(id)}`, {
    ...personReviewRequest(scope),
    method: "PUT",
    body: JSON.stringify(command),
    signal,
  });
  signal?.throwIfAborted();
  const { command_id, document_ids: ids, valid_until: validity, ...source } = command;
  const row = commandReceipt(scope, id, value, command_id, 1, "pending");
  const expectedFiles = [...ids].sort();
  const actualFiles = row.evidence.map((file) => file.document_id).sort();
  if (
    row.proposed_by_subject !== scope.subject ||
    Object.entries(source).some(([key, expected]) => row[key as keyof PersonReview] !== expected) ||
    Date.parse(row.valid_until) !== Date.parse(validity) ||
    JSON.stringify(actualFiles) !== JSON.stringify(expectedFiles) ||
    row.evidence.some((file) => file.record_version !== command.staff_version)
  )
    throw new Error(receiptError);
  return row;
}

function verifiedPersonView(scope: PersonReviewScope, id: string, response: unknown) {
  const parsed = PersonReviewViewContract.safeParse(response);
  if (!parsed.success)
    throw new Error(
      "Could not verify the person-review current state. Refresh before relying on it.",
    );
  const value = parsed.data;
  owned(scope, id, value.review);
  const derived = ["expired", "source_changed", "evidence_unavailable"].includes(
    value.current_state,
  );
  if (
    (derived && !["pending", "approved"].includes(value.review.status)) ||
    (!derived && value.current_state !== value.review.status)
  )
    throw new Error(
      "Could not verify the person-review current state. Refresh before relying on it.",
    );
  return value;
}

export async function getPersonReview(scope: PersonReviewScope, id: string, signal?: AbortSignal) {
  signal?.throwIfAborted();
  const response = await apiFetch<unknown>(`${personReviewsPath(scope)}/${identifier.parse(id)}`, {
    ...personReviewRequest(scope),
    signal,
  });
  signal?.throwIfAborted();
  return verifiedPersonView(scope, id, response);
}

export async function downloadPersonReviewEvidence(
  scope: PersonReviewScope,
  id: string,
  view: PersonReviewView,
  fileId: string,
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  const current = verifiedPersonView(scope, id, view);
  const file = current.review.evidence.find((entry) => entry.document_id === fileId);
  if (!file || !["pending", "approved"].includes(current.current_state))
    throw new Error(
      "Evidence is not available in this review workspace. Refresh before continuing.",
    );
  const response = await apiFetchResponse(
    `${personReviewsPath(scope)}/${identifier.parse(id)}/evidence/${identifier.parse(fileId)}/content`,
    { ...personReviewRequest(scope), signal },
  );
  return verifiedReviewEvidenceBlob(response, file, signal);
}

export async function decidePersonReview(
  scope: PersonReviewScope,
  id: string,
  input: PersonReviewDecisionInput,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const command = PersonReviewDecisionInputContract.parse(input);
  if (
    !command.independent_person_confirmed ||
    (command.decision === "approve" && !command.identity_examined)
  )
    throw new Error("Confirm an independent examination before approving a person finding.");
  const value = await apiFetch<unknown>(
    `${personReviewsPath(scope)}/${identifier.parse(id)}/decision`,
    {
      ...personReviewRequest(scope),
      method: "POST",
      body: JSON.stringify(command),
      signal,
    },
  );
  signal?.throwIfAborted();
  const row = commandReceipt(
    scope,
    id,
    value,
    command.command_id,
    command.expected_version + 1,
    command.decision === "approve" ? "approved" : "rejected",
  );
  if (
    row.decision_reason !== command.reason ||
    row.reviewer_identity_examined !== command.identity_examined ||
    row.independent_person_confirmed !== command.independent_person_confirmed
  )
    throw new Error(receiptError);
  return row;
}

async function endPersonReview(
  scope: PersonReviewScope,
  id: string,
  input: PersonReviewActionInput,
  action: "withdrawal" | "revocation",
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const command = PersonReviewActionInputContract.parse(input);
  const value = await apiFetch<unknown>(
    `${personReviewsPath(scope)}/${identifier.parse(id)}/${action}`,
    {
      ...personReviewRequest(scope),
      method: "POST",
      body: JSON.stringify(command),
      signal,
    },
  );
  signal?.throwIfAborted();
  const row = commandReceipt(
    scope,
    id,
    value,
    command.command_id,
    command.expected_version + 1,
    action === "withdrawal" ? "withdrawn" : "revoked",
  );
  if (row.decision_reason !== command.reason) throw new Error(receiptError);
  return row;
}

export const withdrawPersonReview = (
  scope: PersonReviewScope,
  id: string,
  input: PersonReviewActionInput,
  signal?: AbortSignal,
) => endPersonReview(scope, id, input, "withdrawal", signal);
export const revokePersonReview = (
  scope: PersonReviewScope,
  id: string,
  input: PersonReviewActionInput,
  signal?: AbortSignal,
) => endPersonReview(scope, id, input, "revocation", signal);

const personReviewHistoryPage = z
  .object({
    data: z.array(PersonReviewContract).max(200),
    page: z
      .object({ has_more: z.boolean(), next_cursor: z.string().min(1).max(2048).nullable() })
      .strict(),
  })
  .strict();
export async function listPersonReviewHistory(
  scope: PersonReviewScope,
  id: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const query = new URLSearchParams({ limit: "50" });
  if (cursor !== null) query.set("cursor", z.string().min(1).max(2048).parse(cursor));
  const response = await apiFetch<unknown>(
    `${personReviewsPath(scope)}/${identifier.parse(id)}/history?${query}`,
    { ...personReviewRequest(scope), signal },
  );
  signal?.throwIfAborted();
  const parsed = personReviewHistoryPage.safeParse(response);
  const message = "The person-review history could not be verified. Refresh before relying on it.";
  if (!parsed.success || parsed.data.page.has_more !== Boolean(parsed.data.page.next_cursor))
    throw new Error(message);
  const rows = parsed.data.data;
  if (
    new Set(rows.map((row) => row.revision_id)).size !== rows.length ||
    rows.some((row, index) => {
      const previous = rows[index - 1];
      return previous !== undefined && row.version >= previous.version;
    })
  )
    throw new Error(message);
  try {
    for (const row of rows) owned(scope, id, row);
  } catch {
    throw new Error(message);
  }
  return parsed.data;
}
