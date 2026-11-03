import { afterEach, expect, it, vi } from "vitest";
import * as workspace from "./staff-posting-workspace";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-reviewer" };
const hospital = {
  hospital_id: "22222222-2222-4222-8222-222222222222",
  name: "Synthetic hospital",
  can_submit: false,
  can_review: true,
};
const page = { data: [hospital], page: { has_more: false, next_cursor: null } };
afterEach(() => vi.unstubAllGlobals());

it("discovers reviewer capabilities through the Global API with the original account headers", async () => {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(page), { status: 200 }));
  vi.stubGlobal("fetch", fetch);
  expect(await workspace.listStaffPostingReviewScopes(scope)).toEqual(page);
  expect(fetch).toHaveBeenCalledWith(
    "/api/v1/workforce/posting-review-scopes?limit=20",
    expect.objectContaining({ cache: "no-store" }),
  );
  const init = fetch.mock.calls[0][1] as RequestInit;
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

it("retains empty authorized pages and their opaque continuation", async () => {
  const empty = { data: [], page: { has_more: true, next_cursor: "next-page" } };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(empty))));
  expect(await workspace.listStaffPostingReviewScopes(scope)).toEqual(empty);
});

it("rejects private fields, missing capabilities, duplicate hospitals and invalid page boundaries", async () => {
  for (const invalid of [
    { ...page, data: [{ ...hospital, actor_subject: "private" }] },
    {
      ...page,
      data: [{ hospital_id: hospital.hospital_id, name: hospital.name, can_review: true }],
    },
    { ...page, data: [{ ...hospital, can_submit: "false" }] },
    { ...page, data: [hospital, hospital] },
    { ...page, data: Array.from({ length: 21 }, () => hospital) },
    { ...page, page: { has_more: false, next_cursor: "unexpected" } },
    { ...page, page: { has_more: true, next_cursor: "current-page" } },
  ]) {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(invalid))));
    await expect(workspace.listStaffPostingReviewScopes(scope, "current-page")).rejects.toThrow();
  }
});

it("does not send a request from an invalid account scope or a cancelled request", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(
    workspace.listStaffPostingReviewScopes({ ...scope, tenantId: "invalid" }),
  ).rejects.toThrow();
  await expect(
    workspace.listStaffPostingReviewScopes({ ...scope, subject: "bad\nsubject" }),
  ).rejects.toThrow();
  const abort = new AbortController();
  abort.abort();
  await expect(workspace.listStaffPostingReviewScopes(scope, null, abort.signal)).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});

it("withholds a page when cancellation happens during transport", async () => {
  const abort = new AbortController();
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation(async () => {
      abort.abort();
      return new Response(JSON.stringify(page));
    }),
  );
  await expect(workspace.listStaffPostingReviewScopes(scope, null, abort.signal)).rejects.toThrow();
});

it("preserves authorization outages instead of presenting an empty verified page", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          error: "posting_source_unavailable",
          message: "Current source unavailable",
        }),
        { status: 503 },
      ),
    ),
  );
  await expect(workspace.listStaffPostingReviewScopes(scope)).rejects.toMatchObject({
    code: "posting_source_unavailable",
    status: 503,
  });
});
