import { z } from "zod";
import { apiFetch, apiFetchResponse } from "./client";
import { readAttachmentFile } from "./attachment-file";
import { supplyEvidenceInputSchema as generatedEvidenceInputSchema } from "./supply-contract";
import { supplyRegistryKey, type SupplyRegistryScope } from "./supply-registration";
import { supplyHeaders } from "./supply";

const filename = generatedEvidenceInputSchema.shape.filename.refine(
  (value) =>
    value === value.trim() && value !== "." && value !== ".." && !/[\\/\p{Cc}]/u.test(value),
  "Choose an image with a safe filename.",
);
export const supplyEvidenceFormSchema = generatedEvidenceInputSchema.extend({ filename }).strict();
const identifier = z
  .string()
  .guid()
  .refine((value) => value !== "00000000-0000-0000-0000-000000000000");
const version = z.number().int().safe().min(1);
export const supplyEvidenceSchema = supplyEvidenceFormSchema
  .omit({ expected_license_version: true, sha256: true })
  .extend({
    id: identifier,
    party_id: identifier,
    license_id: identifier,
    license_version: version,
    status: z.enum([
      "upload_pending",
      "scan_pending",
      "available",
      "rejected",
      "scan_error",
      "revoked",
    ]),
    scan_attempt: version,
    uploaded_at: z.string().datetime({ offset: true }).nullable(),
    scanned_at: z.string().datetime({ offset: true }).nullable(),
    failure_code: z
      .enum([
        "unsupported_content",
        "malware_detected",
        "scanner_unavailable",
        "storage_unavailable",
        "integrity_mismatch",
      ])
      .nullable(),
    uploader_subject: z.string(),
    created_at: z.string().datetime({ offset: true }),
    updated_at: z.string().datetime({ offset: true }),
    expires_at: z.string().datetime({ offset: true }),
  });
export type SupplyEvidence = z.infer<typeof supplyEvidenceSchema>;
export type SupplyEvidenceScope = SupplyRegistryScope & { licenseId: string };
export type SupplyEvidenceUpload = {
  id: string;
  file: File;
  input: z.infer<typeof supplyEvidenceFormSchema>;
};
export const supplyEvidenceKey = (scope: SupplyEvidenceScope) =>
  supplyRegistryKey(scope, "evidence", scope.licenseId);
export function supplyEvidenceRoot(scope: SupplyEvidenceScope) {
  identifier.parse(scope.tenantId);
  identifier.parse(scope.partyId);
  identifier.parse(scope.licenseId);
  return `/supply/parties/${scope.partyId}/licenses/${scope.licenseId}/evidence`;
}
export function supplyEvidenceHeaders(scope: SupplyEvidenceScope) {
  supplyEvidenceRoot(scope);
  return supplyHeaders(scope);
}
export function supplyEvidenceReceipt(scope: SupplyEvidenceScope, value: unknown, id?: string) {
  const saved = supplyEvidenceSchema.parse(value);
  if (
    saved.party_id !== scope.partyId ||
    saved.license_id !== scope.licenseId ||
    (id !== undefined && saved.id !== id)
  )
    throw new Error("Evidence ownership does not match this workspace.");
  return saved;
}
export async function prepareSupplyEvidence(
  id: string,
  file: File,
  licenseVersion: number,
  signal: AbortSignal,
): Promise<SupplyEvidenceUpload> {
  signal.throwIfAborted();
  identifier.parse(id);
  const input = supplyEvidenceFormSchema.parse({
    expected_license_version: licenseVersion,
    filename: file.name,
    content_type: file.type,
    size: file.size,
    sha256: "0".repeat(64),
  });
  const data = await readAttachmentFile(file, signal);
  signal.throwIfAborted();
  if (data.byteLength !== input.size)
    throw new Error("The selected image could not be read safely.");
  const digest = await crypto.subtle.digest("SHA-256", data);
  signal.throwIfAborted();
  input.sha256 = Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
  return { id, file, input };
}
export async function submitSupplyEvidence(
  scope: SupplyEvidenceScope,
  upload: SupplyEvidenceUpload,
  signal: AbortSignal,
): Promise<SupplyEvidence> {
  signal.throwIfAborted();
  const input = supplyEvidenceFormSchema.parse(upload.input);
  const path = `${supplyEvidenceRoot(scope)}/${identifier.parse(upload.id)}`;
  const intent = supplyEvidenceReceipt(
    scope,
    await apiFetch(path, {
      method: "PUT",
      headers: supplyEvidenceHeaders(scope),
      body: JSON.stringify(input),
      signal,
    }),
    upload.id,
  );
  if (
    intent.license_version !== input.expected_license_version ||
    intent.filename !== input.filename ||
    intent.size !== input.size ||
    intent.content_type !== input.content_type
  )
    throw new Error("The upload does not match the saved licence evidence request.");
  signal.throwIfAborted();
  if (intent.status !== "upload_pending") return intent;
  const headers = supplyEvidenceHeaders(scope);
  headers.set("Content-Type", input.content_type);
  return supplyEvidenceReceipt(
    scope,
    await apiFetch(path + "/content", { method: "PUT", headers, body: upload.file, signal }),
    upload.id,
  );
}
export async function changeSupplyEvidence(
  scope: SupplyEvidenceScope,
  file: SupplyEvidence,
  action: "retry" | "revoke",
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  supplyEvidenceReceipt(scope, file, file.id);
  const suffix = action === "retry" ? `retry/${version.parse(file.scan_attempt + 1)}` : "revoke";
  return supplyEvidenceReceipt(
    scope,
    await apiFetch(`${supplyEvidenceRoot(scope)}/${file.id}/${suffix}`, {
      method: "POST",
      headers: supplyEvidenceHeaders(scope),
      signal,
    }),
    file.id,
  );
}
export async function downloadSupplyEvidence(
  scope: SupplyEvidenceScope,
  file: SupplyEvidence,
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  supplyEvidenceReceipt(scope, file, file.id);
  if (file.status !== "available") throw new Error("This image is not ready for download.");
  const res = await apiFetchResponse(`${supplyEvidenceRoot(scope)}/${file.id}/download`, {
    headers: supplyEvidenceHeaders(scope),
    cache: "no-store",
    signal,
  });
  const blob = await res.blob();
  signal.throwIfAborted();
  if (blob.size !== file.size || blob.type !== file.content_type)
    throw new Error("The evidence download could not be verified.");
  return blob;
}
