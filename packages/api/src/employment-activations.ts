import { apiFetch } from "./client";
import { personReviewRequest } from "./person-reviews";
import type { EmploymentReviewPreparationScope } from "./employment-reviews";
import {
  EmploymentActivationInputContract,
  EmploymentActivationSourceContract,
  EmploymentGenerationViewContract,
  type EmploymentActivationInput,
} from "./employment-review-contract";

export { v7 as newEmploymentActivationId } from "uuid";
export type EmploymentActivationScope = EmploymentReviewPreparationScope;
const identifier = EmploymentActivationSourceContract.shape.staff_id;

function path(scope: EmploymentActivationScope) {
  if (scope.hrIntake !== true)
    throw new Error("Reopen the original Staff preparation workspace before activation.");
  return `/workforce/hospitals/${identifier.parse(scope.hospitalId)}/staff/${identifier.parse(scope.staffId)}`;
}

export async function getEmploymentActivationSource(
  scope: EmploymentActivationScope,
  review: string,
  signal?: AbortSignal,
) {
  const response = await apiFetch<unknown>(
    `${path(scope)}/employment-reviews/${identifier.parse(review)}/activation-source?employment_activation=v1`,
    { ...personReviewRequest(scope), signal },
  );
  signal?.throwIfAborted();
  const parsed = EmploymentActivationSourceContract.safeParse(response);
  if (
    !parsed.success ||
    parsed.data.hospital_id !== scope.hospitalId ||
    parsed.data.staff_id !== scope.staffId ||
    parsed.data.review_id !== review ||
    parsed.data.can_activate !== (parsed.data.current_state === "eligible")
  )
    throw new Error(
      "The activation source could not be verified. Refresh the original Staff workspace.",
    );
  return parsed.data;
}

export async function activateEmployment(
  scope: EmploymentActivationScope,
  id: string,
  input: EmploymentActivationInput,
  signal?: AbortSignal,
) {
  const inScope = EmploymentActivationInputContract.parse(input);
  const response = await apiFetch<unknown>(
    `${path(scope)}/employment-activations/${identifier.parse(id)}?employment_activation=v1`,
    { ...personReviewRequest(scope), method: "PUT", body: JSON.stringify(inScope), signal },
  );
  signal?.throwIfAborted();
  const parsed = EmploymentGenerationViewContract.safeParse(response);
  const g = parsed.success ? parsed.data.generation : null;
  if (
    !parsed.success ||
    !g ||
    parsed.data.grants_access ||
    g.id !== id ||
    g.revision_id !== inScope.command_id ||
    g.hospital_id !== scope.hospitalId ||
    g.staff_id !== scope.staffId ||
    g.activated_by_subject !== scope.subject ||
    g.review_id !== inScope.review_id ||
    g.review_revision_id !== inScope.review_revision_id ||
    g.review_version !== inScope.review_version ||
    g.review_staff_version !== inScope.staff_version ||
    g.review_employment_epoch !== inScope.employment_epoch ||
    g.staff_version !== inScope.staff_version + 1 ||
    g.employment_epoch !== inScope.employment_epoch + 1 ||
    g.reason !== inScope.reason ||
    (g.access_intent === "digital" ? !g.declared_subject : g.declared_subject !== "")
  )
    throw new Error(
      "The activation receipt could not be verified. Retry the same command to check its outcome.",
    );
  return parsed.data;
}
