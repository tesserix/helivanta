import { apiFetch } from "./client";
import { personReviewRequest as staffReviewRequest } from "./person-reviews";
import type { EmploymentPeriodScope } from "./employment-periods";
import {
  EmploymentReviewContract,
  EmploymentReviewSubmissionInputContract,
  EmploymentReviewDecisionInputContract,
  EmploymentReviewActionInputContract,
  EmploymentReviewViewContract,
  type EmploymentReview,
  type EmploymentReviewSubmissionInput,
  type EmploymentReviewDecisionInput,
  type EmploymentReviewActionInput,
} from "./employment-review-contract";

export { v7 as newEmploymentReviewId } from "uuid";
type EmploymentReviewWorkspace = Omit<EmploymentPeriodScope, "hrIntake">;
export type EmploymentReviewPreparationScope = EmploymentReviewWorkspace & {
  hrIntake: true;
  employmentReview?: never;
};
export type EmploymentReviewReviewerScope = EmploymentReviewWorkspace & {
  employmentReview: true;
  hrIntake?: never;
};
export type EmploymentReviewScope =
  EmploymentReviewPreparationScope | EmploymentReviewReviewerScope;
function reviewerMode(scope: EmploymentReviewScope): boolean {
  if ((scope.hrIntake === true) === (scope.employmentReview === true))
    throw new Error(
      "Verify one explicit Staff preparation or reviewer workspace before continuing.",
    );
  return scope.employmentReview === true;
}
const identifier = EmploymentReviewContract.shape.id;
const receiptError =
  "Could not verify the employment submission receipt. Check the original record before another command.";

export const employmentReviewsKey = (scope: EmploymentReviewScope) => [
  "workforce",
  "employment-reviews",
  scope.tenantId,
  scope.subject,
  scope.hospitalId,
  scope.staffId,
  reviewerMode(scope) ? "employment-review-v1" : "hr-intake-v1",
];
export function employmentReviewsPath(scope: EmploymentReviewScope, id: string) {
  const query = reviewerMode(scope) ? "employment_review=v1" : "hr_intake=v1";
  return `/workforce/hospitals/${identifier.parse(scope.hospitalId)}/staff/${identifier.parse(scope.staffId)}/employment-reviews/${identifier.parse(id)}?${query}`;
}
function owned(scope: EmploymentReviewScope, id: string, value: unknown): EmploymentReview {
  const parsed = EmploymentReviewContract.safeParse(value);
  if (!parsed.success) throw new Error(receiptError);
  const row = parsed.data;
  const terms = row.terms;
  const bindingIds = row.evidence.map(({ binding }) => binding.id);
  const fileIds = row.evidence.map(({ binding }) => binding.document_id);
  if (
    row.id !== id ||
    row.hospital_id !== scope.hospitalId ||
    row.staff_id !== scope.staffId ||
    (row.status === "pending"
      ? row.version !== 1 ||
        row.actor_subject !== row.proposed_by_subject ||
        row.decision_reason !== "" ||
        row.reviewer_terms_examined ||
        row.independent_person_confirmed
      : row.status === "approved" || row.status === "rejected"
        ? row.version !== 2 ||
          row.actor_subject === row.proposed_by_subject ||
          row.actor_subject === row.declared_subject ||
          !row.decision_reason.trim() ||
          !row.reviewer_terms_examined ||
          !row.independent_person_confirmed
        : row.status === "withdrawn"
          ? row.version !== 2 ||
            row.actor_subject !== row.proposed_by_subject ||
            !row.decision_reason.trim() ||
            row.reviewer_terms_examined ||
            row.independent_person_confirmed
          : row.version !== 3 ||
            row.actor_subject === row.proposed_by_subject ||
            row.actor_subject === row.declared_subject ||
            !row.decision_reason.trim() ||
            row.reviewer_terms_examined ||
            row.independent_person_confirmed) ||
    !row.terms_examined ||
    !row.known_associations_disclosed ||
    (row.access_intent === "non_digital"
      ? row.declared_subject !== ""
      : row.declared_subject === "") ||
    terms.id !== row.period_id ||
    terms.revision_id !== row.period_revision_id ||
    terms.version !== row.period_version ||
    terms.status !== "draft" ||
    terms.hospital_id !== row.hospital_id ||
    terms.staff_id !== row.staff_id ||
    terms.staff_version !== row.staff_version ||
    terms.employment_epoch !== row.employment_epoch ||
    new Set(bindingIds).size !== bindingIds.length ||
    new Set(fileIds).size !== fileIds.length ||
    row.evidence.some(
      ({ binding, classification_version: version, classification_revision_id: revision }) =>
        binding.hospital_id !== row.hospital_id ||
        binding.staff_id !== row.staff_id ||
        binding.period_id !== row.period_id ||
        binding.period_revision_id !== row.period_revision_id ||
        binding.period_version !== row.period_version ||
        binding.staff_version !== row.staff_version ||
        binding.document_record_version !== row.staff_version ||
        binding.employment_epoch !== row.employment_epoch ||
        binding.status !== "attached" ||
        (version === 0 ? revision !== "" : !identifier.safeParse(revision).success),
    )
  )
    throw new Error(receiptError);
  return row;
}
export async function submitEmploymentReview(
  scope: EmploymentReviewPreparationScope,
  id: string,
  input: EmploymentReviewSubmissionInput,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  if (reviewerMode(scope)) throw new Error("Current preparation authority is required.");
  const command = EmploymentReviewSubmissionInputContract.parse(input);
  if (
    !command.terms_examined ||
    !command.known_associations_disclosed ||
    new Set(command.evidence.map((selection) => selection.id)).size !== command.evidence.length
  )
    throw new Error("Examine the terms and select distinct evidence before submitting.");
  const response = await apiFetch<unknown>(employmentReviewsPath(scope, id), {
    ...staffReviewRequest(scope),
    method: "PUT",
    body: JSON.stringify(command),
    signal,
  });
  signal?.throwIfAborted();
  const row = owned(scope, id, response);
  const { command_id, evidence, ...source } = command;
  if (
    row.status !== "pending" ||
    row.version !== 1 ||
    row.revision_id !== command_id ||
    row.actor_subject !== scope.subject ||
    row.proposed_by_subject !== scope.subject ||
    Object.entries(source).some(
      ([key, expected]) => row[key as keyof EmploymentReview] !== expected,
    ) ||
    evidence.length !== row.evidence.length ||
    evidence.some(
      (selection) =>
        !row.evidence.some(
          ({ binding }) =>
            binding.id === selection.id &&
            binding.revision_id === selection.revision_id &&
            binding.version === selection.version,
        ),
    )
  )
    throw new Error(receiptError);
  return row;
}
export async function getEmploymentReview(
  scope: EmploymentReviewScope,
  id: string,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const response = await apiFetch<unknown>(employmentReviewsPath(scope, id), {
    ...staffReviewRequest(scope),
    signal,
  });
  signal?.throwIfAborted();
  const parsed = EmploymentReviewViewContract.safeParse(response);
  if (!parsed.success)
    throw new Error(
      "Could not verify current employment preparation. Refresh before relying on it.",
    );
  const row = owned(scope, id, parsed.data.review);
  const view = parsed.data;
  const reviewer = reviewerMode(scope);
  const independent =
    scope.subject !== row.proposed_by_subject && scope.subject !== row.declared_subject;
  const closed =
    row.status === "rejected" || row.status === "withdrawn" || row.status === "revoked";
  if (
    (closed
      ? view.current_state !== row.status
      : view.current_state !== row.status &&
        !["source_changed", "person_unavailable", "evidence_unavailable"].includes(
          view.current_state,
        )) ||
    (view.can_approve &&
      (!reviewer ||
        !independent ||
        row.status !== "pending" ||
        view.current_state !== "pending")) ||
    (view.can_reject && (!reviewer || !independent || row.status !== "pending")) ||
    (view.can_withdraw &&
      (reviewer || row.proposed_by_subject !== scope.subject || row.status !== "pending")) ||
    (view.can_revoke && (!reviewer || !independent || row.status !== "approved"))
  )
    throw new Error(
      "Could not verify current employment review capabilities. Refresh before continuing.",
    );
  return view;
}

async function employmentReviewAction(
  scope: EmploymentReviewScope,
  id: string,
  action: "decision" | "withdrawal" | "revocation",
  command: EmploymentReviewDecisionInput | EmploymentReviewActionInput,
  status: EmploymentReview["status"],
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const reviewer = reviewerMode(scope);
  if ((action === "withdrawal") === reviewer)
    throw new Error("This command requires the appropriate preparation or reviewer authority.");
  const [path, query] = employmentReviewsPath(scope, id).split("?");
  const response = await apiFetch<unknown>(`${path}/${action}?${query}`, {
    ...staffReviewRequest(scope),
    method: "POST",
    body: JSON.stringify(command),
    signal,
  });
  signal?.throwIfAborted();
  const row = owned(scope, id, response);
  if (
    row.revision_id !== command.command_id ||
    row.version !== command.expected_version + 1 ||
    row.status !== status ||
    row.actor_subject !== scope.subject ||
    row.decision_reason !== command.reason ||
    (action === "decision" &&
      (row.reviewer_terms_examined !== (command as EmploymentReviewDecisionInput).terms_examined ||
        row.independent_person_confirmed !==
          (command as EmploymentReviewDecisionInput).independent_person_confirmed))
  )
    throw new Error(receiptError);
  return row;
}
export async function decideEmploymentReview(
  scope: EmploymentReviewReviewerScope,
  id: string,
  input: EmploymentReviewDecisionInput,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const command = EmploymentReviewDecisionInputContract.parse(input);
  if (!command.terms_examined || !command.independent_person_confirmed)
    throw new Error("Examine the terms and confirm independence before recording a decision.");
  return employmentReviewAction(
    scope,
    id,
    "decision",
    command,
    command.decision === "approve" ? "approved" : "rejected",
    signal,
  );
}
export async function withdrawEmploymentReview(
  scope: EmploymentReviewPreparationScope,
  id: string,
  input: EmploymentReviewActionInput,
  signal?: AbortSignal,
) {
  return employmentReviewAction(
    scope,
    id,
    "withdrawal",
    EmploymentReviewActionInputContract.parse(input),
    "withdrawn",
    signal,
  );
}
export async function revokeEmploymentReview(
  scope: EmploymentReviewReviewerScope,
  id: string,
  input: EmploymentReviewActionInput,
  signal?: AbortSignal,
) {
  return employmentReviewAction(
    scope,
    id,
    "revocation",
    EmploymentReviewActionInputContract.parse(input),
    "revoked",
    signal,
  );
}
