import { z } from "zod";
import { apiFetch, apiFetchResponse } from "./client";
import { readAttachmentFile, sha256Hex } from "./attachment-file";
import { pharmacyRoot, type PharmacyScope } from "./pharmacy";
import {
  pharmacyLicenceFieldsSchema,
  pharmacyLicenceReviewFieldsSchema,
  pharmacyLicenceEvidenceFieldsSchema,
  pharmacyLicenceRenewalNoticeSchema,
} from "./pharmacy-contract";

export { pharmacyLicenceRenewalNoticeSchema } from "./pharmacy-contract";

const id = z
  .string()
  .guid()
  .refine((value) => value !== "00000000-0000-0000-0000-000000000000");
export const pharmacyLicenceInputSchema = pharmacyLicenceFieldsSchema.superRefine((value, ctx) => {
  if (value.valid_from < "1900-01-01" || value.valid_until < value.valid_from)
    ctx.addIssue({
      code: "custom",
      path: ["valid_until"],
      message: "Use a validity end date on or after the start date.",
    });
});
export type PharmacyLicenceInput = z.infer<typeof pharmacyLicenceInputSchema>;
export const pharmacyLicenceReviewInputSchema = pharmacyLicenceReviewFieldsSchema;
export type PharmacyLicenceRenewalNotice = z.infer<typeof pharmacyLicenceRenewalNoticeSchema>;
const licenceSchema = pharmacyLicenceFieldsSchema
  .extend({
    id,
    outlet_id: id,
    entered_by: z.string().min(1).max(200),
    created_at: z.string(),
    decision: z.enum(["approved", "rejected"]).nullable(),
    decision_reason: z.string().nullable(),
    reviewed_by: z.string().nullable(),
    reviewed_at: z.string().nullable(),
    reviewed_evidence_file_id: id.nullable(),
  })
  .strict();
export type PharmacyLicence = z.infer<typeof licenceSchema>;

const evidenceSchema = pharmacyLicenceEvidenceFieldsSchema
  .omit({ sha256: true })
  .extend({
    id,
    outlet_id: id,
    licence_id: id,
    status: z.enum(["upload_pending", "scan_pending", "available", "rejected", "scan_error"]),
    scan_attempt: z.number().int().min(1).max(Number.MAX_SAFE_INTEGER),
    uploaded_at: z.string().nullable(),
    scanned_at: z.string().nullable(),
    failure_code: z
      .enum([
        "unsupported_content",
        "malware_detected",
        "scanner_unavailable",
        "storage_unavailable",
        "integrity_mismatch",
      ])
      .nullable(),
    uploader_subject: z.string().min(1).max(200),
    expires_at: z.string(),
    created_at: z.string(),
    updated_at: z.string(),
  })
  .strict();
export type PharmacyLicenceEvidence = z.infer<typeof evidenceSchema>;
export type LicenceEvidenceUpload = {
  id: string;
  file: File;
  input: z.infer<typeof pharmacyLicenceEvidenceFieldsSchema>;
};
export const pharmacyLicencesRoot = (scope: PharmacyScope) => `${pharmacyRoot(scope)}/licences`;
export const pharmacyLicenceEvidenceRoot = (scope: PharmacyScope, licence: string) =>
  `${pharmacyLicencesRoot(scope)}/${id.parse(licence)}/evidence`;

function licenceReceipt(scope: PharmacyScope, licence: string, value: unknown) {
  const parsed = licenceSchema.parse(value);
  if (parsed.id !== licence || parsed.outlet_id !== scope.outletId)
    throw new Error("The declaration response does not belong to this outlet.");
  return parsed;
}

export async function recordPharmacyLicence(
  scope: PharmacyScope,
  licence: string,
  input: PharmacyLicenceInput,
) {
  return licenceReceipt(
    scope,
    licence,
    await apiFetch(`${pharmacyLicencesRoot(scope)}/${id.parse(licence)}`, {
      method: "PUT",
      body: JSON.stringify(pharmacyLicenceInputSchema.parse(input)),
    }),
  );
}

export async function reviewPharmacyLicence(
  scope: PharmacyScope,
  licence: string,
  input: z.infer<typeof pharmacyLicenceReviewInputSchema>,
) {
  return licenceReceipt(
    scope,
    licence,
    await apiFetch(`${pharmacyLicencesRoot(scope)}/${id.parse(licence)}/review`, {
      method: "POST",
      body: JSON.stringify(pharmacyLicenceReviewInputSchema.parse(input)),
    }),
  );
}

function evidenceReceipt(scope: PharmacyScope, licence: string, file: string, value: unknown) {
  const parsed = evidenceSchema.parse(value);
  if (parsed.outlet_id !== scope.outletId || parsed.licence_id !== licence || parsed.id !== file)
    throw new Error("The evidence response does not belong to this licence.");
  return parsed;
}

export async function prepareLicenceEvidence(
  file: File,
  fileID: string,
  signal: AbortSignal,
): Promise<LicenceEvidenceUpload> {
  signal.throwIfAborted();
  if (
    !["image/png", "image/jpeg"].includes(file.type) ||
    file.size < 1 ||
    file.size > 10 * 1024 * 1024
  )
    throw new Error("Choose a PNG or JPEG image up to 10 MiB.");
  if (
    file.name !== file.name.trim() ||
    file.name === "." ||
    file.name === ".." ||
    /[/\\\p{Cc}]/u.test(file.name)
  )
    throw new Error("Use an image filename without slashes or control characters.");
  const bytes = await readAttachmentFile(file, signal);
  signal.throwIfAborted();
  const input = pharmacyLicenceEvidenceFieldsSchema.parse({
    filename: file.name,
    content_type: file.type,
    size: file.size,
    sha256: await sha256Hex(bytes),
  });
  return { id: id.parse(fileID), file, input };
}

export async function submitLicenceEvidence(
  scope: PharmacyScope,
  licence: string,
  upload: LicenceEvidenceUpload,
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  const path = `${pharmacyLicenceEvidenceRoot(scope, licence)}/${id.parse(upload.id)}`;
  const input = pharmacyLicenceEvidenceFieldsSchema.parse(upload.input);
  const intent = evidenceReceipt(
    scope,
    licence,
    upload.id,
    await apiFetch(path, { method: "PUT", body: JSON.stringify(input), signal }),
  );
  if (
    intent.filename !== input.filename ||
    intent.content_type !== input.content_type ||
    intent.size !== input.size
  )
    throw new Error("The image does not match the retained upload intent.");
  if (intent.status !== "upload_pending") return intent;
  signal.throwIfAborted();
  return evidenceReceipt(
    scope,
    licence,
    upload.id,
    await apiFetch(path + "/content", {
      method: "PUT",
      headers: { "Content-Type": input.content_type },
      body: upload.file,
      signal,
    }),
  );
}

export async function retryLicenceEvidence(
  scope: PharmacyScope,
  evidence: PharmacyLicenceEvidence,
  signal: AbortSignal,
) {
  const saved = evidenceReceipt(scope, evidence.licence_id, evidence.id, evidence);
  const attempt = z
    .number()
    .int()
    .min(2)
    .max(Number.MAX_SAFE_INTEGER)
    .parse(saved.scan_attempt + 1);
  return evidenceReceipt(
    scope,
    saved.licence_id,
    saved.id,
    await apiFetch(
      `${pharmacyLicenceEvidenceRoot(scope, saved.licence_id)}/${saved.id}/retry/${attempt}`,
      { method: "POST", signal },
    ),
  );
}

export async function downloadLicenceEvidence(
  scope: PharmacyScope,
  evidence: PharmacyLicenceEvidence,
  signal: AbortSignal,
) {
  const saved = evidenceReceipt(scope, evidence.licence_id, evidence.id, evidence);
  if (saved.status !== "available")
    throw new Error("Wait for the clean scan before downloading this image.");
  const response = await apiFetchResponse(
    `${pharmacyLicenceEvidenceRoot(scope, saved.licence_id)}/${saved.id}/download`,
    { signal },
  );
  const blob = await response.blob();
  if (blob.size !== saved.size || blob.type !== saved.content_type)
    throw new Error("The downloaded image does not match its retained metadata.");
  return blob;
}
