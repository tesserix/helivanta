import { apiFetch } from "./client";
import {
  withStaffIntakeScope,
  staffIntakeScopeKey,
  type EnrollmentRequestScope,
} from "./enrollment";
import {
  EmploymentPeriodDraftContract,
  EmploymentPeriodDraftInputContract,
  EmploymentPeriodSourceContract,
  type EmploymentPeriodSource,
  type EmploymentPeriodDraft,
  type EmploymentPeriodDraftInput,
  EmploymentPeriodWithdrawInputContract,
  type EmploymentPeriodWithdrawInput,
  pharmacyEmploymentPeriodDraftContract,
  type PharmacyEmploymentPeriodDraft,
  pharmacyEmploymentPeriodSourceContract,
  type PharmacyEmploymentPeriodSource,
} from "./employment-periods-contract";
import { pharmacyEmployerPath } from "./pharmacy-employers";
export { v7 as newEmploymentPeriodId } from "uuid";
export type HospitalEmploymentPeriodScope = EnrollmentRequestScope & {
  hospitalId: string;
  pharmacyId?: never;
  staffId: string;
};
// A pharmacy keeps its Staff's terms without an HR intake view (ADR-0054 slice 4b).
export type PharmacyEmploymentPeriodScope = Omit<EnrollmentRequestScope, "hrIntake"> & {
  hrIntake?: never;
  hospitalId?: never;
  pharmacyId: string;
  staffId: string;
};
export type EmploymentPeriodScope = HospitalEmploymentPeriodScope | PharmacyEmploymentPeriodScope;
export type EmploymentTermsOf<S extends EmploymentPeriodScope> =
  S extends PharmacyEmploymentPeriodScope ? PharmacyEmploymentPeriodDraft : EmploymentPeriodDraft;
export type EmploymentSourceOf<S extends EmploymentPeriodScope> =
  S extends PharmacyEmploymentPeriodScope ? PharmacyEmploymentPeriodSource : EmploymentPeriodSource;
const unverifiedWorkspace = () => new Error("The employment workspace could not be verified.");
function employmentOwnerPath(scope: EmploymentPeriodScope): string {
  if (scope.pharmacyId === undefined) return `/workforce/hospitals/${scope.hospitalId}`;
  if (scope.hospitalId !== undefined || scope.hrIntake !== undefined) throw unverifiedWorkspace();
  try {
    return pharmacyEmployerPath(scope.pharmacyId);
  } catch {
    throw unverifiedWorkspace();
  }
}
export const employmentStaffPath = (scope: EmploymentPeriodScope) =>
  `${employmentOwnerPath(scope)}/staff/${scope.staffId}`;
export const employmentPeriodsPath = (scope: EmploymentPeriodScope) =>
  `${employmentStaffPath(scope)}/employment-periods`;
export const employmentSourcePath = (scope: EmploymentPeriodScope) =>
  `${employmentStaffPath(scope)}/employment-source`;
// Sponsors come from the owning establishment's own Staff directory.
export const employmentSponsorsPath = (scope: EmploymentPeriodScope) =>
  `${employmentOwnerPath(scope)}/staff`;
export const employmentOwnerKey = (scope: EmploymentPeriodScope) =>
  scope.pharmacyId === undefined ? ["hospital", scope.hospitalId] : ["pharmacy", scope.pharmacyId];
export const employmentPeriodsKey = (scope: EmploymentPeriodScope) => [
  "workforce",
  "employment-periods",
  scope.tenantId,
  scope.subject,
  ...employmentOwnerKey(scope),
  scope.staffId,
  ...(scope.hrIntake === true ? [staffIntakeScopeKey(scope)] : []),
];
export const employmentPeriodRequest = (scope: EmploymentPeriodScope) => ({
  cache: "no-store" as const,
  headers: {
    "X-Helivanta-Expected-Tenant": scope.tenantId,
    "X-Helivanta-Expected-Subject": scope.subject,
  },
});
function ownedTerm<S extends EmploymentPeriodScope>(
  scope: S,
  value: unknown,
): EmploymentTermsOf<S> | undefined {
  if (scope.pharmacyId === undefined) {
    const parsed = EmploymentPeriodDraftContract.safeParse(value);
    if (!parsed.success || parsed.data.hospital_id !== scope.hospitalId) return undefined;
    if (parsed.data.staff_id !== scope.staffId) return undefined;
    // The schema was chosen by the scope's family above.
    return parsed.data as EmploymentTermsOf<S>;
  }
  const parsed = pharmacyEmploymentPeriodDraftContract.safeParse(value);
  if (!parsed.success || parsed.data.pharmacy_id !== scope.pharmacyId) return undefined;
  if (parsed.data.staff_id !== scope.staffId) return undefined;
  return parsed.data as EmploymentTermsOf<S>;
}
/** The scoped Staff record's employment basis, or undefined when it belongs to another owner or family. */
export function ownedEmploymentSource<S extends EmploymentPeriodScope>(
  scope: S,
  value: unknown,
): EmploymentSourceOf<S> | undefined {
  const parsed =
    scope.pharmacyId === undefined
      ? EmploymentPeriodSourceContract.safeParse(value)
      : pharmacyEmploymentPeriodSourceContract.safeParse(value);
  if (!parsed.success || parsed.data.staff_id !== scope.staffId) return undefined;
  const owner = "pharmacy_id" in parsed.data ? parsed.data.pharmacy_id : parsed.data.hospital_id;
  // The schema was chosen by the scope's family above.
  return owner === (scope.pharmacyId ?? scope.hospitalId)
    ? (parsed.data as EmploymentSourceOf<S>)
    : undefined;
}
/** The scoped Staff record's terms, or undefined when any row belongs to another owner or family. */
export function ownedEmploymentTerms<S extends EmploymentPeriodScope>(
  scope: S,
  value: unknown,
): EmploymentTermsOf<S>[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const rows: EmploymentTermsOf<S>[] = [];
  for (const row of value) {
    const owned = ownedTerm(scope, row);
    if (owned === undefined) return undefined;
    rows.push(owned);
  }
  return rows;
}
export async function saveEmploymentPeriodDraft<S extends EmploymentPeriodScope>(
  scope: S,
  id: string,
  input: EmploymentPeriodDraftInput,
): Promise<EmploymentTermsOf<S>> {
  const command = EmploymentPeriodDraftInputContract.parse(input);
  const path = `${employmentPeriodsPath(scope)}/${id}`;
  const response = await apiFetch<unknown>(withStaffIntakeScope(path, scope), {
    ...employmentPeriodRequest(scope),
    method: "PUT",
    body: JSON.stringify(command),
  });
  const row = ownedTerm(scope, response);
  if (
    row === undefined ||
    row.status !== "draft" ||
    row.id !== id ||
    row.revision_id !== command.command_id ||
    row.version !== command.expected_version + 1 ||
    row.actor_subject !== scope.subject
  )
    throw new Error(
      "Could not verify the employment draft receipt. Check history before retrying.",
    );
  return row;
}

export const employmentLifecycleReadPath = (path: string) => `${path}?lifecycle=withdrawal-v1`;
export async function withdrawEmploymentPeriod<S extends EmploymentPeriodScope>(
  scope: S,
  id: string,
  input: EmploymentPeriodWithdrawInput,
): Promise<EmploymentTermsOf<S>> {
  const command = EmploymentPeriodWithdrawInputContract.parse(input);
  const path = `${employmentPeriodsPath(scope)}/${id}/withdraw`;
  const response = await apiFetch<unknown>(withStaffIntakeScope(path, scope), {
    ...employmentPeriodRequest(scope),
    method: "POST",
    body: JSON.stringify(command),
  });
  const row = ownedTerm(scope, response);
  if (
    row === undefined ||
    row.status !== "withdrawn" ||
    row.id !== id ||
    row.revision_id !== command.command_id ||
    row.version !== command.expected_version + 1 ||
    row.actor_subject !== scope.subject ||
    row.reason !== command.reason
  ) {
    throw new Error(
      "Could not verify the employment withdrawal receipt. Check history before retrying.",
    );
  }
  return row;
}
