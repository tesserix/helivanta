import { afterEach, expect, it, vi } from "vitest";
import {
  listStaffAssignments,
  decideStaffAssignment,
  proposeStaffAssignment,
} from "./staff-assignments";

const tenantId = "11111111-1111-4111-8111-111111111111";
const id = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const input = {
  hospital_id: "22222222-2222-4222-8222-222222222222",
  subject: "synthetic-member",
  template_key: "workforce.leave_reviewer.v1" as const,
  reason: "Synthetic access review",
  valid_from: new Date().toISOString(),
  expires_at: new Date(Date.now() + 86400000).toISOString(),
};
const proposal = {
  id,
  input,
  proposed_by: "synthetic-admin",
  status: "pending_review",
  version: 2,
  created_at: input.valid_from,
  updated_at: input.valid_from,
};
afterEach(() => vi.unstubAllGlobals());

it("keeps activation assignments in their own bounded list", async () => {
  const activation = {
    ...proposal,
    input: { ...input, template_key: "workforce.employment_activator.v1" },
  };
  const fetch = vi
    .fn()
    .mockResolvedValue(
      Response.json({ data: [activation], page: { has_more: false, next_cursor: null } }),
    );
  vi.stubGlobal("fetch", fetch);
  const result = await listStaffAssignments(
    { tenantId, subject: "reviewer" },
    { family: "activation" },
  );
  expect(result.data).toEqual([activation]);
  expect(fetch.mock.calls[0]![0]).toContain(
    "/api/v1/iam/employment-activation-assignment-proposals?",
  );
  expect(fetch.mock.calls[0]![1].cache).toBe("no-store");
});

it("binds paged assignment reads to the expected tenant without caching private data", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValue(
      Response.json({ data: [proposal], page: { has_more: false, next_cursor: null } }),
    );
  vi.stubGlobal("fetch", fetch);
  expect(
    (await listStaffAssignments({ tenantId, subject: "reviewer" }, { cursor: "opaque+/=" })).data,
  ).toEqual([proposal]);
  const [url, options] = fetch.mock.calls[0]!;
  expect(url).toContain("/api/v1/iam/assignment-proposals?");
  expect(new URL(url, "https://example.invalid").searchParams.get("cursor")).toBe("opaque+/=");
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenantId);
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Subject")).toBe("reviewer");
  expect(options.cache).toBe("no-store");
});

it("rejects an invalid assignment response instead of displaying authority", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      Response.json({
        data: [{ ...proposal, status: "unknown" }],
        page: { has_more: false, next_cursor: null },
      }),
    ),
  );
  await expect(listStaffAssignments({ tenantId, subject: "reviewer" })).rejects.toThrow();
});

it("keeps decisions bound to the viewed version and checks returned identity", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValue(Response.json({ ...proposal, id: "33333333-3333-4333-8333-333333333333" }));
  vi.stubGlobal("fetch", fetch);
  await expect(
    decideStaffAssignment({ tenantId, subject: "reviewer" }, id, {
      expected_version: 2,
      decision: "approve",
      reason: "Independent review",
    }),
  ).rejects.toThrow("did not match");
  expect(JSON.parse(fetch.mock.calls[0]![1].body)).toEqual({
    expected_version: 2,
    decision: "approve",
    reason: "Independent review",
  });
});

it("rejects unbounded validity before sending a proposal", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(
    proposeStaffAssignment({ tenantId, subject: "admin" }, id, {
      ...input,
      expires_at: "2029-01-01T00:00:00Z",
    }),
  ).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});

it("keeps identity assignments separate from the existing leave list", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValue(Response.json({ data: [], page: { has_more: false, next_cursor: null } }));
  vi.stubGlobal("fetch", fetch);
  await listStaffAssignments({ tenantId, subject: "reviewer" }, { family: "identity" });
  expect(fetch.mock.calls[0]![0]).toContain("/api/v1/iam/identity-assignment-proposals?");
});

it("uses the explicit intake list without mixing existing leave or identity assignments", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValue(Response.json({ data: [], page: { has_more: false, next_cursor: null } }));
  vi.stubGlobal("fetch", fetch);
  await listStaffAssignments({ tenantId, subject: "reviewer" }, { family: "intake" });
  expect(fetch.mock.calls[0]![0]).toContain("/api/v1/iam/intake-assignment-proposals?");
});
