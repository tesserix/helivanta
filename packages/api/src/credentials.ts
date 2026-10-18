import { z } from "zod";
import { apiFetch } from "./client";
import {
  credentialReviewContract,
  credentialReviewInputContract,
  credentialDecisionInputContract,
  credentialWithdrawInputContract,
  credentialReminderContract,
  credentialReminderPolicyContract,
  credentialReminderPolicyInputContract,
  staffCredentialsContract,
  type CredentialDecisionInput,
  type CredentialReminder,
  type CredentialReminderPolicy,
  type CredentialReview,
  type CredentialReviewInput,
  type CredentialWithdrawInput,
  type StaffCredentials,
} from "./credential-contract";
import type { StaffDocument } from "./documents-fields";
export * from "./credential-contract";

export type CredentialScope = { hospitalId: string; staffId: string };
const identifier = credentialReviewContract.shape.id;

export const credentialsPath = (scope: CredentialScope) =>
  `/workforce/hospitals/${identifier.parse(scope.hospitalId)}/staff/${identifier.parse(scope.staffId)}/credentials`;
export const credentialReviewsPath = (scope: CredentialScope, credential: string) =>
  `${credentialsPath(scope)}/${identifier.parse(credential)}/reviews`;
export const credentialsKey = (scope: CredentialScope) => [
  "workforce",
  "credentials",
  scope.hospitalId,
  scope.staffId,
];
export const credentialRegisterKey = (hospitalId: string) => [
  "workforce",
  "credential-register",
  hospitalId,
];
export const credentialRegisterPath = (hospitalId: string, within: number | null) =>
  `/workforce/hospitals/${identifier.parse(hospitalId)}/hr/credentials${within === null ? "" : `?expiring_within=${z.number().int().min(0).max(365).parse(within)}`}`;

export async function fetchStaffCredentials(
  scope: CredentialScope,
  signal: AbortSignal,
): Promise<StaffCredentials> {
  return staffCredentialsContract.parse(
    await apiFetch(credentialsPath(scope), { cache: "no-store", signal }),
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
export const citableEvidence = (files: StaffDocument[], credential: string) =>
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
      { method: "POST", body: JSON.stringify(body) },
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
      { method: "POST", body: JSON.stringify(body) },
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
const hrPath = (hospitalId: string) => `/workforce/hospitals/${identifier.parse(hospitalId)}/hr`;
export const credentialReminderPolicyPath = (hospitalId: string) =>
  `${hrPath(hospitalId)}/credential-reminder-policy`;
export const credentialReminderPolicyKey = (hospitalId: string) => [
  "workforce",
  "credential-reminder-policy",
  hospitalId,
];
export const credentialRemindersPath = (hospitalId: string, status: CredentialReminderStatus) =>
  `${hrPath(hospitalId)}/credential-reminders?status=${credentialReminderContract.shape.status.parse(status)}`;
export const credentialRemindersKey = (hospitalId: string) => [
  "workforce",
  "credential-reminders",
  hospitalId,
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
  hospitalId: string,
  expectedVersion: number,
  thresholds: number[],
): Promise<CredentialReminderPolicy> {
  const body = credentialReminderPolicyInputContract.parse({
    expected_version: expectedVersion,
    thresholds,
  });
  return credentialReminderPolicyContract.parse(
    await apiFetch(credentialReminderPolicyPath(hospitalId), {
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}

export async function acknowledgeCredentialReminder(
  hospitalId: string,
  reminder: string,
): Promise<CredentialReminder> {
  const saved = credentialReminderContract.parse(
    await apiFetch(
      `${hrPath(hospitalId)}/credential-reminders/${identifier.parse(reminder)}/acknowledgement`,
      { method: "POST" },
    ),
  );
  if (saved.id !== reminder) throw new Error("The acknowledged reminder did not match.");
  return saved;
}
