"use client";
import {
  benefitEvidence,
  ApiError,
  useApiPagedQuery,
  type Dependent,
  type HRMaster,
  type Staff,
} from "@helivanta/api";
import type { EvidenceSource } from "../evidence-panel";

export const PLAN_STATUSES = { active: "Active", retired: "Retired" } as const;
export const DEPENDENT_STATUSES = { active: "Active", removed: "Removed" } as const;
export const PROPOSAL_STATUSES = {
  pending: "Waiting for approval",
  approved: "Approved",
  rejected: "Rejected",
  withdrawn: "Withdrawn",
} as const;

export const evidenceSource: EvidenceSource = {
  client: benefitEvidence,
  form: "Attach eligibility evidence",
  file: "Marriage, birth or other eligibility proof",
};
export const PROPOSED_EVIDENCE = "Evidence for the proposed coverage";

export const useRelationships = (hospital: string) =>
  useApiPagedQuery<HRMaster>(
    ["workforce", "relationship-masters", hospital],
    `/workforce/hospitals/${hospital}/hr/masters?kind=relationship`,
  );

export const personLabel = (
  member: Pick<Staff, "display_name">,
  dependent?: Pick<Dependent, "full_name" | "relationship_name">,
) =>
  dependent
    ? `${dependent.full_name} (${dependent.relationship_name})`
    : `${member.display_name} (employee)`;

export const period = (from: string, until?: string) =>
  until ? `${from} to ${until}` : `${from} with no end date`;

export function saveFailure(error: Error, record: string) {
  return error instanceof ApiError &&
    error.status === 409 &&
    error.detail.reason === "version_conflict"
    ? `This ${record} changed since you opened it. Close and reopen it to see the latest version.`
    : error.message;
}

export function Badge({ children }: { children: string }) {
  return (
    <span className="ml-2 rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
      {children}
    </span>
  );
}
