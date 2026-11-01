import { afterEach, expect, it, vi } from "vitest";
import { listEmploymentPersonFindings } from "./employment-finding-selection";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-preparer",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
  hrIntake: true as const,
};
const basis = { staff_version: 3, employment_epoch: 2 };
const finding = {
  id: "019a0000-0000-7000-8000-000000000001",
  revision_id: "019a0000-0000-7000-8000-000000000002",
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  version: 2,
  employment_epoch: 2,
  identity_hash: "a".repeat(64),
  policy_reference: "SYN-IDENTITY",
  finding_reference: "Synthetic examined person",
  valid_until: "2030-01-01T00:00:00Z",
  access_intent: "non_digital",
  declared_subject: "",
  created_at: "2026-11-01T00:00:00Z",
};
const page = (data: unknown[], next: string | null = null) => ({
  data,
  page: { has_more: next !== null, next_cursor: next },
});
afterEach(() => vi.unstubAllGlobals());
it("pins original workspace and current source without disclosing identity evidence", async () => {
  const fetcher = vi.fn(async () => Response.json(page([finding])));
  vi.stubGlobal("fetch", fetcher);
  expect(await listEmploymentPersonFindings(scope, basis)).toEqual(page([finding]));
  const [url, init] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(url).toBe(
    `/api/v1/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-person-findings?hr_intake=v1&staff_version=3&employment_epoch=2&limit=20`,
  );
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});
it.each([
  { ...finding, staff_id: "44444444-4444-4444-8444-444444444444" },
  { ...finding, employment_epoch: 1 },
  { ...finding, version: 1 },
  { ...finding, evidence: [{ private: "identity" }] },
])("rejects foreign stale pending or over-disclosed finding options", async (option) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(page([option]))),
  );
  await expect(listEmploymentPersonFindings(scope, basis)).rejects.toThrow(/could not be verified/);
});
it("preserves empty filtered-page continuation", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(page([], "opaque-next"))),
  );
  expect((await listEmploymentPersonFindings(scope, basis)).page.next_cursor).toBe("opaque-next");
});
