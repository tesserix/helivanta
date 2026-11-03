import { z } from "zod";
import { apiFetch } from "./client";
import { enrollmentRequestOptions } from "./enrollment";
import { EmploymentPeriodSourceContract } from "./employment-periods-contract";
import { tenantTransitionPending } from "./tenant-transition";
import {
  StaffPostingDraftContract,
  StaffPostingDraftInputContract,
  staffPostingInstantMicros,
  type StaffPostingDraftInput,
  type StaffPostingDraft,
} from "./staff-postings-contract";

export { v7 as newStaffPostingId } from "uuid";
export type StaffPostingScope = {
  tenantId: string;
  subject: string;
  hospitalId: string;
  staffId: string;
  access?: "legacy" | "scoped";
};
const identifier = z
  .string()
  .guid()
  .refine((value) => value !== "00000000-0000-0000-0000-000000000000")
  .transform((value) => value.toLowerCase());
const commandId = z.uuidv7().transform((value) => value.toLowerCase());

export const staffPostingsKey = (scope: StaffPostingScope) => [
  "workforce",
  "posting-drafts",
  scope.access ?? "legacy",
  scope.tenantId,
  scope.subject,
  scope.hospitalId,
  scope.staffId,
];
function request(scope: StaffPostingScope) {
  identifier.parse(scope.tenantId);
  z.string()
    .min(1)
    .max(200)
    .refine((value) => value === value.trim() && !/\p{Cc}/u.test(value))
    .parse(scope.subject);
  if (tenantTransitionPending()) throw new Error("Wait for your workspace to finish switching.");
  return enrollmentRequestOptions(scope);
}
function root(scope: StaffPostingScope) {
  return `/workforce/hospitals/${identifier.parse(scope.hospitalId)}/${scope.access === "scoped" ? "posting-staff" : "staff"}/${identifier.parse(scope.staffId)}`;
}
function sourceMatches(row: { hospital_id: string; staff_id: string }, scope: StaffPostingScope) {
  return (
    identifier.parse(row.hospital_id) === identifier.parse(scope.hospitalId) &&
    identifier.parse(row.staff_id) === identifier.parse(scope.staffId)
  );
}
function verifyDraft(
  raw: unknown,
  scope: StaffPostingScope,
  draft?: string,
  intent?: StaffPostingDraftInput,
): StaffPostingDraft {
  const parsed = StaffPostingDraftContract.safeParse(raw);
  if (!parsed.success)
    throw new Error(
      "The retained Staff posting receipt could not be verified. Check the exact command before retrying.",
    );
  const row = parsed.data;
  commandId.parse(row.id);
  commandId.parse(row.revision_id);
  if (
    !sourceMatches(row, scope) ||
    (draft !== undefined && identifier.parse(row.id) !== identifier.parse(draft)) ||
    (intent &&
      (row.version !== intent.expected_version + 1 ||
        row.staff_version !== intent.staff_version ||
        row.employment_epoch !== intent.employment_epoch ||
        identifier.parse(row.department_id) !== identifier.parse(intent.department_id) ||
        identifier.parse(row.department_revision_id) !==
          identifier.parse(intent.department_revision_id) ||
        staffPostingInstantMicros(row.starts_at) !== staffPostingInstantMicros(intent.starts_at) ||
        (row.ends_at ? staffPostingInstantMicros(row.ends_at) : null) !==
          (intent.ends_at ? staffPostingInstantMicros(intent.ends_at) : null)))
  )
    throw new Error(
      "The retained Staff posting receipt could not be verified. Check the exact command before retrying.",
    );
  return row;
}
export async function getStaffPostingDraftSource(scope: StaffPostingScope, signal?: AbortSignal) {
  const source = EmploymentPeriodSourceContract.parse(
    await apiFetch(`${root(scope)}/posting-draft-source`, { ...request(scope), signal }),
  );
  if (!sourceMatches(source, scope))
    throw new Error("The Staff posting source could not be verified.");
  return source;
}
export async function saveStaffPostingDraft(
  scope: StaffPostingScope,
  draft: string,
  command: string,
  input: StaffPostingDraftInput,
  signal?: AbortSignal,
) {
  const intent = StaffPostingDraftInputContract.parse(input);
  const raw = await apiFetch(
    `${root(scope)}/posting-drafts/${commandId.parse(draft)}/commands/${commandId.parse(command)}`,
    {
      ...request(scope),
      method: "PUT",
      body: JSON.stringify(intent),
      signal,
    },
  );
  return verifyDraft(raw, scope, draft, intent);
}

export async function getStaffPostingDraftReceipt(
  scope: StaffPostingScope,
  draft: string,
  command: string,
  input: StaffPostingDraftInput,
  signal?: AbortSignal,
) {
  const intent = StaffPostingDraftInputContract.parse(input);
  return verifyDraft(
    await apiFetch(
      `${root(scope)}/posting-drafts/${commandId.parse(draft)}/commands/${commandId.parse(command)}`,
      { ...request(scope), signal },
    ),
    scope,
    draft,
    intent,
  );
}

const cursorSchema = z.string().min(1).max(2048);
const pageSchema = z
  .object({
    data: z.array(StaffPostingDraftContract).max(50),
    page: z.object({ has_more: z.boolean(), next_cursor: cursorSchema.nullable() }).strict(),
  })
  .strict();
export async function listStaffPostingDraftHistory(
  scope: StaffPostingScope,
  draft: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  return listDraftPage(scope, commandId.parse(draft), cursor, signal);
}
export async function listStaffPostingDrafts(
  scope: StaffPostingScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  return listDraftPage(scope, undefined, cursor, signal);
}
async function listDraftPage(
  scope: StaffPostingScope,
  draft: string | undefined,
  cursor: string | null,
  signal?: AbortSignal,
) {
  const params = new URLSearchParams({ limit: "50" });
  if (cursor !== null) params.set("cursor", cursorSchema.parse(cursor));
  const page = pageSchema.parse(
    await apiFetch(`${root(scope)}/posting-drafts${draft ? `/${draft}/history` : ""}?${params}`, {
      ...request(scope),
      signal,
    }),
  );
  page.data.forEach((row) => verifyDraft(row, scope, draft));
  if (
    new Set(page.data.map((row) => (draft ? row.revision_id : row.id).toLowerCase())).size !==
      page.data.length ||
    page.page.has_more !== Boolean(page.page.next_cursor) ||
    (cursor !== null && page.page.next_cursor === cursor)
  )
    throw new Error("The Staff posting page could not be verified.");
  return page;
}
