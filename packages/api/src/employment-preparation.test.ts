import {
  getGovernanceEmploymentSource,
  getGovernanceEmploymentDraft,
  listGovernanceEmploymentDrafts,
  listGovernanceEmploymentHistory,
  saveGovernanceEmploymentDraft,
} from "./employment-preparation";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-preparer",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
};
const id = "019a0000-0000-7000-8000-000000000001";
const command = {
  command_id: "019a0000-0000-7000-8000-000000000002",
  expected_version: 0,
  staff_version: 3,
  employment_epoch: 2,
  engagement_kind: "employee" as const,
  duration: "ongoing" as const,
  starts_at: "2026-11-01T00:00:00Z",
  contracting_party: "",
  sponsor_id: "",
  reason: "Synthetic employment preparation",
  evidence_reference: "SYN-OFFER",
};
const { command_id: revision, expected_version: expected, ...terms } = command;
const draft = {
  ...terms,
  id,
  revision_id: revision,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  version: expected + 1,
  status: "draft",
  proposed_by_subject: scope.subject,
  actor_subject: scope.subject,
  created_at: "2026-11-01T00:00:00Z",
  updated_at: "2026-11-01T00:00:00Z",
};
const source = {
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  staff_version: 3,
  employment_epoch: 2,
};
const page = (data: unknown[], more = false) => ({
  data,
  page: { has_more: more, next_cursor: more ? "private-next" : null },
});
afterEach(() => vi.unstubAllGlobals());

it("uses only exact governance paths with original account headers and cancellation", async () => {
  const controller = new AbortController();
  const fetcher = vi.fn(async (input: RequestInfo | URL) =>
    Response.json(String(input).includes("preparation-source") ? source : draft),
  );
  vi.stubGlobal("fetch", fetcher);
  expect(await getGovernanceEmploymentSource(scope, controller.signal)).toEqual(source);
  expect(await getGovernanceEmploymentDraft(scope, id, controller.signal)).toEqual(draft);
  expect(await saveGovernanceEmploymentDraft(scope, id, command, controller.signal)).toEqual(draft);
  expect(fetcher.mock.calls.map(([url]) => String(url))).toEqual([
    `/api/v1/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-preparation-source`,
    `/api/v1/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-preparation-periods/${id}?lifecycle=withdrawal-v1`,
    `/api/v1/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-preparation-periods/${id}`,
  ]);
  for (const call of fetcher.mock.calls) {
    const init = (call as unknown as [string, RequestInit])[1];
    expect(init.cache).toBe("no-store");
    expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
    expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
    expect(init.signal).toBe(controller.signal);
  }
});
it("keeps real history revisions distinct from the period key", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json(
        page([
          draft,
          {
            ...draft,
            version: 2,
            revision_id: "019a0000-0000-7000-8000-000000000003",
            status: "withdrawn",
          },
        ]),
      ),
    ),
  );
  expect((await listGovernanceEmploymentHistory(scope, id)).data).toHaveLength(2);
});
it("requests bounded sealed-cursor draft pages", async () => {
  const fetcher = vi.fn(async () => Response.json(page([draft], true)));
  vi.stubGlobal("fetch", fetcher);
  expect((await listGovernanceEmploymentDrafts(scope, "private-next")).page.next_cursor).toBe(
    "private-next",
  );
  expect(String((fetcher.mock.calls[0] as unknown as [string, RequestInit])[0])).toContain(
    "employment-preparation-periods?limit=50&cursor=private-next",
  );
});
it.each([
  { ...source, staff_id: scope.hospitalId },
  { ...source, hospital_id: scope.staffId },
  { ...source, authority: { source_grant_id: "private" } },
  { ...source, staff_version: 0 },
])("rejects an unverified Staff basis", async (value) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(value)),
  );
  await expect(getGovernanceEmploymentSource(scope)).rejects.toThrow("verified");
});
it.each([
  { ...draft, id: "019a0000-0000-7000-8000-000000000004" },
  { ...draft, actor_subject: "another-preparer" },
  { ...draft, revision_id: "019a0000-0000-7000-8000-000000000004" },
  { ...draft, version: 2 },
  { ...draft, staff_version: 4 },
  { ...draft, employment_epoch: 3 },
  { ...draft, status: "withdrawn" },
  { ...draft, staff_id: scope.hospitalId },
  { ...draft, input_hash: "private" },
])("rejects a mismatched or private command receipt", async (value) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(value)),
  );
  await expect(saveGovernanceEmploymentDraft(scope, id, command)).rejects.toThrow("receipt");
});
it.each([
  page([draft, draft]),
  page([{ ...draft, status: "withdrawn" }]),
  page([{ ...draft, hospital_id: scope.staffId }]),
  page(
    Array.from({ length: 51 }, (_, i) => ({
      ...draft,
      id: `019a0000-0000-7000-8000-${String(i).padStart(12, "0")}`,
    })),
  ),
  { data: [draft], page: { has_more: true, next_cursor: null } },
])("rejects duplicate, out-of-scope or unbounded draft pages", async (value) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(value)),
  );
  await expect(listGovernanceEmploymentDrafts(scope)).rejects.toThrow("verified");
});
it.each([
  { rows: [draft, { ...draft, version: 2 }] },
  { rows: [draft, { ...draft, revision_id: "019a0000-0000-7000-8000-000000000003" }] },
  { rows: [{ ...draft, id: "019a0000-0000-7000-8000-000000000003" }] },
])("rejects ambiguous or foreign period history", async ({ rows }) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(page(rows))),
  );
  await expect(listGovernanceEmploymentHistory(scope, id)).rejects.toThrow("verified");
});
it.each([403, 503])("does not fall back after a governance source failure %s", async (status) => {
  const fetcher = vi.fn(async () =>
    Response.json({ error: "source_unavailable", message: "Synthetic source failure" }, { status }),
  );
  vi.stubGlobal("fetch", fetcher);
  await expect(saveGovernanceEmploymentDraft(scope, id, command)).rejects.toMatchObject({ status });
  expect(fetcher).toHaveBeenCalledTimes(1);
});
it("rejects mixed authority/family scopes and invalid real IDs before network I/O", async () => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  await expect(
    getGovernanceEmploymentSource({ ...scope, hrIntake: true } as never),
  ).rejects.toThrow();
  await expect(
    getGovernanceEmploymentSource({ ...scope, pharmacyId: scope.hospitalId } as never),
  ).rejects.toThrow();
  await expect(saveGovernanceEmploymentDraft(scope, scope.staffId, command)).rejects.toThrow();
  await expect(
    saveGovernanceEmploymentDraft(scope, id, { ...command, command_id: scope.staffId }),
  ).rejects.toThrow();
  expect(fetcher).not.toHaveBeenCalled();
});
it("discards already cancelled and late-cancelled responses", async () => {
  const controller = new AbortController();
  controller.abort();
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  await expect(getGovernanceEmploymentSource(scope, controller.signal)).rejects.toMatchObject({
    name: "AbortError",
  });
  expect(fetcher).not.toHaveBeenCalled();
  const late = new AbortController();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      late.abort();
      return Response.json(source);
    }),
  );
  await expect(getGovernanceEmploymentSource(scope, late.signal)).rejects.toMatchObject({
    name: "AbortError",
  });
});

it.each([
  { ...draft, id: scope.staffId },
  { ...draft, revision_id: scope.staffId },
  { ...draft, hospital_id: scope.staffId },
  { ...draft, authority: { private: true } },
])("rejects an unverified selected-period read", async (value) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(value)),
  );
  await expect(getGovernanceEmploymentDraft(scope, id)).rejects.toThrow("verified");
});
it("rejects a command with no representable next revision before network I/O", async () => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  await expect(
    saveGovernanceEmploymentDraft(scope, id, { ...command, expected_version: 9007199254740990 }),
  ).rejects.toThrow();
  expect(fetcher).not.toHaveBeenCalled();
});
