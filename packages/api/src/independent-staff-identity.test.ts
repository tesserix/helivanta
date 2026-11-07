import { afterEach, expect, it, vi } from "vitest";
import {
  listIndependentStaffLinksPage,
  proposeIndependentStaffLink,
  recoverIndependentStaffProposal,
  decideIndependentStaffLink,
  recoverIndependentStaffCommand,
} from "./independent-staff-identity";

const owner = "11111111-1111-4111-8111-111111111111";
const employer = "019a55c0-1111-7111-8111-111111111111";
const staff = "019a55c0-1111-7111-8111-111111111112";
const link = "019a55c0-1111-7111-8111-111111111113";
const command = "019a55c0-1111-7111-8111-111111111114";
const scope = {
  tenantId: owner,
  subject: "TEST-maker",
  employerKind: "supplier" as const,
  employerId: employer,
  staffId: staff,
};
const wireScope = {
  owner_tenant_id: owner,
  employer_kind: "supplier",
  employer_id: employer,
  staff_id: staff,
};
const input = {
  expected_staff_version: 1,
  subject: "TEST-operator",
  reason: "TEST checked identity",
};
const proposal = {
  id: link,
  scope: wireScope,
  input,
  prepared_by: scope.subject,
  created_at: "2026-11-07T00:00:00Z",
};
const decision = {
  expected_version: 1,
  decision: "approve" as const,
  reason: "TEST independently reviewed",
};
const reviewer = { ...scope, subject: "TEST-reviewer" };
const receipt = {
  id: command,
  link_id: link,
  scope: wireScope,
  input: decision,
  actor_subject: reviewer.subject,
  created_at: "2026-11-07T00:01:00Z",
  result: {
    scope: wireScope,
    staff_version: 1,
    link: {
      id: link,
      staff_id: staff,
      subject: input.subject,
      status: "active",
      version: 2,
      proposed_by_subject: scope.subject,
      proposal_reason: input.reason,
      actor_subject: reviewer.subject,
      reason: decision.reason,
      created_at: proposal.created_at,
      updated_at: "2026-11-07T00:01:00Z",
    },
  },
};
afterEach(() => vi.unstubAllGlobals());
it("recovers a supplier account proposal through Global API using GET only", async () => {
  const request = vi.fn<typeof fetch>().mockImplementation(async () => Response.json(proposal));
  vi.stubGlobal("fetch", request);
  expect(await proposeIndependentStaffLink(scope, link, input)).toEqual(proposal);
  expect(await recoverIndependentStaffProposal(scope, link, input)).toEqual(proposal);
  expect(request.mock.calls.map(([url, init]) => [url, init?.method ?? "GET"])).toEqual([
    [`/api/v1/workforce/suppliers/${employer}/staff/${staff}/account-links/${link}`, "PUT"],
    [
      `/api/v1/workforce/suppliers/${employer}/staff/${staff}/account-links/${link}/proposal-receipt`,
      "GET",
    ],
  ]);
  const headers = new Headers(request.mock.calls[0]![1]?.headers);
  expect(headers.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(headers.get("X-Helivanta-Expected-Tenant")).toBe(owner);
});
it("recovers the original decision receipt without another identity command", async () => {
  const request = vi.fn<typeof fetch>().mockImplementation(async () => Response.json(receipt));
  vi.stubGlobal("fetch", request);
  expect(await decideIndependentStaffLink(reviewer, link, command, decision)).toEqual(receipt);
  expect(await recoverIndependentStaffCommand(reviewer, link, command, decision)).toEqual(receipt);
  expect(request.mock.calls.map(([, init]) => init?.method ?? "GET")).toEqual(["PUT", "GET"]);
});
it.each([
  { scope: { ...wireScope, employer_kind: "pharmacy" } },
  { scope: { ...wireScope, owner_tenant_id: "22222222-2222-4222-8222-222222222222" } },
  { input: { ...input, expected_staff_version: 2 } },
  { prepared_by: "TEST-other" },
  { id: command },
])("rejects a proposal that differs from the exact requested intent %j", async (change) => {
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>().mockImplementation(async () => Response.json({ ...proposal, ...change })),
  );
  await expect(recoverIndependentStaffProposal(scope, link, input)).rejects.toThrow();
});
it.each([
  { actor_subject: "TEST-other" },
  { result: { ...receipt.result, link: { ...receipt.result.link, version: 3 } } },
  { result: { ...receipt.result, scope: { ...wireScope, employer_kind: "pharmacy" } } },
  { result: { ...receipt.result, link: { ...receipt.result.link, status: "pending" } } },
  { result: { ...receipt.result, link: { ...receipt.result.link, actor_subject: "TEST-other" } } },
  { created_at: "2026-11-07T00:02:00Z" },
])("rejects inconsistent command evidence %j", async (change) => {
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>().mockImplementation(async () => Response.json({ ...receipt, ...change })),
  );
  await expect(recoverIndependentStaffCommand(reviewer, link, command, decision)).rejects.toThrow();
});

it("loads a bounded Staff account-link page and rejects another source", async () => {
  const request = vi
    .fn<typeof fetch>()
    .mockImplementation(async () =>
      Response.json({ data: [receipt.result], page: { has_more: false, next_cursor: null } }),
    );
  vi.stubGlobal("fetch", request);
  expect((await listIndependentStaffLinksPage(scope, null)).data).toEqual([receipt.result]);
  expect(request.mock.calls[0]![0]).toBe(
    `/api/v1/workforce/suppliers/${employer}/staff/${staff}/account-links?limit=25`,
  );
  request.mockImplementation(async () =>
    Response.json({
      data: [{ ...receipt.result, scope: { ...wireScope, employer_kind: "pharmacy" } }],
      page: { has_more: false, next_cursor: null },
    }),
  );
  await expect(listIndependentStaffLinksPage(scope, null)).rejects.toThrow();
});
it("rejects duplicate or inconsistent account-link pages", async () => {
  const request = vi.fn<typeof fetch>().mockImplementation(async () =>
    Response.json({
      data: [receipt.result, receipt.result],
      page: { has_more: false, next_cursor: null },
    }),
  );
  vi.stubGlobal("fetch", request);
  await expect(listIndependentStaffLinksPage(scope, null)).rejects.toThrow();
  request.mockImplementation(async () =>
    Response.json({ data: [], page: { has_more: true, next_cursor: "next" } }),
  );
  await expect(listIndependentStaffLinksPage(scope, null)).rejects.toThrow();
});

it("links a pre-existing UUIDv4 Staff record without changing its owner", async () => {
  const existing = "12345678-1234-4234-8234-123456789012";
  vi.stubGlobal(
    "fetch",
    vi
      .fn<typeof fetch>()
      .mockImplementation(async () =>
        Response.json({ ...proposal, scope: { ...wireScope, staff_id: existing } }),
      ),
  );
  expect(
    (await proposeIndependentStaffLink({ ...scope, staffId: existing }, link, input)).scope
      .staff_id,
  ).toBe(existing);
});
