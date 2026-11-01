import { afterEach, expect, it, vi } from "vitest";
import { searchPersonReviewEvidence } from "./person-review-selection";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-manager",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
};
const source = { staff_version: 3, employment_epoch: 1, identity_hash: "a".repeat(64) };
const row = {
  id: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa",
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  record_version: 3,
  filename: "synthetic-identity.png",
  content_type: "image/png",
  size: 88,
  created_at: "2026-10-31T00:00:00Z",
};
const page = (file: unknown) => ({ data: [file], page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
it("pins original workspace/source and searches through the Global API without generic document authority", async () => {
  const fetcher = vi.fn<typeof fetch>(async () => Response.json(page(row)));
  vi.stubGlobal("fetch", fetcher);
  expect(await searchPersonReviewEvidence(scope, source)).toEqual(page(row));
  const [url, init] = fetcher.mock.calls[0];
  expect(url).toBe(
    `/api/v1/workforce/hospitals/${scope.hospitalId}/identity-staff/${scope.staffId}/person-review-evidence/search?limit=50`,
  );
  expect(init?.method).toBe("POST");
  expect(init?.cache).toBe("no-store");
  expect(JSON.parse(String(init?.body))).toEqual(source);
  expect(new Headers(init?.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(init?.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});
it.each([
  { ...row, record_version: 2 },
  { ...row, staff_id: "44444444-4444-4444-8444-444444444444" },
  { ...row, hospital_id: "44444444-4444-4444-8444-444444444444" },
  { ...row, storage_key: "private-location" },
])("rejects another source/scope or unrequested private fields", async (file) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(page(file))),
  );
  await expect(searchPersonReviewEvidence(scope, source)).rejects.toThrow(/could not be verified/i);
});
it("does not release a response after cancellation", async () => {
  const controller = new AbortController();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      controller.abort();
      return Response.json(page(row));
    }),
  );
  await expect(
    searchPersonReviewEvidence(scope, source, null, controller.signal),
  ).rejects.toThrow();
});
