import { describe, expect, it } from "vitest";
import { PartnerGroupContract, PartnerGroupProposalInputContract } from "./partner-group-contract";
const proposal = {
  hospital_id: "11111111-1111-4111-8111-111111111111",
  name: "Supplier receiving team",
  purpose: "receiving",
  reason: "Reviewed team preparation",
  valid_from: "2026-11-06T09:00:00Z",
  expires_at: "2026-11-07T09:00:00Z",
};
const pending = {
  ...proposal,
  id: "019ad451-1234-7123-8123-123456789012",
  status: "pending_review",
  version: 1,
  proposed_by: "synthetic-governance-user",
  membership_policy_version: 1,
  external_access_enabled: false,
  created_at: "2026-11-06T09:00:00Z",
  updated_at: "2026-11-06T09:00:00Z",
};
describe("partner group preparation contract", () => {
  it("accepts a bounded preparation without granting external access", () => {
    expect(PartnerGroupProposalInputContract.parse(proposal)).toEqual(proposal);
    expect(PartnerGroupContract.parse(pending)).toEqual(pending);
  });
  it("refuses a response that claims preparation enables external access", () => {
    expect(
      PartnerGroupContract.safeParse({ ...pending, external_access_enabled: true }).success,
    ).toBe(false);
  });
  it("refuses reversed and overlong validity or a non-canonical group", () => {
    expect(
      PartnerGroupProposalInputContract.safeParse({ ...proposal, expires_at: proposal.valid_from })
        .success,
    ).toBe(false);
    expect(
      PartnerGroupProposalInputContract.safeParse({
        ...proposal,
        expires_at: "2029-11-07T09:00:00Z",
      }).success,
    ).toBe(false);
    expect(PartnerGroupContract.safeParse({ ...pending, id: proposal.hospital_id }).success).toBe(
      false,
    );
  });
});

it("requires examined sources for nominations and both independent approval attestations", async () => {
  const { PartnerMemberProposalInputContract, PartnerMemberCommandInputContract } =
    await import("./partner-group-contract");
  const input = {
    kind: "subject",
    subject: "synthetic-member",
    staff_id: proposal.hospital_id,
    posting_id: pending.id,
    posting_revision_id: pending.id,
    posting_version: 3,
    department_id: pending.id,
    department_revision_id: pending.id,
    expected_group_version: 3,
    valid_from: proposal.valid_from,
    expires_at: proposal.expires_at,
    reason: "Reviewed current receiving Staff",
    sources_examined: true,
  };
  expect(PartnerMemberProposalInputContract.safeParse(input).success).toBe(true);
  expect(
    PartnerMemberProposalInputContract.safeParse({ ...input, sources_examined: false }).success,
  ).toBe(false);
  const decision = {
    operation: "approve",
    expected_version: 1,
    reason: "Independent source and purpose review",
  };
  expect(PartnerMemberCommandInputContract.safeParse(decision).success).toBe(false);
  expect(
    PartnerMemberCommandInputContract.safeParse({
      ...decision,
      sources_examined: true,
      purpose_eligibility_confirmed: true,
    }).success,
  ).toBe(true);
  expect(
    PartnerMemberCommandInputContract.safeParse({ ...decision, operation: "suspend" }).success,
  ).toBe(true);
});
