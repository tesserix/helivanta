import { afterEach, expect, it, vi } from "vitest";
import {
  listEmploymentReviewAuthorityInbox,
  listEmploymentReviewInbox,
} from "./employment-review-browse";
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
it.each([listEmploymentReviewInbox, listEmploymentReviewAuthorityInbox])(
  "rejects rows beyond the requested page limit",
  async (list) => {
    const rows = Array.from({ length: 51 }, (_, index) => ({
      ...row,
      id: `019a0000-0000-7000-8000-${(index + 1).toString(16).padStart(12, "0")}`,
    }));
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json(page(rows))),
    );
    await expect(list(workspace)).rejects.toThrow(/could not be verified/);
  },
);
it.each(["prepare", "review"] as const)(
  "keeps current governance %s inbox on its own Global API route",
  async (mode) => {
    const fetcher = vi.fn(async () => Response.json(page([row])));
    vi.stubGlobal("fetch", fetcher);
    expect(
      await listEmploymentReviewAuthorityInbox({ ...workspace, mode }, "pending", "private-next"),
    ).toEqual(page([row]));
    const [url, init] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe(
      `/api/v1/workforce/hospitals/${workspace.hospitalId}/employment-review-authority-inbox?mode=${mode}&limit=50&status=pending&cursor=private-next`,
    );
    expect(init.cache).toBe("no-store");
    expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(workspace.tenantId);
    expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(workspace.subject);
  },
);
it("does not expose private governance evidence or cancelled inbox responses", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json(
        page([{ ...row, governance_authority: { source_grant_id: "private-evidence" } }]),
      ),
    ),
  );
  await expect(listEmploymentReviewAuthorityInbox(workspace)).rejects.toThrow(
    /could not be verified/,
  );
  await expect(listEmploymentReviewAuthorityInbox(workspace)).rejects.not.toThrow(
    /private-evidence/,
  );
  const controller = new AbortController();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      controller.abort();
      return Response.json(page([row]));
    }),
  );
  await expect(
    listEmploymentReviewAuthorityInbox(workspace, "pending", null, controller.signal),
  ).rejects.toThrow();
});
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
