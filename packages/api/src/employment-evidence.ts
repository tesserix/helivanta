import { apiFetch, apiFetchResponse } from "./client";
import { withStaffIntakeScope, staffIntakeScopeKey } from "./enrollment";
import { sha256Hex } from "./attachment-file";
import { ownedDocument, type EstablishmentDocument } from "./documents";
import {
  employmentOwnerKey,
  employmentStaffPath,
  employmentPeriodsPath,
  employmentPeriodRequest,
  type EmploymentPeriodScope,
  type PharmacyEmploymentPeriodScope,
} from "./employment-periods";
import {
  EmploymentEvidenceAttachInputContract,
  EmploymentEvidenceWithdrawInputContract,
  EmploymentEvidenceBindingContract,
  type EmploymentEvidenceAttachInput,
  type EmploymentEvidenceWithdrawInput,
  type EmploymentEvidenceBinding,
  pharmacyEmploymentEvidenceBindingContract,
  type PharmacyEmploymentEvidenceBinding,
} from "./employment-periods-contract";
export type EmploymentEvidenceScope = EmploymentPeriodScope & {
  periodId: string;
  periodVersion: number;
};
export type EmploymentEvidenceOf<S extends EmploymentEvidenceScope> =
  S extends PharmacyEmploymentPeriodScope
    ? PharmacyEmploymentEvidenceBinding
    : EmploymentEvidenceBinding;
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
  ...employmentOwnerKey(scope),
  scope.staffId,
  scope.periodId,
  scope.periodVersion,
  ...(scope.hrIntake === true ? [staffIntakeScopeKey(scope)] : []),
];
function ownedBinding<S extends EmploymentEvidenceScope>(
  scope: S,
  value: unknown,
): EmploymentEvidenceOf<S> | undefined {
  const parsed =
    scope.pharmacyId === undefined
      ? EmploymentEvidenceBindingContract.safeParse(value)
      : pharmacyEmploymentEvidenceBindingContract.safeParse(value);
  if (!parsed.success) return undefined;
  const row = parsed.data;
  const owner = "pharmacy_id" in row ? row.pharmacy_id : row.hospital_id;
  if (
    owner !== (scope.pharmacyId ?? scope.hospitalId) ||
    row.staff_id !== scope.staffId ||
    row.period_id !== scope.periodId ||
    row.period_version !== scope.periodVersion
  )
    return undefined;
  // The schema was chosen by the scope's family above.
  return row as EmploymentEvidenceOf<S>;
}
/** The terms revision's evidence, or undefined when any row belongs to another owner or family. */
export function ownedEmploymentEvidence<S extends EmploymentEvidenceScope>(
  scope: S,
  value: unknown,
): EmploymentEvidenceOf<S>[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const rows: EmploymentEvidenceOf<S>[] = [];
  for (const row of value) {
    const owned = ownedBinding(scope, row);
    if (owned === undefined) return undefined;
    rows.push(owned);
  }
  return rows;
}
export const employmentEvidenceDocumentsPath = (scope: EmploymentEvidenceScope) =>
  `${employmentStaffPath(scope)}/documents`;
/** Released employment files for the Staff version, or undefined when any file belongs elsewhere. */
export function eligibleEmploymentDocuments(
  scope: EmploymentEvidenceScope,
  staffVersion: number,
  value: unknown,
): EstablishmentDocument[] | undefined {
  if (!Array.isArray(value)) return undefined;
  let files: EstablishmentDocument[];
  try {
    files = value.map((file) => ownedDocument(scope, file));
  } catch {
    return undefined;
  }
  return files.filter(
    (file) =>
      file.record_kind === "enrollment" &&
      file.record_id === scope.staffId &&
      file.record_version === staffVersion &&
      file.category === "employment_evidence" &&
      file.status === "available",
  );
}
function receipt<S extends EmploymentEvidenceScope>(
  scope: S,
  id: string,
  command: string,
  value: unknown,
): EmploymentEvidenceOf<S> {
  const row = ownedBinding(scope, value);
  if (
    row === undefined ||
    row.id !== id ||
    row.revision_id !== command ||
    row.actor_subject !== scope.subject
  )
    throw new Error("Could not verify employment evidence receipt. Check history before retrying.");
  return row;
}
export async function attachEmploymentEvidence<S extends EmploymentEvidenceScope>(
  scope: S,
  id: string,
  input: EmploymentEvidenceAttachInput,
): Promise<EmploymentEvidenceOf<S>> {
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
export async function withdrawEmploymentEvidence<S extends EmploymentEvidenceScope>(
  scope: S,
  id: string,
  input: EmploymentEvidenceWithdrawInput,
): Promise<EmploymentEvidenceOf<S>> {
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
export async function downloadEmploymentEvidence<S extends EmploymentEvidenceScope>(
  scope: S,
  row: EmploymentEvidenceOf<S>,
  signal: AbortSignal,
) {
  const file = ownedBinding(scope, row);
  if (file === undefined || file.status !== "attached")
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
