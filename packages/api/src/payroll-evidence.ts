import { z } from "zod";
import { apiFetch, apiFetchResponse } from "./client";
import { readAttachmentFile } from "./attachment-file";
import { staffDocumentSchema } from "./documents-fields";
import { taxEvidenceInputContract, type TaxEvidenceInput } from "./payroll-contract";

export const MAX_TAX_EVIDENCE = 10;
export const taxEvidenceSchema = staffDocumentSchema.extend({
  record_kind: z.literal("tax_declaration"),
  category: z.literal("tax_declaration_evidence"),
});
export type TaxEvidence = z.infer<typeof taxEvidenceSchema>;
export const taxEvidenceListSchema = z.object({
  evidence: z.array(taxEvidenceSchema).max(MAX_TAX_EVIDENCE),
  uploads_available: z.boolean(),
});
export type TaxEvidenceList = z.infer<typeof taxEvidenceListSchema>;

// Scope pins every receipt to the staff member and proposal the screen shows.
export type TaxEvidenceScope = { declarationPath: string; staffId: string; revisionId: string };
export type TaxEvidenceUpload = { id: string; file: File; input: TaxEvidenceInput };

const identifier = staffDocumentSchema.shape.id;
const filename = staffDocumentSchema.shape.filename;

export const taxEvidenceFileSchema = z.object({
  file: z.custom<File | null>().superRefine((file, ctx) => {
    if (!file) {
      ctx.addIssue({ code: "custom", message: "Choose a file first." });
      return;
    }
    const name = filename.safeParse(file.name);
    if (!name.success) ctx.addIssue({ code: "custom", message: name.error.issues[0]!.message });
    else if (!taxEvidenceInputContract.shape.content_type.safeParse(file.type).success)
      ctx.addIssue({ code: "custom", message: "Choose a PNG or JPEG image." });
    else if (!taxEvidenceInputContract.shape.size.safeParse(file.size).success)
      ctx.addIssue({ code: "custom", message: "Choose an image up to 10 MB." });
  }),
});

export const taxEvidenceKey = (scope: TaxEvidenceScope) => [
  "workforce",
  "tax-declaration-evidence",
  scope.declarationPath,
  scope.revisionId,
];
export const taxEvidenceRoot = (scope: TaxEvidenceScope) => {
  identifier.parse(scope.staffId);
  identifier.parse(scope.revisionId);
  return `${scope.declarationPath}/evidence`;
};

export function taxEvidenceReceipt(scope: TaxEvidenceScope, value: unknown, id?: string) {
  const saved = taxEvidenceSchema.parse(value);
  if (
    saved.staff_id !== scope.staffId ||
    saved.record_id !== scope.revisionId ||
    (id !== undefined && saved.id !== id)
  )
    throw new Error("Evidence ownership does not match this proposal.");
  return saved;
}

export async function fetchTaxEvidence(scope: TaxEvidenceScope, signal: AbortSignal) {
  const list = taxEvidenceListSchema.parse(
    await apiFetch(`${taxEvidenceRoot(scope)}?revision_id=${scope.revisionId}`, {
      signal,
      cache: "no-store",
    }),
  );
  list.evidence.forEach((file) => taxEvidenceReceipt(scope, file));
  return list;
}

export async function prepareTaxEvidence(
  scope: TaxEvidenceScope,
  id: string,
  file: File,
  signal: AbortSignal,
): Promise<TaxEvidenceUpload> {
  signal.throwIfAborted();
  identifier.parse(id);
  const draft = {
    revision_id: scope.revisionId,
    filename: filename.parse(file.name),
    content_type: file.type,
    size: file.size,
  };
  taxEvidenceInputContract.parse({ ...draft, sha256: "0".repeat(64) });
  const data = await readAttachmentFile(file, signal);
  signal.throwIfAborted();
  if (data.byteLength !== file.size)
    throw new Error("The selected image could not be read safely.");
  const digest = await crypto.subtle.digest("SHA-256", data);
  signal.throwIfAborted();
  const sha256 = Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
  return { id, file, input: taxEvidenceInputContract.parse({ ...draft, sha256 }) };
}

export async function submitTaxEvidence(
  scope: TaxEvidenceScope,
  upload: TaxEvidenceUpload,
  signal: AbortSignal,
): Promise<TaxEvidence> {
  signal.throwIfAborted();
  const input = taxEvidenceInputContract.parse(upload.input);
  if (input.revision_id !== scope.revisionId)
    throw new Error("The upload belongs to a different proposal.");
  const path = `${taxEvidenceRoot(scope)}/${identifier.parse(upload.id)}`;
  const intent = taxEvidenceReceipt(
    scope,
    await apiFetch(path, { method: "PUT", body: JSON.stringify(input), signal }),
    upload.id,
  );
  if (
    intent.sha256 !== input.sha256 ||
    intent.filename !== input.filename ||
    intent.size !== input.size ||
    intent.content_type !== input.content_type
  )
    throw new Error("The saved upload request does not match the selected file.");
  signal.throwIfAborted();
  if (intent.status !== "upload_pending") return intent;
  return taxEvidenceReceipt(
    scope,
    await apiFetch(path + "/content", {
      method: "PUT",
      headers: { "Content-Type": input.content_type },
      body: upload.file,
      signal,
    }),
    upload.id,
  );
}

export async function retryTaxEvidence(
  scope: TaxEvidenceScope,
  file: TaxEvidence,
  attempt: number,
  signal: AbortSignal,
): Promise<TaxEvidence> {
  signal.throwIfAborted();
  taxEvidenceReceipt(scope, file, file.id);
  staffDocumentSchema.shape.scan_attempt.parse(attempt);
  return taxEvidenceReceipt(
    scope,
    await apiFetch(`${taxEvidenceRoot(scope)}/${file.id}/retries/${attempt}`, {
      method: "PUT",
      signal,
    }),
    file.id,
  );
}

export async function downloadTaxEvidence(
  scope: TaxEvidenceScope,
  file: TaxEvidence,
  signal: AbortSignal,
): Promise<Blob> {
  signal.throwIfAborted();
  taxEvidenceReceipt(scope, file, file.id);
  if (file.status !== "available") throw new Error("This image is not ready for download.");
  const res = await apiFetchResponse(`${taxEvidenceRoot(scope)}/${file.id}/content`, {
    cache: "no-store",
    signal,
  });
  const blob = await res.blob();
  signal.throwIfAborted();
  if (blob.size !== file.size) throw new Error("The evidence download could not be verified.");
  return blob;
}
