import { withStaffIntakeScope, type EnrollmentRequestScope } from "./enrollment";
import { readAttachmentFile } from "./attachment-file";
import { apiFetch, apiFetchResponse } from "./client";
import { pharmacyEmployerPath } from "./pharmacy-employers";
import {
  documentInputSchema,
  staffDocumentSchema,
  pharmacyStaffDocumentSchema,
  MAX_DOCUMENT_BYTES,
  DOCUMENT_PROCESSING_TIMEOUT_MS,
  type DocumentInput,
  type DocumentRecordChoice,
  type PharmacyStaffDocument,
  type StaffDocument,
} from "./documents-fields";
export * from "./documents-fields";

export type HospitalDocumentScope = EnrollmentRequestScope & {
  hospitalId: string;
  pharmacyId?: never;
  staffId: string;
};
// A pharmacy owns only enrollment, credential and qualification files, never an HR intake (ADR-0054).
export type PharmacyDocumentScope = Omit<EnrollmentRequestScope, "hrIntake"> & {
  hrIntake?: never;
  hospitalId?: never;
  pharmacyId: string;
  staffId: string;
};
export type DocumentScope = HospitalDocumentScope | PharmacyDocumentScope;
export type DocumentOf<S extends DocumentScope> = S extends PharmacyDocumentScope
  ? PharmacyStaffDocument
  : StaffDocument;
export type EstablishmentDocument = DocumentOf<DocumentScope>;
export type DocumentUpload = { id: string; input: DocumentInput; file: File };

export function canRetryDocument(
  file: Pick<StaffDocument, "status" | "updated_at">,
  nowMs: number,
): boolean {
  return (
    file.status === "scan_error" ||
    (file.status === "scan_pending" &&
      Number.isFinite(nowMs) &&
      nowMs - Date.parse(file.updated_at) >= DOCUMENT_PROCESSING_TIMEOUT_MS)
  );
}

export function documentPath(scope: DocumentScope): string {
  const identifier = staffDocumentSchema.shape.id;
  identifier.parse(scope.tenantId);
  const staff = `/staff/${identifier.parse(scope.staffId)}`;
  if (scope.pharmacyId === undefined)
    return `/workforce/hospitals/${identifier.parse(scope.hospitalId)}${staff}`;
  if (scope.hospitalId !== undefined || scope.hrIntake !== undefined)
    throw new Error("The document workspace could not be verified.");
  return pharmacyEmployerPath(scope.pharmacyId) + staff;
}

export function documentHeaders(scope: DocumentScope): Headers {
  documentPath(scope);
  if (
    !scope.subject?.trim() ||
    scope.subject !== scope.subject.trim() ||
    scope.subject.length > 200
  )
    throw new Error("The document account could not be verified.");
  return new Headers({
    "X-Helivanta-Expected-Tenant": scope.tenantId,
    "X-Helivanta-Expected-Subject": scope.subject,
  });
}

export function ownedDocument<S extends DocumentScope>(scope: S, value: unknown): DocumentOf<S> {
  const document: EstablishmentDocument =
    scope.pharmacyId === undefined
      ? staffDocumentSchema.parse(value)
      : pharmacyStaffDocumentSchema.parse(value);
  if (
    ("pharmacy_id" in document
      ? document.pharmacy_id !== scope.pharmacyId
      : document.hospital_id !== scope.hospitalId) ||
    document.staff_id !== scope.staffId
  )
    throw new Error("Document ownership did not match this workspace.");
  // The schema was chosen by the scope's family above.
  return document as DocumentOf<S>;
}

function receipt<S extends DocumentScope>(scope: S, id: string, value: unknown): DocumentOf<S> {
  const document = ownedDocument(scope, value);
  if (document.id !== id) throw new Error("Document ownership did not match this workspace.");
  return document;
}

export async function prepareDocumentUpload(
  id: string,
  file: File,
  record: DocumentRecordChoice,
  category: DocumentInput["category"],
  signal: AbortSignal,
): Promise<DocumentUpload> {
  signal.throwIfAborted();
  staffDocumentSchema.shape.id.parse(id);
  if (!record.categories.includes(category))
    throw new Error("Choose evidence for the selected record.");
  documentInputSchema.parse({
    record_kind: record.record_kind,
    record_id: record.record_id,
    category,
    filename: file.name,
    content_type: file.type,
    size: file.size,
    sha256: "0".repeat(64),
  });
  const data = await readAttachmentFile(file, signal);
  signal.throwIfAborted();
  if (data.byteLength !== file.size || data.byteLength > MAX_DOCUMENT_BYTES)
    throw new Error("The selected file could not be read safely.");
  const bytes = new Uint8Array(data.byteLength);
  bytes.set(new Uint8Array(data));
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  signal.throwIfAborted();
  const sha256 = Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
  return {
    id,
    file,
    input: documentInputSchema.parse({
      record_kind: record.record_kind,
      record_id: record.record_id,
      category,
      filename: file.name,
      content_type: file.type,
      size: file.size,
      sha256,
    }),
  };
}

export async function submitDocumentUpload<S extends DocumentScope>(
  scope: S,
  upload: DocumentUpload,
  signal: AbortSignal,
): Promise<DocumentOf<S>> {
  signal.throwIfAborted();
  const input = documentInputSchema.parse(upload.input);
  const path = `${documentPath(scope)}/documents/${staffDocumentSchema.shape.id.parse(upload.id)}`;
  const intent = receipt(
    scope,
    upload.id,
    await apiFetch(withStaffIntakeScope(path, scope), {
      method: "PUT",
      headers: documentHeaders(scope),
      body: JSON.stringify(input),
      signal,
    }),
  );
  if (
    intent.sha256 !== input.sha256 ||
    intent.record_id !== input.record_id ||
    intent.category !== input.category ||
    intent.size !== input.size ||
    intent.content_type !== input.content_type ||
    intent.filename !== input.filename ||
    intent.record_kind !== input.record_kind
  )
    throw new Error("The saved upload request does not match the selected file.");
  signal.throwIfAborted();
  if (intent.status !== "upload_pending") return intent;
  const headers = documentHeaders(scope);
  headers.set("Content-Type", input.content_type);
  return receipt(
    scope,
    upload.id,
    await apiFetch(withStaffIntakeScope(path + "/content", scope), {
      method: "PUT",
      headers,
      body: upload.file,
      signal,
    }),
  );
}

export async function retryDocumentScan<S extends DocumentScope>(
  scope: S,
  id: string,
  attempt: number,
  signal: AbortSignal,
): Promise<DocumentOf<S>> {
  signal.throwIfAborted();
  staffDocumentSchema.shape.scan_attempt.parse(attempt);
  const path = `${documentPath(scope)}/documents/${staffDocumentSchema.shape.id.parse(id)}/retries/${attempt}`;
  return receipt(
    scope,
    id,
    await apiFetch(withStaffIntakeScope(path, scope), {
      method: "PUT",
      headers: documentHeaders(scope),
      signal,
    }),
  );
}

export async function revokeStaffDocument<S extends DocumentScope>(
  scope: S,
  id: string,
  signal: AbortSignal,
): Promise<DocumentOf<S>> {
  signal.throwIfAborted();
  const path = `${documentPath(scope)}/documents/${staffDocumentSchema.shape.id.parse(id)}/revocation`;
  return receipt(
    scope,
    id,
    await apiFetch(withStaffIntakeScope(path, scope), {
      method: "PUT",
      headers: documentHeaders(scope),
      signal,
    }),
  );
}

export async function downloadStaffDocument<S extends DocumentScope>(
  scope: S,
  document: DocumentOf<S>,
  signal: AbortSignal,
): Promise<Blob> {
  signal.throwIfAborted();
  if (document.status !== "available") throw new Error("Document is not available for download.");
  receipt(scope, document.id, document);
  const response = await apiFetchResponse(
    withStaffIntakeScope(`${documentPath(scope)}/documents/${document.id}/content`, scope),
    { headers: documentHeaders(scope), cache: "no-store", signal },
  );
  const blob = await response.blob();
  signal.throwIfAborted();
  if (blob.size !== document.size) throw new Error("The downloaded file could not be verified.");
  return blob;
}
