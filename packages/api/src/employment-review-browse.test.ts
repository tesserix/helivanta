import { afterEach, expect, it, vi } from "vitest";
import { listEmploymentReviewInbox } from "./employment-review-browse";
const workspace = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-reviewer",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  mode: "review" as const,
};
const row = {
  id: "019a0000-0000-7000-8000-000000000001",
  hospital_id: workspace.hospitalId,
  staff_id: "33333333-3333-4333-8333-333333333333",
  employee_code: "SYN-TERMS",
  display_name: "Synthetic Joiner",
  status: "pending",
  version: 1,
  created_at: "2026-11-01T00:00:00Z",
};
const page = (data: unknown[], cursor: string | null = null) => ({
  data,
  page: { has_more: cursor !== null, next_cursor: cursor },
});
afterEach(() => vi.unstubAllGlobals());
it("keeps original reviewer mode and workspace for every cursor page", async () => {
  const fetcher = vi.fn(async () => Response.json(page([row])));
  vi.stubGlobal("fetch", fetcher);
  expect(await listEmploymentReviewInbox(workspace, "pending", "opaque-next")).toEqual(page([row]));
  const [url, init] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(url).toBe(
    `/api/v1/workforce/hospitals/${workspace.hospitalId}/employment-review-inbox?employment_review=v1&limit=50&status=pending&cursor=opaque-next`,
  );
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(workspace.tenantId);
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(workspace.subject);
  expect(init.cache).toBe("no-store");
});
it.each([
  page([{ ...row, hospital_id: "44444444-4444-4444-8444-444444444444" }]),
  page([{ ...row, status: "approved" }]),
  page([{ ...row, reason: "private" }]),
  page([row, row]),
  { data: [row], page: { has_more: true, next_cursor: null } },
])("refuses foreign mismatched duplicate or unrequested private responses", async (body) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(body)),
  );
  await expect(listEmploymentReviewInbox(workspace)).rejects.toThrow(/could not be verified/);
});
it("discards a cancelled inbox", async () => {
  const controller = new AbortController();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      controller.abort();
      return Response.json(page([row]));
    }),
  );
  await expect(
    listEmploymentReviewInbox(workspace, "pending", null, controller.signal),
  ).rejects.toThrow();
});
