import { expect, it } from "vitest";
import { ProposalInputContract, ScopeRequestContract } from "./staff-access-contract";
const hospital = "22222222-2222-4222-8222-222222222222";
const proposal = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const proposalInput = {
  hospital_id: hospital,
  subject: "user-candidate0x",
  template_key: "workforce.leave_reviewer.v1",
  reason: "Independent synthetic review",
  valid_from: "2026-10-27T00:00:00Z",
  expires_at: "2026-10-28T00:00:00Z",
};

it("accepts the separately reviewed hospital identity-check templates", () => {
  for (const template_key of ["workforce.identity_manager.v1", "workforce.identity_reviewer.v1"]) {
    expect(ProposalInputContract.safeParse({ ...proposalInput, template_key }).success).toBe(true);
  }
  for (const template_key of ["tenant_admin", "workforce.identity_admin.v1", "iam.member.manage"]) {
    expect(ProposalInputContract.safeParse({ ...proposalInput, template_key }).success).toBe(false);
  }
});
it("accepts subject identifiers and rejects NUL or authority delimiters", () => {
  expect(ProposalInputContract.safeParse(proposalInput).success).toBe(true);
  for (const subject of [
    "user\0candidate",
    "user candidate",
    "user*candidate",
    "user#candidate",
    "user:candidate",
  ]) {
    expect(ProposalInputContract.safeParse({ ...proposalInput, subject }).success).toBe(false);
  }
});
it("applies the same subject constraints to owner-verification actors", () => {
  const input = {
    proposal_id: proposal,
    request_id: proposal,
    hospital_id: hospital,
    actor_subject: "user-reviewer0x",
  };
  expect(ScopeRequestContract.safeParse(input).success).toBe(true);
  expect(
    ScopeRequestContract.safeParse({ ...input, actor_subject: "user\0reviewer" }).success,
  ).toBe(false);
});
