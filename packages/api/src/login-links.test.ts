import { afterEach, expect, it, vi } from "vitest";
import { loginLinkInputContract } from "./login-link-contract";
import {
  proposeLoginLink,
  decideLoginLink,
  fetchLoginLinks,
  fetchLoginLinkHistory,
  loginLinksKey,
  type LoginLinkScope,
} from "./login-links";

const establishmentId = "22222222-2222-4222-8222-222222222222";
const scope: LoginLinkScope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  establishment: { kind: "hospital", id: establishmentId },
  staffId: "33333333-3333-4333-8333-333333333333",
};
const pharmacyScope: LoginLinkScope = {
  ...scope,
  establishment: { kind: "pharmacy", id: establishmentId },
};
const id = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const input = { subject: "user-candidate", reason: "Verified synthetic account" };
const saved = {
  id,
  staff_id: scope.staffId,
  ...input,
  status: "pending",
  version: 1,
  proposed_by_subject: "reviewer-a",
  proposal_reason: input.reason,
  actor_subject: "reviewer-a",
  created_at: "2026-10-26T00:00:00Z",
  updated_at: "2026-10-26T00:00:00Z",
};
const base = `/api/v1/workforce/hospitals/${establishmentId}/staff/${scope.staffId}/login-links`;
const pharmacyBase = `/api/v1/workforce/pharmacies/${establishmentId}/staff/${scope.staffId}/login-links`;
afterEach(() => vi.unstubAllGlobals());

it("preserves proposal identity across retries through Global API and pins the tenant", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(new Response("unavailable", { status: 503 }))
    .mockResolvedValueOnce(Response.json(saved));
  vi.stubGlobal("fetch", fetch);
  await expect(proposeLoginLink(scope, id, input)).rejects.toMatchObject({ status: 503 });
  await expect(proposeLoginLink(scope, id, input)).resolves.toEqual(saved);
  for (const [url, init] of fetch.mock.calls) {
    expect(url).toBe(`${base}/${id}`);
    expect(init.method).toBe("PUT");
    expect(JSON.parse(init.body)).toEqual(input);
    expect(init.headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
    expect(init.cache).toBe("no-store");
  }
});

it("rejects a decision receipt for another staff member", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json({ ...saved, staff_id: establishmentId }));
  vi.stubGlobal("fetch", fetch);
  const decision = {
    expected_version: 1,
    decision: "approve" as const,
    reason: "Independent review",
  };
  await expect(decideLoginLink(scope, id, decision)).rejects.toThrow("did not match");
  expect(fetch.mock.calls[0][0]).toBe(`${base}/${id}/decision`);
  expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual(decision);
});

it("rejects invalid identifiers and commands before sending a request", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(proposeLoginLink(scope, scope.staffId, input)).rejects.toThrow();
  await expect(proposeLoginLink({ ...scope, tenantId: "" }, id, input)).rejects.toThrow();
  await expect(
    proposeLoginLink(
      { ...scope, establishment: { kind: "outlet" as "pharmacy", id: establishmentId } },
      id,
      input,
    ),
  ).rejects.toThrow();
  await expect(
    proposeLoginLink(scope, id, { ...input, subject: " user-candidate" }),
  ).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});

it("reads bounded pages with cancellation and rejects foreign records", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(
      Response.json({ data: [saved], page: { has_more: true, next_cursor: "opaque+/=" } }),
    )
    .mockResolvedValueOnce(
      Response.json({
        data: [{ ...saved, staff_id: establishmentId }],
        page: { has_more: false, next_cursor: null },
      }),
    );
  vi.stubGlobal("fetch", fetch);
  const controller = new AbortController();
  const page = await fetchLoginLinks(scope, { cursor: "opaque+/=", signal: controller.signal });
  expect(page.data).toEqual([saved]);
  expect(fetch.mock.calls[0][0]).toBe(`${base}?limit=50&cursor=opaque%2B%2F%3D`);
  expect(fetch.mock.calls[0][1].signal).toBe(controller.signal);
  await expect(fetchLoginLinks(scope)).rejects.toThrow("did not match");
});

it("validates history belongs to the requested link", async () => {
  const fetch = vi.fn().mockResolvedValue(
    Response.json({
      data: [
        {
          id: scope.staffId,
          version: 1,
          status: "pending",
          actor_subject: "reviewer-a",
          reason: "Private reason",
          created_at: saved.created_at,
        },
      ],
      page: { has_more: false, next_cursor: null },
    }),
  );
  vi.stubGlobal("fetch", fetch);
  await expect(fetchLoginLinkHistory(scope, id)).rejects.toThrow("did not match");
  expect(fetch.mock.calls[0][0]).toBe(`${base}/${id}/history?limit=50`);
});

it("rejects ambiguous account identifiers instead of silently changing them", () => {
  for (const subject of [
    " user-candidate",
    "user-candidate ",
    "user candidate",
    "user:other",
    "user#member",
    "*",
    "user\u0085candidate",
  ]) {
    expect(
      loginLinkInputContract.safeParse({ subject, reason: "Verified account" }).success,
      subject,
    ).toBe(false);
  }
  expect(
    loginLinkInputContract.parse({ subject: "User-Candidate_01", reason: "Verified account" })
      .subject,
  ).toBe("User-Candidate_01");
});

it("addresses a pharmacy Staff record through the pharmacy family", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(Response.json(saved))
    .mockResolvedValueOnce(Response.json({ ...saved, status: "active", version: 2 }))
    .mockResolvedValueOnce(
      Response.json({ data: [saved], page: { has_more: false, next_cursor: null } }),
    )
    .mockResolvedValueOnce(
      Response.json({ data: [], page: { has_more: false, next_cursor: null } }),
    );
  vi.stubGlobal("fetch", fetch);
  await proposeLoginLink(pharmacyScope, id, input);
  await decideLoginLink(pharmacyScope, id, {
    expected_version: 1,
    decision: "approve",
    reason: "Independent review",
  });
  await fetchLoginLinks(pharmacyScope);
  await fetchLoginLinkHistory(pharmacyScope, id);
  expect(fetch.mock.calls.map(([url, init]) => [init.method ?? "GET", url])).toEqual([
    ["PUT", `${pharmacyBase}/${id}`],
    ["POST", `${pharmacyBase}/${id}/decision`],
    ["GET", `${pharmacyBase}?limit=50`],
    ["GET", `${pharmacyBase}/${id}/history?limit=50`],
  ]);
});

it("keys a hospital and a pharmacy with the same ID apart", () => {
  expect(loginLinksKey(scope)).not.toEqual(loginLinksKey(pharmacyScope));
  expect(loginLinksKey(pharmacyScope)).toEqual([
    "workforce",
    "login-links",
    scope.tenantId,
    "pharmacy",
    establishmentId,
    scope.staffId,
  ]);
});
