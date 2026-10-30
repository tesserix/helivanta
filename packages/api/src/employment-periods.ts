import { apiFetch } from "./client";
import {
  EmploymentPeriodDraftContract,
  EmploymentPeriodDraftInputContract,
  type EmploymentPeriodDraftInput,
  EmploymentPeriodWithdrawInputContract,
  type EmploymentPeriodWithdrawInput,
} from "./employment-periods-contract";
export { v7 as newEmploymentPeriodId } from "uuid";
export type EmploymentPeriodScope = {
  tenantId: string;
  subject: string;
  hospitalId: string;
  staffId: string;
};
export const employmentPeriodsPath = (scope: EmploymentPeriodScope) =>
  `/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-periods`;
export const employmentPeriodsKey = (scope: EmploymentPeriodScope) => [
  "workforce",
  "employment-periods",
  scope.tenantId,
  scope.subject,
  scope.hospitalId,
  scope.staffId,
];
export const employmentPeriodRequest = (scope: EmploymentPeriodScope) => ({
  cache: "no-store" as const,
  headers: {
    "X-Helivanta-Expected-Tenant": scope.tenantId,
    "X-Helivanta-Expected-Subject": scope.subject,
  },
});
export async function saveEmploymentPeriodDraft(
  scope: EmploymentPeriodScope,
  id: string,
  input: EmploymentPeriodDraftInput,
) {
  const command = EmploymentPeriodDraftInputContract.parse(input);
  const response = await apiFetch<unknown>(`${employmentPeriodsPath(scope)}/${id}`, {
    ...employmentPeriodRequest(scope),
    method: "PUT",
    body: JSON.stringify(command),
  });
  const parsed = EmploymentPeriodDraftContract.safeParse(response);
  if (
    !parsed.success ||
    parsed.data.status !== "draft" ||
    parsed.data.id !== id ||
    parsed.data.revision_id !== command.command_id ||
    parsed.data.staff_id !== scope.staffId ||
    parsed.data.hospital_id !== scope.hospitalId ||
    parsed.data.version !== command.expected_version + 1 ||
    parsed.data.actor_subject !== scope.subject
  )
    throw new Error(
      "Could not verify the employment draft receipt. Check history before retrying.",
    );
  return parsed.data;
}

export const employmentLifecycleReadPath = (path: string) => `${path}?lifecycle=withdrawal-v1`;
export async function withdrawEmploymentPeriod(
  scope: EmploymentPeriodScope,
  id: string,
  input: EmploymentPeriodWithdrawInput,
) {
  const command = EmploymentPeriodWithdrawInputContract.parse(input);
  const response = await apiFetch<unknown>(`${employmentPeriodsPath(scope)}/${id}/withdraw`, {
    ...employmentPeriodRequest(scope),
    method: "POST",
    body: JSON.stringify(command),
  });
  const parsed = EmploymentPeriodDraftContract.safeParse(response);
  if (
    !parsed.success ||
    parsed.data.status !== "withdrawn" ||
    parsed.data.id !== id ||
    parsed.data.revision_id !== command.command_id ||
    parsed.data.staff_id !== scope.staffId ||
    parsed.data.hospital_id !== scope.hospitalId ||
    parsed.data.version !== command.expected_version + 1 ||
    parsed.data.actor_subject !== scope.subject ||
    parsed.data.reason !== command.reason
  ) {
    throw new Error(
      "Could not verify the employment withdrawal receipt. Check history before retrying.",
    );
  }
  return parsed.data;
}
