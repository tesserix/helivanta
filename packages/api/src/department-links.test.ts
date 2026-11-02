import { afterEach, expect, it, vi } from "vitest";
import { listDepartmentLinks, recordDepartmentLink } from "./department-links";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-manager" };
const hospital = "22222222-2222-4222-8222-222222222222";
const master = "33333333-3333-4333-8333-333333333333";
const revision = "44444444-4444-4444-8444-444444444444";
const request = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const node = "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb";
const nodeRevision = "019ccccc-cccc-7ccc-8ccc-cccccccccccc";
const receipt = {
  request_id: request,
  hospital_id: hospital,
  master_id: master,
  master_revision_id: revision,
  department_id: node,
  department_revision_id: nodeRevision,
  version: 1,
  status: "pending",
  created_at: "2026-11-02T00:00:00Z",
};
const body = {
  expected_version: 0,
  master_revision_id: revision,
  department_id: node,
  department_revision_id: nodeRevision,
  reason: "Synthetic explicit handoff",
};
afterEach(() => vi.unstubAllGlobals());
it("records only the exact owner receipt through Global API with no-store and account assertions", async () => {
  const fetcher = vi.fn().mockResolvedValue(Response.json(receipt));
  vi.stubGlobal("fetch", fetcher);
  expect(await recordDepartmentLink(scope, hospital, master, request, body)).toEqual(receipt);
  expect(fetcher.mock.calls[0]?.[0]).toBe(
    `/api/v1/workforce/hospitals/${hospital}/hr/masters/${master}/department-links/${request}`,
  );
  expect(fetcher.mock.calls[0]?.[1]).toMatchObject({
    cache: "no-store",
    method: "PUT",
    body: JSON.stringify(body),
  });
  expect(new Headers(fetcher.mock.calls[0]?.[1].headers).get("X-Helivanta-Expected-Subject")).toBe(
    scope.subject,
  );
});
it.each([
  ["foreign source", { ...receipt, hospital_id: scope.tenantId }],
  ["wrong master", { ...receipt, master_id: revision }],
  ["wrong command", { ...receipt, request_id: node }],
  ["wrong source revision", { ...receipt, master_revision_id: master }],
  ["wrong department", { ...receipt, department_id: nodeRevision }],
  ["wrong version", { ...receipt, version: 2 }],
  ["private reason", { ...receipt, reason: "protected" }],
  ["unobserved match", { ...receipt, status: "matched" }],
  ["pending with observation", { ...receipt, observed_at: "2026-11-02T00:00:00Z" }],
])("rejects %s", async (_label, out) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(out)));
  await expect(recordDepartmentLink(scope, hospital, master, request, body)).rejects.toThrow();
});
it("validates bounded history and rejects duplicate commands and invalid continuation", async () => {
  const page = { data: [receipt], page: { has_more: false, next_cursor: null } };
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(Response.json(page))
    .mockResolvedValueOnce(Response.json({ ...page, data: [receipt, receipt] }))
    .mockResolvedValueOnce(Response.json({ ...page, page: { has_more: true, next_cursor: null } }));
  vi.stubGlobal("fetch", fetcher);
  expect(await listDepartmentLinks(scope, hospital, master)).toEqual(page);
  await expect(listDepartmentLinks(scope, hospital, master)).rejects.toThrow();
  await expect(listDepartmentLinks(scope, hospital, master)).rejects.toThrow();
});
