import { z } from "zod";
import { apiFetch, apiFetchResponse } from "./client";
import { personReviewRequest } from "./person-reviews";
import { governanceEmploymentDocumentScopeContract } from "./employment-document-preparation";
import { verifiedReviewEvidenceBlob } from "./review-evidence-content";
import { staffDocumentSchema, type StaffDocument } from "./documents-fields";
import {
  EmploymentPeriodDraftContract,
  EmploymentEvidenceBindingContract,
  EmploymentEvidenceAttachInputContract,
  EmploymentEvidenceWithdrawInputContract,
  type EmploymentEvidenceBinding,
  type EmploymentEvidenceAttachInput,
  type EmploymentEvidenceWithdrawInput,
} from "./employment-periods-contract";
const periodShape = EmploymentPeriodDraftContract.shape;
const version7 = EmploymentEvidenceBindingContract.shape.id.refine(
  (value) => value[14] === "7",
  "Use the original command identifier.",
);
const scopeContract = governanceEmploymentDocumentScopeContract
  .extend({
    periodId: version7,
    periodRevisionId: version7,
    periodVersion: periodShape.version,
    periodStaffVersion: periodShape.staff_version,
    periodEmploymentEpoch: periodShape.employment_epoch,
  })
  .strict();
export type GovernanceEmploymentBindingScope = z.infer<typeof scopeContract>;
const generation = EmploymentEvidenceBindingContract.shape.source_generation.refine(
  (value) => /^[1-9][0-9]{0,18}$/.test(value) && BigInt(value) <= 9223372036854775807n,
  "Verify the original file generation.",
);
const bindingContract = EmploymentEvidenceBindingContract.extend({
  id: version7,
  revision_id: version7,
  period_id: version7,
  period_revision_id: version7,
  filename: staffDocumentSchema.shape.filename,
  content_type: staffDocumentSchema.shape.content_type,
  size: staffDocumentSchema.shape.size,
  source_generation: generation,
  released_generation: generation,
}).strict();
const unverified = () =>
  new Error(
    "The employment evidence binding could not be verified. Refresh the exact terms before continuing.",
  );
function workspace(value: GovernanceEmploymentBindingScope) {
  const scope = scopeContract.parse(value);
  const query = new URLSearchParams({
    staff_version: String(scope.staffVersion),
    employment_epoch: String(scope.employmentEpoch),
  });
  return {
    scope,
    query,
    request: personReviewRequest(scope),
    path: `/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-preparation-periods/${scope.periodId}/revisions/${scope.periodVersion}/evidence`,
  };
}
function owned(scope: GovernanceEmploymentBindingScope, value: unknown, id?: string) {
  const parsed = bindingContract.safeParse(value);
  if (!parsed.success) throw unverified();
  const row = parsed.data;
  if (
    (id !== undefined && row.id !== id) ||
    row.hospital_id !== scope.hospitalId ||
    row.staff_id !== scope.staffId ||
    row.period_id !== scope.periodId ||
    row.period_revision_id !== scope.periodRevisionId ||
    row.period_version !== scope.periodVersion ||
    row.staff_version !== scope.periodStaffVersion ||
    row.employment_epoch !== scope.periodEmploymentEpoch ||
    row.document_record_version !== scope.periodStaffVersion ||
    (row.status === "attached" ? row.version !== 1 : row.version !== 2) ||
    Date.parse(row.updated_at) < Date.parse(row.created_at)
  )
    throw unverified();
  return row;
}
export async function getGovernanceEmploymentEvidence(
  scope: GovernanceEmploymentBindingScope,
  id: string,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const current = workspace(scope);
  const selected = version7.parse(id);
  const response = await apiFetch<unknown>(`${current.path}/${selected}?${current.query}`, {
    ...current.request,
    signal,
  });
  signal?.throwIfAborted();
  return owned(current.scope, response, selected);
}
const pageContract = z
  .object({
    data: z.array(bindingContract).max(20),
    page: z
      .object({ has_more: z.boolean(), next_cursor: z.string().min(1).max(2048).nullable() })
      .strict(),
  })
  .strict();
async function listBindings(
  scope: GovernanceEmploymentBindingScope,
  id: string | null,
  cursor: string | null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const current = workspace(scope);
  const selected = id === null ? null : version7.parse(id);
  current.query.set("limit", "20");
  if (cursor !== null) current.query.set("cursor", z.string().min(1).max(2048).parse(cursor));
  const suffix = selected === null ? "" : `/${selected}/history`;
  const response = await apiFetch<unknown>(`${current.path}${suffix}?${current.query}`, {
    ...current.request,
    signal,
  });
  signal?.throwIfAborted();
  const parsed = pageContract.safeParse(response);
  if (!parsed.success) throw unverified();
  const { data, page } = parsed.data;
  if (
    page.has_more !== (page.next_cursor !== null) ||
    (page.has_more && data.length !== 20) ||
    (cursor !== null && page.next_cursor === cursor) ||
    new Set(data.map((row) => (selected === null ? row.id : row.revision_id).toLowerCase()))
      .size !== data.length ||
    (selected !== null && new Set(data.map((row) => row.version)).size !== data.length)
  )
    throw unverified();
  for (const row of data) owned(current.scope, row, selected ?? undefined);
  return parsed.data;
}
export function listGovernanceEmploymentEvidence(
  scope: GovernanceEmploymentBindingScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  return listBindings(scope, null, cursor, signal);
}
export function listGovernanceEmploymentEvidenceHistory(
  scope: GovernanceEmploymentBindingScope,
  id: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  return listBindings(scope, id, cursor, signal);
}
function sameFile(row: EmploymentEvidenceBinding, file: StaffDocument) {
  return (
    row.document_id === file.id &&
    row.filename === file.filename &&
    row.content_type === file.content_type &&
    row.size === file.size &&
    row.sha256 === file.sha256 &&
    row.scan_attempt === file.scan_attempt &&
    file.scanned_at !== null &&
    Date.parse(row.scanned_at) === Date.parse(file.scanned_at)
  );
}
export async function attachGovernanceEmploymentEvidence(
  scope: GovernanceEmploymentBindingScope,
  id: string,
  input: EmploymentEvidenceAttachInput,
  file: StaffDocument,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const current = workspace(scope);
  const selected = version7.parse(id);
  const command = EmploymentEvidenceAttachInputContract.parse(input);
  version7.parse(command.command_id);
  const document = staffDocumentSchema.strict().parse(file);
  if (
    current.scope.staffVersion !== current.scope.periodStaffVersion ||
    current.scope.employmentEpoch !== current.scope.periodEmploymentEpoch ||
    document.id !== command.document_id ||
    document.hospital_id !== current.scope.hospitalId ||
    document.staff_id !== current.scope.staffId ||
    document.record_id !== current.scope.staffId ||
    document.record_kind !== "enrollment" ||
    document.record_version !== current.scope.periodStaffVersion ||
    document.category !== "employment_evidence" ||
    document.status !== "available" ||
    document.scanned_at === null ||
    document.uploaded_at === null
  )
    throw unverified();
  const response = await apiFetch<unknown>(`${current.path}/${selected}?${current.query}`, {
    ...current.request,
    method: "PUT",
    body: JSON.stringify(command),
    signal,
  });
  signal?.throwIfAborted();
  const row = owned(current.scope, response, selected);
  if (
    row.revision_id !== command.command_id ||
    row.status !== "attached" ||
    row.actor_subject !== current.scope.subject ||
    row.proposed_by_subject !== current.scope.subject ||
    row.reason !== command.reason ||
    !sameFile(row, document)
  )
    throw unverified();
  return row;
}
function sameFrozen(original: EmploymentEvidenceBinding, row: EmploymentEvidenceBinding) {
  return (
    (
      [
        "id",
        "hospital_id",
        "staff_id",
        "period_id",
        "period_revision_id",
        "period_version",
        "staff_version",
        "employment_epoch",
        "document_id",
        "document_record_version",
        "filename",
        "content_type",
        "size",
        "sha256",
        "source_generation",
        "released_generation",
        "scan_attempt",
        "proposed_by_subject",
      ] as const
    ).every((key) => original[key] === row[key]) &&
    Date.parse(original.scanned_at) === Date.parse(row.scanned_at) &&
    Date.parse(original.created_at) === Date.parse(row.created_at)
  );
}
export async function withdrawGovernanceEmploymentEvidence(
  scope: GovernanceEmploymentBindingScope,
  value: EmploymentEvidenceBinding,
  input: EmploymentEvidenceWithdrawInput,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const current = workspace(scope);
  const original = owned(current.scope, value);
  const command = EmploymentEvidenceWithdrawInputContract.parse(input);
  version7.parse(command.command_id);
  if (original.status !== "attached" || command.expected_version !== original.version)
    throw unverified();
  const response = await apiFetch<unknown>(
    `${current.path}/${original.id}/withdraw?${current.query}`,
    { ...current.request, method: "POST", body: JSON.stringify(command), signal },
  );
  signal?.throwIfAborted();
  const row = owned(current.scope, response, original.id);
  if (
    row.revision_id !== command.command_id ||
    row.status !== "withdrawn" ||
    row.version !== command.expected_version + 1 ||
    row.actor_subject !== current.scope.subject ||
    row.reason !== command.reason ||
    !sameFrozen(original, row)
  )
    throw unverified();
  return row;
}
export async function downloadGovernanceEmploymentEvidence(
  scope: GovernanceEmploymentBindingScope,
  value: EmploymentEvidenceBinding,
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  const current = workspace(scope);
  const original = owned(current.scope, value);
  if (original.status !== "attached") throw unverified();
  const response = await apiFetchResponse(
    `${current.path}/${original.id}/content?${current.query}`,
    { ...current.request, signal },
  );
  signal.throwIfAborted();
  return verifiedReviewEvidenceBlob(response, original, signal);
}
