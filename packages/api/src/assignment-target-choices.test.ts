import { afterEach, expect, it, vi } from "vitest";
import { ApiError } from "./client";
import {
  assignmentTargetChoicesKey,
  listAssignmentTargetChoices,
} from "./assignment-target-choices";

const tenantId = "11111111-1111-4111-8111-111111111111";
const hospitalId = "22222222-2222-4222-8222-222222222222";
const staffId = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const scope = { tenantId, subject: "synthetic-reviewer", hospitalId };
const target = {
  hospital_id: hospitalId,
  staff_id: staffId,
  subject: "synthetic-recipient",
  name: "Synthetic reviewed recipient",
  email: "synthetic-recipient@example.invalid",
};
const page = { data: [target], page: { has_more: false, next_cursor: null } };
afterEach(() => vi.unstubAllGlobals());

it("reads qualified targets through the Global API with exact account preconditions", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json(page));
  vi.stubGlobal("fetch", fetch);
  expect((await listAssignmentTargetChoices(scope, "sealed+/=")).data).toEqual([target]);
  const [url, options] = fetch.mock.calls[0]!;
  const parsed = new URL(url, "https://example.invalid");
  expect(parsed.pathname).toBe(`/api/v1/iam/hospitals/${hospitalId}/assignment-target-choices`);
  expect(parsed.searchParams.get("limit")).toBe("20");
  expect(parsed.searchParams.get("status")).toBe("eligible");
  expect(parsed.searchParams.get("cursor")).toBe("sealed+/=");
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenantId);
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(options.cache).toBe("no-store");
});

it("retains the sealed continuation when all scanned Staff candidates are ineligible", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        Response.json({ data: [], page: { has_more: true, next_cursor: "sealed-after-scan" } }),
      ),
  );
  const result = await listAssignmentTargetChoices(scope);
  expect(result.data).toEqual([]);
  expect(result.page.next_cursor).toBe("sealed-after-scan");
});

it.each([
  { hospital_id: tenantId },
  { hospital_id: "00000000-0000-0000-0000-000000000000" },
  { staff_id: "00000000-0000-0000-0000-000000000000" },
  { subject: "synthetic:*" },
  { name: "" },
  { name: "Synthetic\u0000private" },
  { email: "not-an-email" },
  { generation_id: staffId },
])("rejects an unqualified, foreign or private target payload: %j", async (change) => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(Response.json({ ...page, data: [{ ...target, ...change }] })),
  );
  await expect(listAssignmentTargetChoices(scope)).rejects.toThrow();
});

it.each([
  { ...target },
  { ...target, staff_id: tenantId },
  { ...target, subject: "another-recipient" },
])("rejects repeated Staff or account choices: %j", async (other) => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(Response.json({ ...page, data: [target, other] })),
  );
  await expect(listAssignmentTargetChoices(scope)).rejects.toThrow();
});

it.each([
  { has_more: true, next_cursor: null },
  { has_more: false, next_cursor: "unexpected-continuation" },
])("rejects an inconsistent continuation: %j", async (pagination) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ ...page, page: pagination })));
  await expect(listAssignmentTargetChoices(scope)).rejects.toThrow();
});

it("rejects invalid scope and cursor before sending a request", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  for (const bad of [
    { ...scope, hospitalId: "not-a-hospital" },
    { ...scope, tenantId: "00000000-0000-0000-0000-000000000000" },
    { ...scope, subject: "synthetic:*" },
  ])
    await expect(listAssignmentTargetChoices(bad)).rejects.toThrow();
  await expect(listAssignmentTargetChoices(scope, "")).rejects.toThrow();
  await expect(listAssignmentTargetChoices(scope, "x".repeat(2049))).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});

it("does not fall back to a legacy directory when current authority is denied", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValue(
      Response.json(
        { error: { code: "forbidden", message: "Source no longer current" } },
        { status: 403 },
      ),
    );
  vi.stubGlobal("fetch", fetch);
  await expect(listAssignmentTargetChoices(scope)).rejects.toBeInstanceOf(ApiError);
  expect(fetch).toHaveBeenCalledTimes(1);
});

it("does not return private identity details after cancellation", async () => {
  const controller = new AbortController();
  const fetch = vi.fn().mockImplementation(async () => {
    controller.abort();
    return Response.json(page);
  });
  vi.stubGlobal("fetch", fetch);
  await expect(listAssignmentTargetChoices(scope, null, controller.signal)).rejects.toThrow();
  expect(fetch).toHaveBeenCalledTimes(1);
  await expect(listAssignmentTargetChoices(scope, null, controller.signal)).rejects.toThrow();
  expect(fetch).toHaveBeenCalledTimes(1);
});

it("separates cached targets by tenant, actor and hospital", () => {
  const base = assignmentTargetChoicesKey(scope);
  expect(base).not.toEqual(assignmentTargetChoicesKey({ ...scope, tenantId: staffId }));
  expect(base).not.toEqual(assignmentTargetChoicesKey({ ...scope, subject: "other-reviewer" }));
  expect(base).not.toEqual(assignmentTargetChoicesKey({ ...scope, hospitalId: staffId }));
});
