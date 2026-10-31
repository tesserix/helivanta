import { afterEach, expect, it, vi } from "vitest";
import { searchPersonReviewTargets, listPersonReviewInbox } from "./person-review-browse";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  subject: "synthetic-reviewer",
};
const target = {
  id: "33333333-3333-4333-8333-333333333333",
  hospital_id: scope.hospitalId,
  employee_code: "SYN-JOINER",
  display_name: "Synthetic Joiner",
  employment_status: "inactive",
  staff_version: 1,
  created_at: "2026-10-31T00:00:00Z",
};
const summary = {
  id: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa",
  hospital_id: scope.hospitalId,
  staff_id: target.id,
  employee_code: target.employee_code,
  display_name: target.display_name,
  status: "pending",
  version: 1,
  valid_until: "2027-01-01T00:00:00Z",
  created_at: target.created_at,
};
const envelope = (row: unknown) => ({ data: [row], page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());

it("searches Staff ID in the JSON body and pins the original workspace", async () => {
  const fetcher = vi.fn(async () => Response.json(envelope(target)));
  vi.stubGlobal("fetch", fetcher);
  expect(await searchPersonReviewTargets(scope, "syn-joiner")).toEqual(envelope(target));
  const [path, init] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(path).toBe(
    `/api/v1/workforce/hospitals/${scope.hospitalId}/person-review-targets/search?limit=50`,
  );
  expect(path).not.toContain("syn-joiner");
  expect(init.method).toBe("POST");
  expect(JSON.parse(String(init.body))).toEqual({ employee_code: "syn-joiner" });
  expect(init.cache).toBe("no-store");
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

it("filters the inbox by recorded status and preserves pagination", async () => {
  const fetcher = vi.fn(async () => Response.json(envelope(summary)));
  vi.stubGlobal("fetch", fetcher);
  expect(await listPersonReviewInbox(scope, "pending", "opaque-next")).toEqual(envelope(summary));
  expect((fetcher.mock.calls[0] as unknown as [string, RequestInit])[0]).toBe(
    `/api/v1/workforce/hospitals/${scope.hospitalId}/person-review-inbox?limit=50&cursor=opaque-next&status=pending`,
  );
});

it.each([
  { ...summary, hospital_id: "44444444-4444-4444-8444-444444444444" },
  { ...summary, status: "approved" },
  { ...summary, finding_reference: "private-value-not-requested" },
])(
  "refuses summaries from another hospital/status or containing unrequested private fields",
  async (row) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json(envelope(row))),
    );
    await expect(listPersonReviewInbox(scope, "pending")).rejects.toThrow(/could not be verified/i);
  },
);

it("refuses an exact search response for another Staff ID", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(envelope(target))),
  );
  await expect(searchPersonReviewTargets(scope, "SYN-OTHER")).rejects.toThrow(
    /could not be verified/i,
  );
});
