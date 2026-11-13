import { expect, it } from "vitest";
import {
  GovernanceGrantInputContract,
  GovernanceGrantDecisionInputContract,
  governanceTemplates,
} from "./staff-governance-contract";

const input = {
  subject: "synthetic-reviewer",
  expected_membership_epoch: 1,
  template_key: "workforce.account_link_reviewer.v1",
  scope_kind: "hospital",
  scope_id: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa",
  reason: "Independently review Staff account links",
  valid_from: "2026-11-13T00:00:00Z",
  expires_at: "2026-11-13T01:00:00Z",
};
it("rejects control characters in an attributable governance reason", () => {
  expect(
    GovernanceGrantInputContract.safeParse({
      ...input,
      reason: "review\nwithout retained attribution",
    }).success,
  ).toBe(false);
});
it("binds fixed templates to exactly one employer or tenant governance", () => {
  for (const scope_kind of ["hospital", "pharmacy", "supplier"])
    expect(GovernanceGrantInputContract.parse({ ...input, scope_kind })).toMatchObject({
      scope_kind,
    });
  const { scope_id, ...tenant } = input;
  expect(GovernanceGrantInputContract.safeParse({ ...tenant, scope_kind: "tenant" }).success).toBe(
    false,
  );
  expect(
    GovernanceGrantInputContract.parse({
      ...tenant,
      scope_kind: "tenant",
      template_key: "iam.governance_auditor.v1",
    }),
  ).toMatchObject({ scope_kind: "tenant" });
  expect(
    GovernanceGrantInputContract.safeParse({ ...input, template_key: "iam.governance_auditor.v1" })
      .success,
  ).toBe(false);
  expect(scope_id).toBe(input.scope_id);
});
it.each(["actor_subject", "tenant_id", "role_key", "verified", "permissions"])(
  "refuses supplied authority field %s",
  (field) => {
    expect(GovernanceGrantInputContract.safeParse({ ...input, [field]: "forged" }).success).toBe(
      false,
    );
  },
);
it.each([
  { expected_membership_epoch: 0 },
  { expected_membership_epoch: 9007199254740992 },
  { scope_id: null },
  { scope_id: "00000000-0000-0000-0000-000000000000" },
  { reason: "   " },
  { subject: "*" },
  { expires_at: input.valid_from },
  { expires_at: "invalid" },
  { scope_kind: "department" },
  { template_key: "tenant_admin" },
])("refuses invalid or broadened input %j", (change) => {
  expect(GovernanceGrantInputContract.safeParse({ ...input, ...change }).success).toBe(false);
});
it("keeps the versioned governance bundles free of operating authority", () => {
  expect(
    governanceTemplates.map(({ key, permissions, scope_kinds }) => ({
      key,
      permissions,
      scope_kinds,
    })),
  ).toEqual([
    {
      key: "workforce.account_link_reviewer.v1",
      scope_kinds: ["hospital", "pharmacy", "supplier"],
      permissions: ["workforce.login_links.review", "workforce.independent_login.review"],
    },
    {
      key: "iam.governance_auditor.v1",
      scope_kinds: ["tenant"],
      permissions: ["iam.governance.read", "iam.governance.review"],
    },
  ]);
});
it("preserves microsecond interval ordering instead of comparing rounded milliseconds", () => {
  expect(
    GovernanceGrantInputContract.parse({
      ...input,
      valid_from: "2026-11-13T00:00:00.000001Z",
      expires_at: "2026-11-13T00:00:00.000002Z",
    }),
  ).toMatchObject({ expires_at: "2026-11-13T00:00:00.000002Z" });
});
it.each([
  { valid_from: "2026-11-13T00:00:00.0000001Z" },
  { valid_from: "0001-01-01T00:00:00Z" },
  { valid_from: "0000-01-01T00:00:00Z" },
  { expires_at: "9999-12-31T23:59:59-01:00" },
])("refuses instants outside canonical precision or bounds %j", (change) => {
  expect(GovernanceGrantInputContract.safeParse({ ...input, ...change }).success).toBe(false);
});

it.each([" ", "\ufeff", "\t", "review\nchanged"])(
  "refuses blank or control decision reasons %j",
  (reason) => {
    expect(
      GovernanceGrantDecisionInputContract.safeParse({
        expected_version: 1,
        decision: "approve",
        reason,
      }).success,
    ).toBe(false);
  },
);
it("uses UTF16 limits consistently for grant and decision reasons", () => {
  for (const reason of ["🔎".repeat(250), "🔎".repeat(250) + "x"]) {
    const expected = reason.length <= 500;
    expect(GovernanceGrantInputContract.safeParse({ ...input, reason }).success).toBe(expected);
    expect(
      GovernanceGrantDecisionInputContract.safeParse({
        expected_version: 1,
        decision: "approve",
        reason,
      }).success,
    ).toBe(expected);
  }
});

it.each(["\ufeff", "\u0085", "\u0001"])(
  "rejects invisible or control subject characters %j",
  (character) => {
    expect(
      GovernanceGrantInputContract.safeParse({ ...input, subject: `reviewer${character}suffix` })
        .success,
    ).toBe(false);
  },
);

it.each([
  "2026-11-13T0:00:00Z",
  "2026-11-13T00:00:00,1Z",
  "2026-11-13T00:00:00+24:00",
  "2026-11-13T00:00:00+00:60",
  "2026-11-13T00:00:00.0000000001Z",
])("refuses malformed or silently truncated instants %j", (valid_from) => {
  expect(GovernanceGrantInputContract.safeParse({ ...input, valid_from }).success).toBe(false);
});
it.each([
  "2026-11-13T00:00:00.000001Z",
  "2026-11-13T00:00:00.0000010000Z",
  "2026-11-13T00:00:00+05:30",
])("accepts exact microseconds and Indian timezone offsets %j", (valid_from) => {
  expect(GovernanceGrantInputContract.safeParse({ ...input, valid_from }).success).toBe(true);
});

it("leaves a safe integer successor for a governance decision", () => {
  expect(
    GovernanceGrantDecisionInputContract.safeParse({
      expected_version: 9007199254740991,
      decision: "approve",
      reason: "Independent review",
    }).success,
  ).toBe(false);
});
