import { readAttachmentFile } from "./attachment-file";
import { apiFetch, apiFetchResponse } from "./client";
import {
  documentInputSchema,
  staffDocumentSchema,
  MAX_DOCUMENT_BYTES,
  DOCUMENT_PROCESSING_TIMEOUT_MS,
  type DocumentInput,
  type DocumentRecordChoice,
  type StaffDocument,
} from "./documents-fields";
export * from "./documents-fields";

export type DocumentScope = { tenantId: string; hospitalId: string; staffId: string };
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
  return `/workforce/hospitals/${identifier.parse(scope.hospitalId)}/staff/${identifier.parse(scope.staffId)}`;
}

export function documentHeaders(scope: DocumentScope): Headers {
  documentPath(scope);
  return new Headers({ "X-Helivanta-Expected-Tenant": scope.tenantId });
}

function receipt(scope: DocumentScope, id: string, value: unknown): StaffDocument {
  const document = staffDocumentSchema.parse(value);
  if (
    document.id !== id ||
    document.hospital_id !== scope.hospitalId ||
    document.staff_id !== scope.staffId
  )
    throw new Error("Document ownership did not match this workspace.");
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

export async function submitDocumentUpload(
  scope: DocumentScope,
  upload: DocumentUpload,
  signal: AbortSignal,
): Promise<StaffDocument> {
  signal.throwIfAborted();
  const input = documentInputSchema.parse(upload.input);
  const path = `${documentPath(scope)}/documents/${staffDocumentSchema.shape.id.parse(upload.id)}`;
  const intent = receipt(
    scope,
    upload.id,
    await apiFetch(path, {
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
    await apiFetch(path + "/content", { method: "PUT", headers, body: upload.file, signal }),
  );
}

export async function retryDocumentScan(
  scope: DocumentScope,
  id: string,
  attempt: number,
  signal: AbortSignal,
): Promise<StaffDocument> {
  signal.throwIfAborted();
  staffDocumentSchema.shape.scan_attempt.parse(attempt);
  const path = `${documentPath(scope)}/documents/${staffDocumentSchema.shape.id.parse(id)}/retries/${attempt}`;
  return receipt(
    scope,
    id,
    await apiFetch(path, { method: "PUT", headers: documentHeaders(scope), signal }),
  );
}

export async function revokeStaffDocument(
  scope: DocumentScope,
  id: string,
  signal: AbortSignal,
): Promise<StaffDocument> {
  signal.throwIfAborted();
  const path = `${documentPath(scope)}/documents/${staffDocumentSchema.shape.id.parse(id)}/revocation`;
  return receipt(
    scope,
    id,
    await apiFetch(path, { method: "PUT", headers: documentHeaders(scope), signal }),
  );
}

export async function downloadStaffDocument(
  scope: DocumentScope,
  document: StaffDocument,
  signal: AbortSignal,
): Promise<Blob> {
  signal.throwIfAborted();
  if (document.status !== "available") throw new Error("Document is not available for download.");
  receipt(scope, document.id, document);
  const response = await apiFetchResponse(
    `${documentPath(scope)}/documents/${document.id}/content`,
    { headers: documentHeaders(scope), cache: "no-store", signal },
  );
  const blob = await response.blob();
  signal.throwIfAborted();
  if (blob.size !== document.size) throw new Error("The downloaded file could not be verified.");
  return blob;
}
