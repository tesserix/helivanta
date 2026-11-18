import { afterEach, expect, it, vi } from "vitest";
import { readAccountPresentations, readMemberPresentations } from "./member-presentations";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "owner" };
const view = (subject: string) => ({
  subject,
  available: true,
  name: "Asha Rao",
  email: "asha@example.test",
  roles: [{ key: "staff_member", label: "Staff member (membership only)" }],
});
afterEach(() => vi.unstubAllGlobals());

it("bounds identity batches, binds tenant and actor, and preserves opaque subjects", async () => {
  const calls: RequestInit[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      expect(url).toBe("/api/v1/iam/member-presentations");
      calls.push(init);
      const { subjects } = JSON.parse(String(init.body));
      expect(subjects.length).toBeLessThanOrEqual(20);
      expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
      expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
      return new Response(
        JSON.stringify({
          tenant_id: scope.tenantId,
          actor_subject: scope.subject,
          data: subjects.map(view),
        }),
      );
    }),
  );
  const subjects = Array.from({ length: 21 }, (_, i) => `account-${i}`);
  expect((await readMemberPresentations(scope, subjects)).map((x) => x.subject)).toEqual(
    [...subjects].sort(),
  );
  expect(calls).toHaveLength(2);
});

it.each([
  { tenant_id: "22222222-2222-4222-8222-222222222222" },
  { actor_subject: "another-owner" },
  { data: [view("wrong-target")] },
  { data: [view("target"), view("target")] },
  { data: [{ ...view("target"), name: undefined }] },
  { data: [{ ...view("target"), available: false }] },
  { data: [{ ...view("target"), roles: [] }] },
])("withholds mismatched or incomplete identity presentations", async (patch) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            actor_subject: scope.subject,
            data: [view("target")],
            ...patch,
          }),
        ),
    ),
  );
  await expect(readMemberPresentations(scope, ["target"])).rejects.toThrow();
});

it("keeps a known unavailable account unnamed", async () => {
  const unavailable = { subject: "target", available: false, roles: view("target").roles };
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            actor_subject: scope.subject,
            data: [unavailable],
          }),
        ),
    ),
  );
  expect(await readMemberPresentations(scope, ["target"])).toEqual([unavailable]);
});

it("resolves verified native accounts before membership without inventing roles or unknown names", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      expect(url).toBe("/api/v1/iam/account-presentations");
      return Response.json({
        tenant_id: scope.tenantId,
        actor_subject: scope.subject,
        data: [{ ...view("native"), roles: [] }],
      });
    }),
  );
  expect(await readAccountPresentations(scope, ["unknown", "native"])).toEqual([
    { ...view("native"), roles: [] },
  ]);
});

it.each([
  { data: [view("extra")] },
  { data: [view("a"), view("a")] },
  { data: [view("b"), view("a")] },
  { tenant_id: "22222222-2222-4222-8222-222222222222" },
  { actor_subject: "other-owner" },
])(
  "rejects foreign, duplicate, unordered or incorrectly bound account names: %j",
  async (patch) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({
          tenant_id: scope.tenantId,
          actor_subject: scope.subject,
          data: [view("a")],
          ...patch,
        }),
      ),
    );
    await expect(readAccountPresentations(scope, ["a", "b"])).rejects.toThrow();
  },
);

it.each([401, 403, 409, 503])(
  "does not disguise account source failure %s as an empty lookup",
  async (status) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json({}, { status })),
    );
    await expect(readAccountPresentations(scope, ["a"])).rejects.toThrow();
  },
);

it("conceals unknown accounts on a not-found response", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({}, { status: 404 })),
  );
  expect(await readAccountPresentations(scope, ["unknown"])).toEqual([]);
});
