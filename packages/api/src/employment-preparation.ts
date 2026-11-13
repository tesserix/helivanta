import { z } from "zod";
import { apiFetch } from "./client";
import { personReviewRequest } from "./person-reviews";
import {
  EmploymentPeriodSourceContract,
  EmploymentPeriodDraftContract,
  EmploymentPeriodDraftInputContract,
  type EmploymentPeriodDraftInput,
  type EmploymentPeriodDraft,
} from "./employment-periods-contract";

import {
  PersonReviewTargetSearchInputContract,
  PersonReviewTargetContract,
  type PersonReviewTargetSearchInput,
} from "./person-review-contract";

export const governanceEmploymentPreparationScopeContract = z
  .object({
    tenantId: EmploymentPeriodSourceContract.shape.hospital_id,
    subject: z
      .string()
      .min(1)
      .max(200)
      .refine((value) => !value.includes("\0")),
    hospitalId: EmploymentPeriodSourceContract.shape.hospital_id,
    staffId: EmploymentPeriodSourceContract.shape.staff_id,
  })
  .strict();
export type GovernanceEmploymentPreparationScope = z.infer<
  typeof governanceEmploymentPreparationScopeContract
>;
const periodId = z
  .string()
  .uuid()
  .refine((value) => value[14] === "7", "Choose a real employment draft.");
const pageContract = z
  .object({
    data: z.array(EmploymentPeriodDraftContract).max(50),
    page: z
      .object({ has_more: z.boolean(), next_cursor: z.string().min(1).max(2048).nullable() })
      .strict(),
  })
  .strict();
const unverified = () =>
  new Error(
    "The employment preparation response could not be verified. Refresh before continuing.",
  );
const unverifiedReceipt = () =>
  new Error("The employment draft receipt could not be verified. Check history before retrying.");
function workspace(value: GovernanceEmploymentPreparationScope) {
  const scope = governanceEmploymentPreparationScopeContract.parse(value);
  return {
    scope,
    path: `/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}`,
    request: personReviewRequest(scope),
  };
}
function owned(scope: GovernanceEmploymentPreparationScope, row: EmploymentPeriodDraft) {
  return (
    periodId.safeParse(row.id).success &&
    periodId.safeParse(row.revision_id).success &&
    row.hospital_id === scope.hospitalId &&
    row.staff_id === scope.staffId
  );
}

export async function getGovernanceEmploymentSource(
  scope: GovernanceEmploymentPreparationScope,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const current = workspace(scope);
  const response = await apiFetch<unknown>(`${current.path}/employment-preparation-source`, {
    ...current.request,
    signal,
  });
  signal?.throwIfAborted();
  const parsed = EmploymentPeriodSourceContract.safeParse(response);
  if (
    !parsed.success ||
    parsed.data.hospital_id !== current.scope.hospitalId ||
    parsed.data.staff_id !== current.scope.staffId
  )
    throw unverified();
  return parsed.data;
}

export async function getGovernanceEmploymentDraft(
  scope: GovernanceEmploymentPreparationScope,
  id: string,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const current = workspace(scope);
  const selected = periodId.parse(id);
  const response = await apiFetch<unknown>(
    `${current.path}/employment-preparation-periods/${selected}?lifecycle=withdrawal-v1`,
    { ...current.request, signal },
  );
  signal?.throwIfAborted();
  const parsed = EmploymentPeriodDraftContract.safeParse(response);
  if (!parsed.success || !owned(current.scope, parsed.data) || parsed.data.id !== selected)
    throw unverified();
  return parsed.data;
}

async function listPreparation(
  scope: GovernanceEmploymentPreparationScope,
  id: string | null,
  cursor: string | null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const current = workspace(scope);
  const selected = id === null ? null : periodId.parse(id);
  const query = new URLSearchParams({ limit: "50" });
  if (selected !== null) query.set("lifecycle", "withdrawal-v1");
  if (cursor !== null) query.set("cursor", z.string().min(1).max(2048).parse(cursor));
  const suffix = selected === null ? "" : `/${selected}/history`;
  const response = await apiFetch<unknown>(
    `${current.path}/employment-preparation-periods${suffix}?${query}`,
    { ...current.request, signal },
  );
  signal?.throwIfAborted();
  const parsed = pageContract.safeParse(response);
  if (!parsed.success || parsed.data.page.has_more !== Boolean(parsed.data.page.next_cursor))
    throw unverified();
  const rows = parsed.data.data;
  if (
    rows.some(
      (row) =>
        !owned(current.scope, row) ||
        (selected === null ? row.status !== "draft" : row.id !== selected),
    )
  )
    throw unverified();
  const keys = rows.map((row) => (selected === null ? row.id : row.revision_id).toLowerCase());
  if (
    new Set(keys).size !== keys.length ||
    (selected !== null && new Set(rows.map((row) => row.version)).size !== rows.length)
  )
    throw unverified();
  return parsed.data;
}

export function listGovernanceEmploymentDrafts(
  scope: GovernanceEmploymentPreparationScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  return listPreparation(scope, null, cursor, signal);
}
export function listGovernanceEmploymentHistory(
  scope: GovernanceEmploymentPreparationScope,
  id: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  return listPreparation(scope, id, cursor, signal);
}

export async function saveGovernanceEmploymentDraft(
  scope: GovernanceEmploymentPreparationScope,
  id: string,
  input: EmploymentPeriodDraftInput,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const current = workspace(scope);
  const selected = periodId.parse(id);
  const command = EmploymentPeriodDraftInputContract.parse(input);
  periodId.parse(command.command_id);
  EmploymentPeriodDraftContract.shape.version.parse(command.expected_version + 1);
  const response = await apiFetch<unknown>(
    `${current.path}/employment-preparation-periods/${selected}`,
    { ...current.request, signal, method: "PUT", body: JSON.stringify(command) },
  );
  signal?.throwIfAborted();
  const parsed = EmploymentPeriodDraftContract.safeParse(response);
  if (
    !parsed.success ||
    !owned(current.scope, parsed.data) ||
    parsed.data.id !== selected ||
    parsed.data.status !== "draft" ||
    parsed.data.revision_id !== command.command_id ||
    parsed.data.version !== command.expected_version + 1 ||
    parsed.data.actor_subject !== current.scope.subject ||
    parsed.data.staff_version !== command.staff_version ||
    parsed.data.employment_epoch !== command.employment_epoch
  )
    throw unverifiedReceipt();
  return parsed.data;
}

const targetScopeContract = governanceEmploymentPreparationScopeContract
  .omit({ staffId: true })
  .strict();
export type GovernanceEmploymentTargetScope = z.infer<typeof targetScopeContract>;
const targetsPageContract = pageContract.extend({
  data: z.array(PersonReviewTargetContract).max(20),
});
export async function searchGovernanceEmploymentTargets(
  scope: GovernanceEmploymentTargetScope,
  input: PersonReviewTargetSearchInput,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const current = targetScopeContract.parse(scope);
  const body = PersonReviewTargetSearchInputContract.parse(input);
  const query = new URLSearchParams({ limit: "20" });
  if (cursor !== null) query.set("cursor", z.string().min(1).max(2048).parse(cursor));
  const response = await apiFetch<unknown>(
    `/workforce/hospitals/${current.hospitalId}/employment-preparation-targets/search?${query}`,
    {
      ...personReviewRequest(current),
      method: "POST",
      body: JSON.stringify(body),
      signal,
    },
  );
  signal?.throwIfAborted();
  const parsed = targetsPageContract.safeParse(response);
  if (!parsed.success) throw unverified();
  const { data, page } = parsed.data;
  if (
    page.has_more !== (page.next_cursor !== null) ||
    (page.has_more && data.length !== 20) ||
    data.some(
      (row) =>
        row.hospital_id !== current.hospitalId ||
        (body.employee_code !== "" && row.employee_code !== body.employee_code.toUpperCase()),
    ) ||
    new Set(data.map((row) => row.id.toLowerCase())).size !== data.length
  )
    throw unverified();
  return parsed.data;
}
