import { afterEach, expect, it, vi } from "vitest";
import { ApiError } from "./client";
import {
  listGovernanceAssignmentScopes,
  listGovernanceAssignmentTemplates,
  listGovernanceAssignments,
  getGovernanceAssignment,
  getGovernanceAssignmentAccount,
  isGovernanceAssignmentAccessChanged,
  proposeGovernanceAssignment,
  decideGovernanceAssignment,
} from "./governance-assignments";

import { staffAccessTemplates } from "./staff-access-templates";

const tenantId = "11111111-1111-4111-8111-111111111111";
const hospitalId = "22222222-2222-4222-8222-222222222222";
const id = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const scope = { tenantId, subject: "synthetic-reviewer", hospitalId };
const input = {
  hospital_id: hospitalId,
  subject: "synthetic-recipient",
  template_key: "workforce.leave_reviewer.v1" as const,
  reason: "Synthetic independent duty review",
  valid_from: new Date().toISOString(),
  expires_at: new Date(Date.now() + 86400000).toISOString(),
};
const proposal = {
  id,
  input,
  scope_kind: "hospital",
  proposed_by: "synthetic-owner",
  status: "pending_review",
  version: 2,
  created_at: input.valid_from,
  updated_at: input.valid_from,
};
const page = { data: [proposal], page: { has_more: false, next_cursor: null } };
afterEach(() => vi.unstubAllGlobals());

it("discovers named authorized hospitals through the Global API with account preconditions", async () => {
  const choice = {
    hospital_id: hospitalId,
    name: "Synthetic hospital",
    can_manage: true,
    can_review: true,
  };
  const fetch = vi.fn().mockResolvedValue(Response.json({ data: [choice], page: page.page }));
  vi.stubGlobal("fetch", fetch);
  const result = await listGovernanceAssignmentScopes(scope, "opaque+/=");
  expect(result.data).toEqual([choice]);
  const [url, options] = fetch.mock.calls[0]!;
  const parsed = new URL(url, "https://example.invalid");
  expect(parsed.pathname).toBe("/api/v1/workforce/assignment-authority-scopes");
  expect(parsed.searchParams.get("cursor")).toBe("opaque+/=");
  expect(parsed.searchParams.get("limit")).toBe("20");
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenantId);
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(options.cache).toBe("no-store");
});

it("uses the hospital-native route and binds family and cursor without legacy fallback", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json(page));
  vi.stubGlobal("fetch", fetch);
  expect(
    (await listGovernanceAssignments(scope, { family: "leave", cursor: "opaque+/=" })).data,
  ).toEqual([proposal]);
  const parsed = new URL(fetch.mock.calls[0]![0], "https://example.invalid");
  expect(parsed.pathname).toBe(`/api/v1/iam/hospitals/${hospitalId}/assignment-proposals`);
  expect(parsed.searchParams.get("family")).toBe("leave");
  expect(parsed.searchParams.get("cursor")).toBe("opaque+/=");
});

it("retains the proposal ID and expected decision version on uncertain writes", async () => {
  const fetch = vi.fn().mockImplementation(async () => Response.json(proposal));
  vi.stubGlobal("fetch", fetch);
  await proposeGovernanceAssignment(scope, id, input);
  await decideGovernanceAssignment(scope, id, {
    expected_version: 2,
    decision: "approve",
    reason: "Independent sources examined",
  });
  expect(fetch.mock.calls.map(([url]) => new URL(url, "https://example.invalid").pathname)).toEqual(
    [
      `/api/v1/iam/hospitals/${hospitalId}/assignment-proposals/${id}`,
      `/api/v1/iam/hospitals/${hospitalId}/assignment-proposals/${id}/decision`,
    ],
  );
  expect(fetch.mock.calls[0]![1].method).toBe("PUT");
  expect(JSON.parse(fetch.mock.calls[1]![1].body)).toMatchObject({ expected_version: 2 });
});

it("rejects a proposed hospital different from the selected scope before sending", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(
    proposeGovernanceAssignment(scope, id, { ...input, hospital_id: tenantId }),
  ).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});

it("rejects a response that belongs to another hospital", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        Response.json({ ...proposal, input: { ...input, hospital_id: tenantId } }),
      ),
  );
  await expect(getGovernanceAssignment(scope, id)).rejects.toThrow();
});

it.each([
  {
    data: [
      { hospital_id: hospitalId, name: "Synthetic hospital", can_manage: false, can_review: false },
    ],
    page: page.page,
  },
  { data: [], page: { has_more: true, next_cursor: null } },
  { data: [], page: { has_more: false, next_cursor: "contradictory" } },
])("refuses unverified discovery and contradictory pagination", async (answer) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(answer)));
  await expect(listGovernanceAssignmentScopes(scope)).rejects.toThrow();
});

it("propagates denial without a hidden retry on owner endpoints", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValue(
      Response.json(
        { error: { code: "forbidden", message: "Current grant required" } },
        { status: 403 },
      ),
    );
  vi.stubGlobal("fetch", fetch);
  await expect(listGovernanceAssignments(scope)).rejects.toMatchObject({ status: 403 });
  expect(fetch).toHaveBeenCalledTimes(1);
});

it("loads the exact canonical assignment catalog under the selected hospital", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json(staffAccessTemplates));
  vi.stubGlobal("fetch", fetch);
  expect(await listGovernanceAssignmentTemplates(scope)).toEqual(staffAccessTemplates);
  expect(new URL(fetch.mock.calls[0]![0], "https://example.invalid").pathname).toBe(
    `/api/v1/iam/hospitals/${hospitalId}/assignment-templates`,
  );
  expect(new Headers(fetch.mock.calls[0]![1].headers).get("X-Helivanta-Expected-Subject")).toBe(
    scope.subject,
  );
});
it.each([
  staffAccessTemplates.slice(1),
  [...staffAccessTemplates, staffAccessTemplates[0]],
  staffAccessTemplates.map((row, index) =>
    index === 0 ? { ...row, permissions: ["workforce.documents.manage"] } : row,
  ),
])("refuses missing, duplicated or changed template definitions", async (catalog) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(catalog)));
  await expect(listGovernanceAssignmentTemplates(scope)).rejects.toThrow();
});

const account = {
  proposal_id: id,
  proposal_version: 2,
  subject: input.subject,
  available: true,
  name: "Synthetic Staff",
  email: "synthetic@example.invalid",
};
it("reads account presentation only through the selected hospital proposal", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json(account));
  vi.stubGlobal("fetch", fetch);
  expect(await getGovernanceAssignmentAccount(scope, proposal)).toEqual(account);
  expect(new URL(fetch.mock.calls[0]![0], "https://example.invalid").pathname).toBe(
    `/api/v1/iam/hospitals/${hospitalId}/assignment-proposals/${id}/account`,
  );
  expect(new Headers(fetch.mock.calls[0]![1].headers).get("X-Helivanta-Expected-Subject")).toBe(
    scope.subject,
  );
});
it.each([
  { ...account, proposal_id: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaab" },
  { ...account, proposal_version: 3 },
  { ...account, subject: "another-user" },
  { ...account, available: false },
  { ...account, name: "" },
  { ...account, email: "not-an-email" },
])("withholds mismatched, incomplete or contradictory account presentation", async (answer) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(answer)));
  await expect(getGovernanceAssignmentAccount(scope, proposal)).rejects.toThrow();
});
it("retains explicit known-unavailable presentation without fabricating a name", async () => {
  const answer = { proposal_id: id, proposal_version: 2, subject: input.subject, available: false };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(answer)));
  expect(await getGovernanceAssignmentAccount(scope, proposal)).toEqual(answer);
});

it.each([
  "tenant_changed",
  "assignment_session_context_required",
  "session_changed",
  "workspace_changed",
])("treats %s as a scope cutoff", (code) => {
  expect(
    isGovernanceAssignmentAccessChanged(new ApiError(code, "Synthetic scope changed", 409)),
  ).toBe(true);
});
it("keeps a stale proposal conflict separate from an account cutoff", () => {
  expect(
    isGovernanceAssignmentAccessChanged(new ApiError("conflict", "Synthetic stale proposal", 409)),
  ).toBe(false);
});
