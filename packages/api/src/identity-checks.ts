import { apiFetch } from "./client";
import {
  IdentityCaseContract,
  IdentityCaseDraftInputContract,
  type IdentityCaseDraftInput,
} from "./identity-checks-contract";

export { v7 as newIdentityCaseId } from "uuid";

export type IdentityCaseScope = {
  tenantId: string;
  subject: string;
  hospitalId: string;
  staffId: string;
};

export const identityCasesKey = (scope: IdentityCaseScope) => [
  "workforce",
  "identity-cases",
  scope.tenantId,
  scope.subject,
  scope.hospitalId,
  scope.staffId,
];

export const identityCasesPath = (scope: IdentityCaseScope) =>
  `/workforce/hospitals/${scope.hospitalId}/identity-staff/${scope.staffId}/checks`;

export const identityCaseRequest = (scope: IdentityCaseScope) => ({
  cache: "no-store" as const,
  headers: {
    "X-Helivanta-Expected-Tenant": scope.tenantId,
    "X-Helivanta-Expected-Subject": scope.subject,
  },
});

export async function saveIdentityCaseDraft(
  scope: IdentityCaseScope,
  id: string,
  input: IdentityCaseDraftInput,
) {
  const command = IdentityCaseDraftInputContract.parse(input);
  const response = await apiFetch<unknown>(`${identityCasesPath(scope)}/${id}`, {
    ...identityCaseRequest(scope),
    method: "PUT",
    body: JSON.stringify(command),
  });
  const parsed = IdentityCaseContract.safeParse(response);
  if (
    !parsed.success ||
    parsed.data.id !== id ||
    parsed.data.revision_id !== command.command_id ||
    parsed.data.hospital_id !== scope.hospitalId ||
    parsed.data.staff_id !== scope.staffId ||
    parsed.data.version !== command.expected_version + 1 ||
    parsed.data.status !== "draft"
  ) {
    throw new Error(
      "Could not validate the saved draft receipt. Check history before starting another request.",
    );
  }
  return parsed.data;
}
