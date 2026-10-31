import { expect, it } from "vitest";
import { PersonReviewSubmissionInputContract } from "./person-review-contract";

it("requires protected identity evidence before a person-review submission", () => {
  const input = {
    command_id: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa",
    staff_version: 1,
    employment_epoch: 1,
    identity_hash: "a".repeat(64),
    policy_reference: "SYN-HUMAN-IDENTITY-POLICY",
    finding_reference: "Synthetic in-person examination",
    observed_on: "2026-10-31",
    valid_until: "2027-01-01T00:00:00Z",
    access_intent: "non_digital",
    declared_subject: "",
    identity_examined: true,
    known_associations_disclosed: true,
    document_ids: [],
  };
  expect(PersonReviewSubmissionInputContract.safeParse(input).success).toBe(false);
  expect(
    PersonReviewSubmissionInputContract.safeParse({
      ...input,
      document_ids: ["019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb"],
    }).success,
  ).toBe(true);
});
