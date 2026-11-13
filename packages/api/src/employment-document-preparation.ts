import { z } from "zod";
import { prepareDocumentUpload, type DocumentUpload } from "./documents";
import { verifiedReviewEvidenceBlob } from "./review-evidence-content";
import { apiFetch, apiFetchResponse } from "./client";
import { personReviewRequest } from "./person-reviews";
import { governanceEmploymentPreparationScopeContract } from "./employment-preparation";
import { EmploymentPeriodSourceContract } from "./employment-periods-contract";
import {
  documentInputSchema,
  staffDocumentSchema,
  type DocumentInput,
  type StaffDocument,
} from "./documents-fields";

const scopeContract = governanceEmploymentPreparationScopeContract
  .extend({
    staffVersion: EmploymentPeriodSourceContract.shape.staff_version,
    employmentEpoch: EmploymentPeriodSourceContract.shape.employment_epoch,
  })
  .strict();
export type GovernanceEmploymentDocumentScope = z.infer<typeof scopeContract>;
const documentContract = staffDocumentSchema.strict();
const unverified = () =>
  new Error("The employment document could not be verified. Refresh its status before continuing.");
function workspace(value: GovernanceEmploymentDocumentScope) {
  const scope = scopeContract.parse(value);
  const query = new URLSearchParams({
    staff_version: String(scope.staffVersion),
    employment_epoch: String(scope.employmentEpoch),
  });
  return {
    scope,
    query,
    path: `/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-preparation-documents`,
    request: personReviewRequest(scope),
  };
}
function owned(
  scope: GovernanceEmploymentDocumentScope,
  value: unknown,
  id?: string,
): StaffDocument {
  const parsed = documentContract.safeParse(value);
  if (!parsed.success) throw unverified();
  const row = parsed.data;
  if (
    (id !== undefined && row.id !== id) ||
    row.hospital_id !== scope.hospitalId ||
    row.staff_id !== scope.staffId ||
    row.record_id !== scope.staffId ||
    row.record_kind !== "enrollment" ||
    row.category !== "employment_evidence" ||
    row.record_version !== scope.staffVersion
  )
    throw unverified();
  if (
    Date.parse(row.expires_at) <= Date.parse(row.created_at) ||
    Date.parse(row.updated_at) < Date.parse(row.created_at) ||
    (row.uploaded_at !== null && Date.parse(row.uploaded_at) < Date.parse(row.created_at)) ||
    (row.scanned_at !== null &&
      (row.uploaded_at === null || Date.parse(row.scanned_at) < Date.parse(row.uploaded_at))) ||
    (["scan_pending", "scan_error", "rejected", "available", "revoked"].includes(row.status) &&
      row.uploaded_at === null) ||
    (["available", "revoked"].includes(row.status) && row.scanned_at === null)
  )
    throw unverified();
  return row;
}
export async function getGovernanceEmploymentDocument(
  scope: GovernanceEmploymentDocumentScope,
  id: string,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const current = workspace(scope);
  const selected = documentContract.shape.id.parse(id);
  const response = await apiFetch<unknown>(`${current.path}/${selected}?${current.query}`, {
    ...current.request,
    signal,
  });
  signal?.throwIfAborted();
  return owned(current.scope, response, selected);
}
export async function registerGovernanceEmploymentDocument(
  scope: GovernanceEmploymentDocumentScope,
  id: string,
  input: DocumentInput,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const current = workspace(scope);
  const selected = documentContract.shape.id.parse(id);
  const command = documentInputSchema.parse(input);
  if (
    command.record_kind !== "enrollment" ||
    command.record_id !== current.scope.staffId ||
    command.category !== "employment_evidence"
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
  if (Object.entries(command).some(([key, value]) => row[key as keyof StaffDocument] !== value))
    throw unverified();
  return row;
}

const pageContract = z
  .object({
    data: z.array(documentContract).max(20),
    page: z
      .object({ has_more: z.boolean(), next_cursor: z.string().min(1).max(2048).nullable() })
      .strict(),
  })
  .strict();
export async function listGovernanceEmploymentDocuments(
  scope: GovernanceEmploymentDocumentScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const current = workspace(scope);
  current.query.set("limit", "20");
  if (cursor !== null) current.query.set("cursor", z.string().min(1).max(2048).parse(cursor));
  const response = await apiFetch<unknown>(`${current.path}?${current.query}`, {
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
    (page.next_cursor === cursor && cursor !== null) ||
    new Set(data.map((row) => row.id.toLowerCase())).size !== data.length
  )
    throw unverified();
  for (const row of data) owned(current.scope, row);
  return parsed.data;
}

function sameIntent(original: StaffDocument, row: StaffDocument) {
  return (
    [
      "id",
      "hospital_id",
      "staff_id",
      "record_id",
      "record_kind",
      "record_version",
      "category",
      "filename",
      "content_type",
      "size",
      "sha256",
    ].every((key) => original[key as keyof StaffDocument] === row[key as keyof StaffDocument]) &&
    Date.parse(original.created_at) === Date.parse(row.created_at) &&
    Date.parse(original.expires_at) === Date.parse(row.expires_at)
  );
}
export async function prepareGovernanceEmploymentDocument(
  scope: GovernanceEmploymentDocumentScope,
  id: string,
  file: File,
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  const current = workspace(scope);
  return prepareDocumentUpload(
    id,
    file,
    {
      record_kind: "enrollment",
      record_id: current.scope.staffId,
      label: "Staff employment",
      categories: ["employment_evidence"],
    },
    "employment_evidence",
    signal,
  );
}
export async function submitGovernanceEmploymentDocument(
  scope: GovernanceEmploymentDocumentScope,
  upload: DocumentUpload,
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  const current = workspace(scope);
  const input = documentInputSchema.parse(upload.input);
  if (
    upload.file.size !== input.size ||
    upload.file.type !== input.content_type ||
    upload.file.name.trim() !== input.filename
  )
    throw unverified();
  const intent = await registerGovernanceEmploymentDocument(scope, upload.id, input, signal);
  signal.throwIfAborted();
  if (intent.status !== "upload_pending") return intent;
  const headers = new Headers(current.request.headers);
  headers.set("Content-Type", input.content_type);
  const response = await apiFetch<unknown>(
    `${current.path}/${intent.id}/content?${current.query}`,
    { ...current.request, headers, method: "PUT", body: upload.file, signal },
  );
  signal.throwIfAborted();
  const row = owned(current.scope, response, intent.id);
  if (!sameIntent(intent, row) || row.status === "upload_pending") throw unverified();
  return row;
}

export async function retryGovernanceEmploymentDocument(
  scope: GovernanceEmploymentDocumentScope,
  value: StaffDocument,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const current = workspace(scope);
  const original = owned(current.scope, value);
  if (!["scan_pending", "scan_error"].includes(original.status)) throw unverified();
  const attempt = z
    .number()
    .int()
    .min(2)
    .max(9007199254740990)
    .parse(original.scan_attempt + 1);
  const response = await apiFetch<unknown>(
    `${current.path}/${original.id}/retries/${attempt}?${current.query}`,
    { ...current.request, method: "PUT", signal },
  );
  signal?.throwIfAborted();
  const row = owned(current.scope, response, original.id);
  if (row.scan_attempt !== attempt || !sameIntent(original, row) || row.status === "upload_pending")
    throw unverified();
  return row;
}
export async function revokeGovernanceEmploymentDocument(
  scope: GovernanceEmploymentDocumentScope,
  value: StaffDocument,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const current = workspace(scope);
  const original = owned(current.scope, value);
  if (!["available", "revoked"].includes(original.status)) throw unverified();
  const response = await apiFetch<unknown>(
    `${current.path}/${original.id}/revocation?${current.query}`,
    { ...current.request, method: "PUT", signal },
  );
  signal?.throwIfAborted();
  const row = owned(current.scope, response, original.id);
  if (
    row.status !== "revoked" ||
    row.scan_attempt !== original.scan_attempt ||
    !sameIntent(original, row)
  )
    throw unverified();
  return row;
}

export async function downloadGovernanceEmploymentDocument(
  scope: GovernanceEmploymentDocumentScope,
  value: StaffDocument,
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  const current = workspace(scope);
  const original = owned(current.scope, value);
  if (original.status !== "available") throw unverified();
  const response = await apiFetchResponse(
    `${current.path}/${original.id}/content?${current.query}`,
    { ...current.request, signal },
  );
  signal.throwIfAborted();
  return verifiedReviewEvidenceBlob(response, original, signal);
}
