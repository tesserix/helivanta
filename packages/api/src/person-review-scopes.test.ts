import { afterEach, expect, it, vi } from "vitest";
import { listPersonReviewScopes, listPersonReviewAuthorityScopes } from "./person-review-scopes";

const workspace = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-reviewer",
};
const hospital = {
  hospital_id: "22222222-2222-4222-8222-222222222222",
  name: "Synthetic Hospital",
  can_submit: false,
  can_review: true,
};
afterEach(() => vi.unstubAllGlobals());

it("preserves empty-page continuation and original workspace through the Global API", async () => {
  const first = { data: [], page: { has_more: true, next_cursor: "opaque-next" } };
  const last = { data: [hospital], page: { has_more: false, next_cursor: null } };
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(Response.json(first))
    .mockResolvedValueOnce(Response.json(last));
  vi.stubGlobal("fetch", fetcher);
  const page = await listPersonReviewScopes(workspace);
  expect(page).toEqual(first);
  expect(await listPersonReviewScopes(workspace, page.page.next_cursor)).toEqual(last);
  expect(fetcher.mock.calls[0][0]).toBe("/api/v1/workforce/person-review-scopes?limit=20");
  expect(fetcher.mock.calls[1][0]).toBe(
    "/api/v1/workforce/person-review-scopes?limit=20&cursor=opaque-next",
  );
  for (const [, init] of fetcher.mock.calls) {
    expect(init.cache).toBe("no-store");
    expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(workspace.tenantId);
    expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(workspace.subject);
  }
});

it.each([
  { data: [{ ...hospital, can_review: false }], page: { has_more: false, next_cursor: null } },
  { data: [hospital], page: { has_more: true, next_cursor: null } },
  { data: [], page: { has_more: false, next_cursor: "unexpected-next" } },
  {
    data: [{ ...hospital, private_finding: "unrequested-private-value" }],
    page: { has_more: false, next_cursor: null },
  },
])("refuses unverifiable scope/cursor responses without echoing private fields", async (body) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(body)),
  );
  await expect(listPersonReviewScopes(workspace)).rejects.toThrow(/could not be verified/i);
  await expect(listPersonReviewScopes(workspace)).rejects.not.toThrow(/unrequested-private-value/);
});

it("does not release a scope response after cancellation", async () => {
  const controller = new AbortController();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      controller.abort();
      return Response.json({ data: [hospital], page: { has_more: false, next_cursor: null } });
    }),
  );
  await expect(listPersonReviewScopes(workspace, null, controller.signal)).rejects.toThrow();
});

it("discovers current person authority separately from legacy actor-link cursors", async () => {
  const fetcher = vi.fn<typeof fetch>(async () =>
    Response.json({ data: [hospital], page: { has_more: false, next_cursor: null } }),
  );
  vi.stubGlobal("fetch", fetcher);
  await expect(listPersonReviewAuthorityScopes(workspace)).resolves.toMatchObject({
    data: [hospital],
  });
  expect(fetcher.mock.calls[0][0]).toBe(
    "/api/v1/workforce/person-review-authority-scopes?limit=20",
  );
});

it.each([
  { data: [hospital, hospital], page: { has_more: false, next_cursor: null } },
  { data: [hospital], page: { has_more: true, next_cursor: "same" } },
])("refuses ambiguous authority scope pages", async (body) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(body)),
  );
  await expect(listPersonReviewAuthorityScopes(workspace, "same")).rejects.toThrow();
});

it("keeps authority discovery bounded and private across filtered empty pages", async () => {
  const fetcher = vi
    .fn<typeof fetch>()
    .mockResolvedValueOnce(
      Response.json({ data: [], page: { has_more: true, next_cursor: "authority-next" } }),
    )
    .mockResolvedValueOnce(
      Response.json({ data: [hospital], page: { has_more: false, next_cursor: null } }),
    );
  vi.stubGlobal("fetch", fetcher);
  const first = await listPersonReviewAuthorityScopes(workspace);
  expect(first.data).toEqual([]);
  expect((await listPersonReviewAuthorityScopes(workspace, first.page.next_cursor)).data).toEqual([
    hospital,
  ]);
  expect(fetcher.mock.calls[1][0]).toBe(
    "/api/v1/workforce/person-review-authority-scopes?limit=20&cursor=authority-next",
  );
  const headers = new Headers(fetcher.mock.calls[1][1]?.headers);
  expect(headers.get("X-Helivanta-Expected-Tenant")).toBe(workspace.tenantId);
  expect(headers.get("X-Helivanta-Expected-Subject")).toBe(workspace.subject);
  expect(fetcher.mock.calls[1][1]?.cache).toBe("no-store");
});

it("discards authority discovery after workspace cancellation", async () => {
  const controller = new AbortController();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      controller.abort();
      return Response.json({ data: [hospital], page: { has_more: false, next_cursor: null } });
    }),
  );
  await expect(
    listPersonReviewAuthorityScopes(workspace, null, controller.signal),
  ).rejects.toThrow();
});
