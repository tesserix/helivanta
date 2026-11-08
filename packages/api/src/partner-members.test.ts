import { afterEach, expect, it, vi } from "vitest";
import {
  preparePartnerMember,
  decidePartnerMember,
  recoverPartnerMemberCommand,
} from "./partner-members";
const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "preparer" };
const id = "019ad451-1234-7123-8123-123456789012";
const group = "019ad451-1234-7123-8123-123456789013";
const command = "019ad451-1234-7123-8123-123456789014";
const hospital = "22222222-2222-4222-8222-222222222222";
const input = {
  kind: "subject" as const,
  subject: "member",
  staff_id: hospital,
  posting_id: id,
  posting_revision_id: id,
  posting_version: 3,
  department_id: id,
  department_revision_id: id,
  expected_group_version: 3,
  valid_from: "2026-11-06T09:00:00Z",
  expires_at: "2026-11-06T10:00:00Z",
  reason: "Reviewed receiving membership",
  sources_examined: true as const,
};
const member = {
  id,
  group_id: group,
  hospital_id: hospital,
  input,
  member_staff_id: hospital,
  member_display_name: "Synthetic Member",
  member_link_id: id,
  member_link_version: 1,
  member_generation_id: id,
  member_employment_epoch: 1,
  member_membership_epoch: 1,
  prepared_source_use_id: id,
  source_checked_at: input.valid_from,
  source_until: input.expires_at,
  prepared_by: scope.subject,
  prepared_by_staff_id: group,
  prepared_by_link_id: group,
  prepared_by_generation_id: group,
  status: "pending_review",
  version: 1,
  created_at: input.valid_from,
  updated_at: input.valid_from,
};
const decision = {
  operation: "approve" as const,
  expected_version: 1,
  reason: "Independent current Staff review",
  sources_examined: true,
  purpose_eligibility_confirmed: true,
};
const receipt = {
  id: command,
  member_id: id,
  input: decision,
  result_version: 2,
  actor_subject: "reviewer",
  actor_staff_id: command,
  actor_link_id: command,
  actor_generation_id: command,
  reviewed_source_use_id: command,
  member: { ...member, status: "approved", version: 2 },
  cohort_epoch: 1,
  created_at: input.valid_from,
};
afterEach(() => vi.unstubAllGlobals());
it("binds preparation and recovery to exact hospital, group, member and actor", async () => {
  const fetch = vi.fn(async () => Response.json(member));
  vi.stubGlobal("fetch", fetch);
  expect(await preparePartnerMember(scope, hospital, group, id, input)).toEqual(member);
  const [path, init] = fetch.mock.calls[0] as unknown as [string, RequestInit];
  expect(path).toBe(
    `/api/v1/iam/hospitals/${hospital}/partner-groups/${group}/member-proposals/${id}`,
  );
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...member, group_id: id })),
  );
  await expect(preparePartnerMember(scope, hospital, group, id, input)).rejects.toThrow();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...member, input: { ...input, subject: "other" } })),
  );
  await expect(preparePartnerMember(scope, hospital, group, id, input)).rejects.toThrow();
});
it("accepts only the retained exact review intent and consistent result", async () => {
  const reviewer = { ...scope, subject: "reviewer" };
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(receipt)),
  );
  expect(await decidePartnerMember(reviewer, hospital, group, id, command, decision)).toEqual(
    receipt,
  );
  expect(
    await recoverPartnerMemberCommand(reviewer, hospital, group, id, command, decision),
  ).toEqual(receipt);
  for (const response of [
    { ...receipt, actor_subject: "other" },
    { ...receipt, input: { ...decision, reason: "Changed intent" } },
    { ...receipt, member: { ...receipt.member, status: "rejected" } },
    { ...receipt, member: { ...receipt.member, version: 3 } },
  ]) {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json(response)),
    );
    await expect(
      recoverPartnerMemberCommand(reviewer, hospital, group, id, command, decision),
    ).rejects.toThrow();
  }
});

it("pages scoped hospital groups, candidate postings and retained memberships with no foreign rows", async () => {
  const { listHospitalPartnerGroups, listPartnerMembers, listPartnerMemberCandidates } =
    await import("./partner-members");
  const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(page([member]))),
  );
  expect((await listPartnerMembers(scope, hospital, group)).data).toEqual([member]);
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(page([{ ...member, hospital_id: scope.tenantId }]))),
  );
  await expect(listPartnerMembers(scope, hospital, group)).rejects.toThrow();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(page([]))),
  );
  expect((await listHospitalPartnerGroups(scope, hospital)).data).toEqual([]);
  expect((await listPartnerMemberCandidates(scope, hospital)).data).toEqual([]);
});

it("uses hospital-scoped capability hints without accepting another hospital or missing flags", async () => {
  const { getPartnerMemberCapabilities } = await import("./partner-members");
  const capability = { hospital_id: hospital, can_prepare: false, can_review: true };
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(capability)),
  );
  expect(await getPartnerMemberCapabilities(scope, hospital)).toEqual(capability);
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...capability, hospital_id: scope.tenantId })),
  );
  await expect(getPartnerMemberCapabilities(scope, hospital)).rejects.toThrow();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ hospital_id: hospital, can_review: true })),
  );
  await expect(getPartnerMemberCapabilities(scope, hospital)).rejects.toThrow();
});
