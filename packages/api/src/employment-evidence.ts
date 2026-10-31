import { apiFetch, apiFetchResponse } from "./client";
import { withStaffIntakeScope, staffIntakeScopeKey } from "./enrollment";
import { sha256Hex } from "./attachment-file";
import {
  employmentPeriodsPath,
  employmentPeriodRequest,
  type EmploymentPeriodScope,
} from "./employment-periods";
import {
  EmploymentEvidenceAttachInputContract,
  EmploymentEvidenceWithdrawInputContract,
  EmploymentEvidenceBindingContract,
  type EmploymentEvidenceAttachInput,
  type EmploymentEvidenceWithdrawInput,
  type EmploymentEvidenceBinding,
} from "./employment-periods-contract";
export type EmploymentEvidenceScope = EmploymentPeriodScope & {
  periodId: string;
  periodVersion: number;
};
export function employmentEvidencePath(scope: EmploymentEvidenceScope) {
  const id = EmploymentEvidenceBindingContract.shape.period_id.parse(scope.periodId);
  const version = EmploymentEvidenceBindingContract.shape.period_version.parse(scope.periodVersion);
  return `${employmentPeriodsPath(scope)}/${id}/revisions/${version}/evidence`;
}
export const employmentEvidenceKey = (scope: EmploymentEvidenceScope) => [
  "workforce",
  "employment-evidence",
  scope.tenantId,
  scope.subject,
  scope.hospitalId,
  scope.staffId,
  scope.periodId,
  scope.periodVersion,
  ...(scope.hrIntake === true ? [staffIntakeScopeKey(scope)] : []),
];
export function employmentEvidenceMatches(
  scope: EmploymentEvidenceScope,
  row: EmploymentEvidenceBinding,
) {
  return (
    row.hospital_id === scope.hospitalId &&
    row.staff_id === scope.staffId &&
    row.period_id === scope.periodId &&
    row.period_version === scope.periodVersion
  );
}
function receipt(scope: EmploymentEvidenceScope, id: string, command: string, value: unknown) {
  const parsed = EmploymentEvidenceBindingContract.safeParse(value);
  if (
    !parsed.success ||
    !employmentEvidenceMatches(scope, parsed.data) ||
    parsed.data.id !== id ||
    parsed.data.revision_id !== command ||
    parsed.data.actor_subject !== scope.subject
  )
    throw new Error("Could not verify employment evidence receipt. Check history before retrying.");
  return parsed.data;
}
export async function attachEmploymentEvidence(
  scope: EmploymentEvidenceScope,
  id: string,
  input: EmploymentEvidenceAttachInput,
) {
  EmploymentEvidenceBindingContract.shape.id.parse(id);
  const command = EmploymentEvidenceAttachInputContract.parse(input);
  const saved = receipt(
    scope,
    id,
    command.command_id,
    await apiFetch(withStaffIntakeScope(`${employmentEvidencePath(scope)}/${id}`, scope), {
      ...employmentPeriodRequest(scope),
      method: "PUT",
      body: JSON.stringify(command),
    }),
  );
  if (
    saved.status !== "attached" ||
    saved.version !== 1 ||
    saved.document_id !== command.document_id
  )
    throw new Error("Could not verify employment evidence receipt.");
  return saved;
}
export async function withdrawEmploymentEvidence(
  scope: EmploymentEvidenceScope,
  id: string,
  input: EmploymentEvidenceWithdrawInput,
) {
  EmploymentEvidenceBindingContract.shape.id.parse(id);
  const command = EmploymentEvidenceWithdrawInputContract.parse(input);
  const saved = receipt(
    scope,
    id,
    command.command_id,
    await apiFetch(withStaffIntakeScope(`${employmentEvidencePath(scope)}/${id}/withdraw`, scope), {
      ...employmentPeriodRequest(scope),
      method: "POST",
      body: JSON.stringify(command),
    }),
  );
  if (saved.status !== "withdrawn" || saved.version !== command.expected_version + 1)
    throw new Error("Could not verify employment evidence receipt.");
  return saved;
}
export async function downloadEmploymentEvidence(
  scope: EmploymentEvidenceScope,
  row: EmploymentEvidenceBinding,
  signal: AbortSignal,
) {
  const file = EmploymentEvidenceBindingContract.parse(row);
  if (!employmentEvidenceMatches(scope, file) || file.status !== "attached")
    throw new Error("Evidence is not available in this workspace.");
  signal.throwIfAborted();
  const response = await apiFetchResponse(
    withStaffIntakeScope(`${employmentEvidencePath(scope)}/${file.id}/content`, scope),
    {
      ...employmentPeriodRequest(scope),
      signal,
    },
  );
  const data = await response.arrayBuffer();
  signal.throwIfAborted();
  if (data.byteLength !== file.size || (await sha256Hex(data)) !== file.sha256)
    throw new Error("The downloaded evidence could not be verified.");
  signal.throwIfAborted();
  return new Blob([data], { type: file.content_type });
}
