import { afterEach, expect, it, vi } from "vitest";
import { listOperatingAuthorityScopes } from "./operating-authority-scopes";
const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-operator" };
const employer = {
  employer_kind: "pharmacy",
  employer_id: "019a55c0-1111-7111-8111-111111111112",
  name: "Synthetic pharmacy",
  can_manage: true,
  can_review: true,
};
const page = { data: [employer], page: { has_more: false, next_cursor: null } };
afterEach(() => vi.unstubAllGlobals());
it("discovers authorized employer names through the scoped Global API request", async () => {
  const request = vi.fn<typeof fetch>().mockResolvedValue(Response.json(page));
  vi.stubGlobal("fetch", request);
  expect((await listOperatingAuthorityScopes(scope)).data).toEqual([employer]);
  const [url, options] = request.mock.calls[0]!;
  expect(url).toBe("/api/v1/workforce/operating-authority-scopes?limit=20");
  expect(new Headers(options?.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(options?.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});
it.each([
  { ...page, data: [{ ...employer, employer_kind: "hospital" }] },
  { ...page, data: [{ ...employer, can_manage: false, can_review: false }] },
  { ...page, data: [employer, employer] },
  { ...page, page: { has_more: true, next_cursor: null } },
  { ...page, page: { has_more: false, next_cursor: "unexpected" } },
  { ...page, data: [{ ...employer, source_grant_id: employer.employer_id }] },
])("rejects unverifiable employer choices %j", async (value) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(value)));
  await expect(listOperatingAuthorityScopes(scope)).rejects.toThrow();
});
it("stops an already aborted scope discovery before transport", async () => {
  const request = vi.fn<typeof fetch>();
  vi.stubGlobal("fetch", request);
  const controller = new AbortController();
  controller.abort();
  await expect(listOperatingAuthorityScopes(scope, null, controller.signal)).rejects.toThrow();
  expect(request).not.toHaveBeenCalled();
});
