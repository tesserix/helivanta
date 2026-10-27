import { z } from "zod";
import { apiFetch, apiFetchResponse } from "./client";
import { readAttachmentFile, sha256Hex } from "./attachment-file";
import { pharmacyKey, pharmacyRoot, type PharmacyScope } from "./pharmacy";

// Prescription evidence (#1386 B-2a): the outlet's prescription record and
// its scanned pages, released only after the pharmacy evidence worker scans them.

const identifier = z
  .string()
  .guid()
  .refine((value) => value !== "00000000-0000-0000-0000-000000000000");
const text = (max: number, label: string) =>
  z
    .string()
    .trim()
    .min(1, `Enter the ${label}`)
    .max(max, `Use at most ${max} characters`)
    .refine((value) => !/\p{Cc}/u.test(value), `Remove control characters from the ${label}`);

export const prescriptionInputSchema = z
  .object({
    prescriber_name: text(200, "prescriber's name"),
    prescriber_registration: z
      .string()
      .trim()
      .regex(
        /^[A-Za-z0-9][A-Za-z0-9/ .-]{0,39}$/,
        "Use the registration number as printed: letters, digits, / . - and spaces",
      ),
    prescriber_council: text(120, "council"),
    prescribed_on: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, "Enter the prescription date"),
    patient_name: text(200, "patient's name"),
    patient_address: text(500, "patient's address").optional(),
  })
  .strict();
export type PrescriptionInput = z.infer<typeof prescriptionInputSchema>;

export const prescriptionSchema = prescriptionInputSchema.extend({
  id: identifier,
  outlet_id: identifier,
  patient_address: z.string().nullable(),
  recorded_by: z.string(),
  created_at: z.string().datetime({ offset: true }),
});
export type PharmacyPrescription = z.infer<typeof prescriptionSchema>;

const filename = z
  .string()
  .min(1)
  .max(180)
  .refine(
    (value) =>
      value === value.trim() && value !== "." && value !== ".." && !/[\\/\p{Cc}]/u.test(value),
    "Choose an image with a safe filename.",
  );
export const prescriptionImageInputSchema = z
  .object({
    filename,
    content_type: z.enum(["image/png", "image/jpeg"]),
    size: z.number().int().min(1).max(10_485_760),
    sha256: z.string().regex(/^[a-f0-9]{64}$/),
  })
  .strict();
const attempt = z.number().int().safe().min(1);
export const prescriptionImageSchema = prescriptionImageInputSchema.omit({ sha256: true }).extend({
  id: identifier,
  outlet_id: identifier,
  prescription_id: identifier,
  status: z.enum(["upload_pending", "scan_pending", "available", "rejected", "scan_error"]),
  scan_attempt: attempt,
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
export type PharmacyPrescriptionImage = z.infer<typeof prescriptionImageSchema>;
export type PrescriptionImageUpload = {
  id: string;
  file: File;
  input: z.infer<typeof prescriptionImageInputSchema>;
};

export const prescriptionImagesKey = (scope: PharmacyScope, prescriptionId: string) =>
  pharmacyKey(scope, "prescriptions", prescriptionId, "images");
export function prescriptionImagesRoot(scope: PharmacyScope, prescriptionId: string) {
  return `${pharmacyRoot(scope)}/prescriptions/${identifier.parse(prescriptionId)}/images`;
}
export function prescriptionImageReceipt(
  scope: PharmacyScope,
  prescriptionId: string,
  value: unknown,
  id?: string,
) {
  const saved = prescriptionImageSchema.parse(value);
  if (
    saved.outlet_id !== scope.outletId ||
    saved.prescription_id !== prescriptionId ||
    (id !== undefined && saved.id !== id)
  )
    throw new Error("This page does not belong to the prescription on screen.");
  return saved;
}

export async function preparePrescriptionImage(
  id: string,
  file: File,
  signal: AbortSignal,
): Promise<PrescriptionImageUpload> {
  signal.throwIfAborted();
  identifier.parse(id);
  const data = await readAttachmentFile(file, signal);
  signal.throwIfAborted();
  if (data.byteLength !== file.size)
    throw new Error("The selected image could not be read safely.");
  const input = prescriptionImageInputSchema.parse({
    filename: file.name,
    content_type: file.type,
    size: file.size,
    sha256: await sha256Hex(data),
  });
  signal.throwIfAborted();
  return { id, file, input };
}

export async function submitPrescriptionImage(
  scope: PharmacyScope,
  prescriptionId: string,
  upload: PrescriptionImageUpload,
  signal: AbortSignal,
): Promise<PharmacyPrescriptionImage> {
  signal.throwIfAborted();
  const input = prescriptionImageInputSchema.parse(upload.input);
  const path = `${prescriptionImagesRoot(scope, prescriptionId)}/${identifier.parse(upload.id)}`;
  const intent = prescriptionImageReceipt(
    scope,
    prescriptionId,
    await apiFetch(path, { method: "PUT", body: JSON.stringify(input), signal }),
    upload.id,
  );
  if (
    intent.filename !== input.filename ||
    intent.size !== input.size ||
    intent.content_type !== input.content_type
  )
    throw new Error("The upload does not match the saved page.");
  signal.throwIfAborted();
  if (intent.status !== "upload_pending") return intent;
  return prescriptionImageReceipt(
    scope,
    prescriptionId,
    await apiFetch(path + "/content", {
      method: "PUT",
      headers: { "Content-Type": input.content_type },
      body: upload.file,
      signal,
    }),
    upload.id,
  );
}

export async function retryPrescriptionImage(
  scope: PharmacyScope,
  image: PharmacyPrescriptionImage,
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  prescriptionImageReceipt(scope, image.prescription_id, image, image.id);
  return prescriptionImageReceipt(
    scope,
    image.prescription_id,
    await apiFetch(
      `${prescriptionImagesRoot(scope, image.prescription_id)}/${image.id}/retry/${attempt.parse(image.scan_attempt + 1)}`,
      { method: "POST", signal },
    ),
    image.id,
  );
}

export async function downloadPrescriptionImage(
  scope: PharmacyScope,
  image: PharmacyPrescriptionImage,
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  prescriptionImageReceipt(scope, image.prescription_id, image, image.id);
  if (image.status !== "available") throw new Error("This page is not ready to open.");
  const res = await apiFetchResponse(
    `${prescriptionImagesRoot(scope, image.prescription_id)}/${image.id}/download`,
    { cache: "no-store", signal },
  );
  const blob = await res.blob();
  signal.throwIfAborted();
  if (blob.size !== image.size || blob.type !== image.content_type)
    throw new Error("The page download could not be verified.");
  return blob;
}
