import { afterEach, expect, it, vi } from "vitest";
import {
  listOrganizationDepartments,
  recordOrganizationDepartment,
} from "./organization-directory";

const scope = {
  tenantId: "22222222-2222-4222-8222-222222222222",
  subject: "synthetic-governance-manager",
};
const hospital = "11111111-1111-4111-8111-111111111111";
const node = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const revision = "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb";
const row = {
  id: node,
  hospital_id: hospital,
  revision_id: revision,
  version: 1,
  code: "PHARMA",
  name: "Pharmacy",
  kind: "department",
  status: "active",
  effective_at: "2026-11-02T00:00:00Z",
  created_at: "2026-11-02T00:00:00Z",
};
const page = (data: unknown[] = [row]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
it("loads bounded structural metadata through Global API with exact account assertions", async () => {
  const fetcher = vi.fn().mockResolvedValue(Response.json(page()));
  vi.stubGlobal("fetch", fetcher);
  const controller = new AbortController();
  expect(await listOrganizationDepartments(scope, hospital, null, controller.signal)).toEqual(
    page(),
  );
  expect(fetcher.mock.calls[0]?.[0]).toBe(
    `/api/v1/organization/hospitals/${hospital}/departments?limit=50`,
  );
  const init = fetcher.mock.calls[0]?.[1];
  expect(init.cache).toBe("no-store");
  expect(init.signal).toBe(controller.signal);
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});
it.each([
  ["foreign hospital", page([{ ...row, hospital_id: scope.tenantId }])],
  ["duplicate node", page([row, row])],
  ["missing continuation", { data: [row], page: { has_more: true, next_cursor: null } }],
  ["unrequested continuation", { data: [row], page: { has_more: false, next_cursor: "opaque" } }],
  ["private projection", page([{ ...row, actor_subject: "private-subject" }])],
])("rejects %s instead of showing unverified rows", async (_label, response) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(response)));
  await expect(listOrganizationDepartments(scope, hospital)).rejects.toThrow();
});
it("requires the exact retained receipt for a submitted structural command", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(Response.json({ ...row, id: "019ccccc-cccc-7ccc-8ccc-cccccccccccc" }));
  vi.stubGlobal("fetch", fetcher);
  await expect(
    recordOrganizationDepartment(scope, hospital, node, revision, {
      expected_version: 0,
      code: "PHARMA",
      name: "Pharmacy",
      kind: "department",
      status: "active",
      reason: "Synthetic QA",
    }),
  ).rejects.toThrow();
  expect(fetcher.mock.calls[0]?.[1].method).toBe("PUT");
});

it("uses retained hospital-reference receipts without treating existence as professional approval", async () => {
  const { listOrganizationHospitals, registerOrganizationHospital } =
    await import("./organization-directory");
  const root = {
    hospital_id: hospital,
    request_id: revision,
    status: "verified",
    created_at: row.created_at,
  };
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(Response.json(page([{ ...root, version: 2 }])))
    .mockResolvedValueOnce(Response.json(root));
  vi.stubGlobal("fetch", fetcher);
  expect((await listOrganizationHospitals(scope)).data).toEqual([{ ...root, version: 2 }]);
  expect(
    await registerOrganizationHospital(scope, hospital, revision, " Synthetic source lookup "),
  ).toEqual(root);
  expect(fetcher.mock.calls[1]?.[0]).toBe(
    `/api/v1/organization/hospitals/${hospital}/registrations/${revision}`,
  );
  expect(JSON.parse(fetcher.mock.calls[1]?.[1].body)).toEqual({
    reason: "Synthetic source lookup",
  });
});
it("validates all page entries in a node's history and rejects another node's revision", async () => {
  const { listOrganizationDepartmentRevisions } = await import("./organization-directory");
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(Response.json(page([{ ...row, id: revision }]))),
  );
  await expect(listOrganizationDepartmentRevisions(scope, hospital, node)).rejects.toThrow();
});
it("accepts only a matching normalized command receipt and keeps protected reasons out of its result", async () => {
  const fetcher = vi.fn().mockResolvedValue(Response.json(row));
  vi.stubGlobal("fetch", fetcher);
  const saved = await recordOrganizationDepartment(
    scope,
    hospital.toUpperCase(),
    node.toUpperCase(),
    revision.toUpperCase(),
    {
      expected_version: 0,
      code: " pharma ",
      name: " Pharmacy ",
      kind: "department",
      status: "active",
      reason: "Synthetic   QA",
    },
  );
  expect(saved).toEqual(row);
  expect(saved).not.toHaveProperty("reason");
  expect(JSON.parse(fetcher.mock.calls[0]?.[1].body)).toMatchObject({
    code: "PHARMA",
    name: "Pharmacy",
    reason: "Synthetic QA",
  });
});

it("accepts the actual retained hospital head version on a source page", async () => {
  const { listOrganizationHospitals } = await import("./organization-directory");
  const head = {
    hospital_id: hospital,
    request_id: revision,
    status: "verified",
    version: 2,
    created_at: row.created_at,
  };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(page([head]))));
  expect((await listOrganizationHospitals(scope)).data).toEqual([head]);
});

it("selects current posting department choices without requesting structure governance", async () => {
  const { listPostingDepartmentChoices } = await import("./organization-directory");
  const fetcher = vi.fn().mockResolvedValue(Response.json(page()));
  vi.stubGlobal("fetch", fetcher);
  const controller = new AbortController();
  expect(
    await listPostingDepartmentChoices(scope, hospital, "opaque+/=", controller.signal),
  ).toEqual(page());
  const [url, options] = fetcher.mock.calls[0]!;
  const parsed = new URL(String(url), "https://example.invalid");
  expect(parsed.pathname).toBe(
    `/api/v1/organization/hospitals/${hospital}/posting-department-choices`,
  );
  expect(parsed.searchParams.get("limit")).toBe("50");
  expect(parsed.searchParams.get("cursor")).toBe("opaque+/=");
  expect(options.cache).toBe("no-store");
  expect(options.signal).toBe(controller.signal);
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

it.each([
  ["retired head", page([{ ...row, status: "retired" }])],
  ["foreign hospital", page([{ ...row, hospital_id: scope.tenantId }])],
  ["private reason", page([{ ...row, reason: "private" }])],
  ["private actor", page([{ ...row, actor_subject: "private" }])],
  ["duplicate choice", page([row, row])],
  ["wrong current revision", page([{ ...row, revision_id: hospital }])],
  ["incomplete continuation", { data: [row], page: { has_more: true, next_cursor: null } }],
  ["unrequested continuation", { data: [row], page: { has_more: false, next_cursor: "opaque" } }],
  ["repeated continuation", { data: [row], page: { has_more: true, next_cursor: "opaque" } }],
])("rejects a %s in the posting-choice projection", async (_label, response) => {
  const { listPostingDepartmentChoices } = await import("./organization-directory");
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(response)));
  await expect(listPostingDepartmentChoices(scope, hospital, "opaque")).rejects.toThrow();
});

it("narrows posting department choices to one held department subtree", async () => {
  const { listPostingDepartmentChoices } = await import("./organization-directory");
  const fetcher = vi.fn().mockResolvedValue(Response.json(page()));
  vi.stubGlobal("fetch", fetcher);
  await listPostingDepartmentChoices(scope, hospital, null, undefined, node);
  const parsed = new URL(String(fetcher.mock.calls[0]![0]), "https://example.invalid");
  expect(parsed.searchParams.get("department")).toBe(node);
  expect(parsed.searchParams.has("cursor")).toBe(false);
  for (const invalid of [hospital, "", node.toUpperCase(), "not-a-department"]) {
    await expect(
      listPostingDepartmentChoices(scope, hospital, null, undefined, invalid),
    ).rejects.toThrow();
  }
  expect(fetcher).toHaveBeenCalledTimes(1);
});
