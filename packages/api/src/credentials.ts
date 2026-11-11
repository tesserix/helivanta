import { z } from "zod";
import { apiFetch, type ApiQueryRequest } from "./client";
import {
  credentialReviewContract,
  credentialReviewInputContract,
  credentialDecisionInputContract,
  credentialWithdrawInputContract,
  credentialReminderContract,
  credentialReminderPolicyContract,
  credentialReminderPolicyInputContract,
  pharmacyTimeZoneContract,
  pharmacyTimeZoneInputContract,
  staffCredentialsContract,
  type CredentialDecisionInput,
  type CredentialReminder,
  type CredentialReminderPolicy,
  type CredentialReview,
  type CredentialReviewInput,
  type CredentialWithdrawInput,
  type PharmacyTimeZone,
  type StaffCredentials,
} from "./credential-contract";
import type { PharmacyStaffDocument, StaffDocument } from "./documents-fields";
import { pharmacyEmployerPath } from "./pharmacy-employers";
import { isSupportedTimezone } from "./roster-config";
export * from "./credential-contract";

export type HospitalCredentialOwner = {
  hospitalId: string;
  pharmacyId?: never;
  tenantId?: never;
  subject?: never;
};
// Pharmacy HR routes refuse a request that does not name the account it expects (ADR-0054).
export type PharmacyCredentialOwner = {
  pharmacyId: string;
  hospitalId?: never;
  tenantId: string;
  subject: string;
};
export type CredentialOwner = HospitalCredentialOwner | PharmacyCredentialOwner;
export type CredentialScope = CredentialOwner & { staffId: string };
const identifier = credentialReviewContract.shape.id;
const tenant = z
  .string()
  .guid()
  .refine((value) => value !== "00000000-0000-0000-0000-000000000000");
const accountSubject = z
  .string()
  .min(1)
  .max(200)
  .refine((value) => value === value.trim() && !/\p{Cc}/u.test(value));
const unverifiedWorkspace = () => new Error("The credential workspace could not be verified.");

function ownerPath(owner: CredentialOwner): string {
  if (owner.pharmacyId === undefined)
    return `/workforce/hospitals/${identifier.parse(owner.hospitalId)}`;
  if (
    owner.hospitalId !== undefined ||
    !tenant.safeParse(owner.tenantId).success ||
    !accountSubject.safeParse(owner.subject).success
  )
    throw unverifiedWorkspace();
  try {
    return pharmacyEmployerPath(owner.pharmacyId);
  } catch {
    throw unverifiedWorkspace();
  }
}
const ownerKey = (owner: CredentialOwner) =>
  owner.pharmacyId === undefined
    ? ["hospital", owner.hospitalId]
    : ["pharmacy", owner.pharmacyId, owner.tenantId, owner.subject];

export function credentialRequest(owner: CredentialOwner): ApiQueryRequest {
  ownerPath(owner);
  if (owner.pharmacyId === undefined) return { cache: "no-store" };
  return {
    cache: "no-store",
    headers: {
      "X-Helivanta-Expected-Tenant": owner.tenantId,
      "X-Helivanta-Expected-Subject": owner.subject,
    },
  };
}

export const credentialsPath = (scope: CredentialScope) =>
  `${ownerPath(scope)}/staff/${identifier.parse(scope.staffId)}/credentials`;
export const credentialReviewsPath = (scope: CredentialScope, credential: string) =>
  `${credentialsPath(scope)}/${identifier.parse(credential)}/reviews`;
export const credentialsKey = (scope: CredentialScope) => [
  "workforce",
  "credentials",
  ...ownerKey(scope),
  scope.staffId,
];
export const credentialRegisterKey = (owner: CredentialOwner) => [
  "workforce",
  "credential-register",
  ...ownerKey(owner),
];
const hrPath = (owner: CredentialOwner) => `${ownerPath(owner)}/hr`;
export const credentialRegisterPath = (owner: CredentialOwner, within: number | null) =>
  `${hrPath(owner)}/credentials${within === null ? "" : `?expiring_within=${z.number().int().min(0).max(365).parse(within)}`}`;

export async function fetchStaffCredentials(
  scope: CredentialScope,
  signal: AbortSignal,
): Promise<StaffCredentials> {
  return staffCredentialsContract.parse(
    await apiFetch(credentialsPath(scope), { ...credentialRequest(scope), signal }),
  );
}

// The form adds what the contract leaves to the server: at least one file and an ordered period.
export const credentialReviewFormSchema = credentialReviewInputContract
  .extend({
    number: z.string().trim().min(1, "Enter the number.").max(100, "Keep it to 100 characters."),
    reason: z
      .string()
      .trim()
      .min(1, "Say what you checked.")
      .max(500, "Keep it to 500 characters."),
    evidence_document_ids: z
      .array(identifier)
      .min(1, "Choose at least one evidence file.")
      .max(5, "Choose up to 5 evidence files."),
  })
  .superRefine((value, ctx) => {
    if (value.valid_until < value.valid_from)
      ctx.addIssue({
        code: "custom",
        path: ["valid_until"],
        message: "Expiry must be on or after the valid-from date.",
      });
  });
export type CredentialReviewForm = z.infer<typeof credentialReviewFormSchema>;

export const credentialDecisionFormSchema = z
  .object({ decision: z.enum(["approve", "reject"]), note: z.string().max(500) })
  .superRefine((value, ctx) => {
    if (value.decision === "reject" && !value.note.trim())
      ctx.addIssue({ code: "custom", path: ["note"], message: "Say why you reject it." });
  });
export type CredentialDecisionForm = z.infer<typeof credentialDecisionFormSchema>;

// Only this credential's evidence that is released or still scanning can be cited.
export const citableEvidence = <F extends StaffDocument | PharmacyStaffDocument>(
  files: F[],
  credential: string,
): F[] =>
  files.filter(
    (f) =>
      f.record_kind === "credential" &&
      f.record_id === credential &&
      f.category === "credential_evidence" &&
      (f.status === "available" || f.status === "scan_pending"),
  );

function owned(
  scope: CredentialScope,
  credential: string,
  review: string,
  value: unknown,
): CredentialReview {
  const saved = credentialReviewContract.parse(value);
  if (saved.id !== review || saved.staff_id !== scope.staffId || saved.credential_id !== credential)
    throw new Error("The saved review did not match this credential.");
  return saved;
}

export async function proposeCredentialReview(
  scope: CredentialScope,
  credential: string,
  review: string,
  input: CredentialReviewInput,
): Promise<CredentialReview> {
  const body = credentialReviewInputContract.parse(input);
  return owned(
    scope,
    credential,
    review,
    await apiFetch(`${credentialReviewsPath(scope, credential)}/${identifier.parse(review)}`, {
      ...credentialRequest(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}

export async function decideCredentialReview(
  scope: CredentialScope,
  credential: string,
  review: string,
  input: CredentialDecisionInput,
): Promise<CredentialReview> {
  const body = credentialDecisionInputContract.parse(input);
  return owned(
    scope,
    credential,
    review,
    await apiFetch(
      `${credentialReviewsPath(scope, credential)}/${identifier.parse(review)}/decision`,
      { ...credentialRequest(scope), method: "POST", body: JSON.stringify(body) },
    ),
  );
}

export async function withdrawCredentialReview(
  scope: CredentialScope,
  credential: string,
  review: string,
  input: CredentialWithdrawInput,
): Promise<CredentialReview> {
  const body = credentialWithdrawInputContract.parse(input);
  return owned(
    scope,
    credential,
    review,
    await apiFetch(
      `${credentialReviewsPath(scope, credential)}/${identifier.parse(review)}/withdraw`,
      { ...credentialRequest(scope), method: "POST", body: JSON.stringify(body) },
    ),
  );
}

export const credentialRegisterFilterSchema = z
  .object({
    within: z
      .string()
      .trim()
      .refine((v) => v === "" || (/^\d{1,3}$/.test(v) && Number(v) <= 365), "Use 0 to 365 days."),
  })
  .strict();
export type CredentialRegisterFilter = z.infer<typeof credentialRegisterFilterSchema>;

export type CredentialReminderStatus = CredentialReminder["status"];
export const credentialReminderPolicyPath = (owner: CredentialOwner) =>
  `${hrPath(owner)}/credential-reminder-policy`;
export const credentialReminderPolicyKey = (owner: CredentialOwner) => [
  "workforce",
  "credential-reminder-policy",
  ...ownerKey(owner),
];
export const credentialRemindersPath = (owner: CredentialOwner, status: CredentialReminderStatus) =>
  `${hrPath(owner)}/credential-reminders?status=${credentialReminderContract.shape.status.parse(status)}`;
export const credentialRemindersKey = (owner: CredentialOwner) => [
  "workforce",
  "credential-reminders",
  ...ownerKey(owner),
];

// Days are typed as a list ("90, 30, 0") and must be 1 to 6 different whole days from 0 to 365.
export const parseReminderDays = (text: string): number[] =>
  text
    .split(",")
    .map((part) => part.trim())
    .filter((part) => part !== "")
    .map(Number);
export const credentialReminderPolicyFormSchema = z
  .object({ days: z.string().max(100, "Keep it to 100 characters.") })
  .strict()
  .superRefine((value, ctx) => {
    const parts = value.days.split(",").map((part) => part.trim());
    const days = parseReminderDays(value.days);
    if (days.length === 0)
      ctx.addIssue({ code: "custom", path: ["days"], message: "Enter at least one day." });
    else if (parts.some((part) => !/^\d{1,3}$/.test(part)) || days.some((d) => d > 365))
      ctx.addIssue({
        code: "custom",
        path: ["days"],
        message: "Use whole days from 0 to 365, separated by commas.",
      });
    else if (days.length > 6)
      ctx.addIssue({ code: "custom", path: ["days"], message: "Use up to 6 reminder days." });
    else if (new Set(days).size !== days.length)
      ctx.addIssue({ code: "custom", path: ["days"], message: "List each day once." });
  });
export type CredentialReminderPolicyForm = z.infer<typeof credentialReminderPolicyFormSchema>;

export async function saveCredentialReminderPolicy(
  owner: CredentialOwner,
  expectedVersion: number,
  thresholds: number[],
): Promise<CredentialReminderPolicy> {
  const body = credentialReminderPolicyInputContract.parse({
    expected_version: expectedVersion,
    thresholds,
  });
  return credentialReminderPolicyContract.parse(
    await apiFetch(credentialReminderPolicyPath(owner), {
      ...credentialRequest(owner),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}

export async function acknowledgeCredentialReminder(
  owner: CredentialOwner,
  reminder: string,
): Promise<CredentialReminder> {
  const saved = credentialReminderContract.parse(
    await apiFetch(
      `${hrPath(owner)}/credential-reminders/${identifier.parse(reminder)}/acknowledgement`,
      { ...credentialRequest(owner), method: "POST" },
    ),
  );
  if (saved.id !== reminder) throw new Error("The acknowledged reminder did not match.");
  return saved;
}

export const pharmacyTimeZonePath = (owner: PharmacyCredentialOwner) =>
  `${hrPath(owner)}/time-zone`;
export const pharmacyTimeZoneKey = (owner: PharmacyCredentialOwner) => [
  "workforce",
  "pharmacy-time-zone",
  ...ownerKey(owner),
];
// Version 0 is the unsaved row; every saved version names a supported zone.
export async function fetchPharmacyTimeZone(
  owner: PharmacyCredentialOwner,
  signal?: AbortSignal,
): Promise<PharmacyTimeZone> {
  const zone = pharmacyTimeZoneContract.parse(
    await apiFetch(pharmacyTimeZonePath(owner), { ...credentialRequest(owner), signal }),
  );
  const saved = zone.time_zone !== undefined;
  if (
    zone.version < 0 ||
    saved !== zone.version > 0 ||
    (saved && !isSupportedTimezone(zone.time_zone!))
  )
    throw new Error("The pharmacy time zone could not be verified. Reload and try again.");
  return zone;
}

export const pharmacyTimeZoneFormSchema = z
  .object({
    time_zone: z
      .string()
      .trim()
      .refine(isSupportedTimezone, "Use a supported IANA time zone such as Asia/Kolkata."),
  })
  .strict();
export type PharmacyTimeZoneForm = z.infer<typeof pharmacyTimeZoneFormSchema>;

// A replay of the current zone returns the saved row at its own version, so only the zone is compared.
export async function savePharmacyTimeZone(
  owner: PharmacyCredentialOwner,
  expectedVersion: number,
  timeZone: string,
): Promise<PharmacyTimeZone> {
  const body = pharmacyTimeZoneInputContract.parse({
    expected_version: expectedVersion,
    time_zone: timeZone,
  });
  const saved = pharmacyTimeZoneContract.parse(
    await apiFetch(pharmacyTimeZonePath(owner), {
      ...credentialRequest(owner),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  if (saved.version < 1 || saved.time_zone !== body.time_zone)
    throw new Error("The saved time zone did not match. Reload before saving again.");
  return saved;
}
