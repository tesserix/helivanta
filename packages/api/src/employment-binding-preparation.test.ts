import { webcrypto } from "node:crypto";
import {
  attachGovernanceEmploymentEvidence,
  getGovernanceEmploymentEvidence,
  listGovernanceEmploymentEvidence,
  listGovernanceEmploymentEvidenceHistory,
  withdrawGovernanceEmploymentEvidence,
  downloadGovernanceEmploymentEvidence,
} from "./employment-binding-preparation";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-native-preparer",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
  staffVersion: 3,
  employmentEpoch: 2,
  periodId: "019a0000-0000-7000-8000-000000000011",
  periodRevisionId: "019a0000-0000-7000-8000-000000000012",
  periodVersion: 1,
  periodStaffVersion: 3,
  periodEmploymentEpoch: 2,
};
const id = "019a0000-0000-7000-8000-000000000021";
const command = "019a0000-0000-7000-8000-000000000022";
const file = {
  id: "019a0000-0000-7000-8000-000000000023",
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  record_id: scope.staffId,
  record_kind: "enrollment" as const,
  record_version: 3,
  category: "employment_evidence" as const,
  filename: "synthetic-contract.png",
  content_type: "image/png" as const,
  size: 1,
  sha256: "2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881",
  status: "available" as const,
  scan_attempt: 1,
  uploaded_at: "2026-11-01T00:01:00Z",
  scanned_at: "2026-11-01T00:02:00Z",
  failure_code: null,
  created_at: "2026-11-01T00:00:00Z",
  updated_at: "2026-11-01T00:02:00Z",
  expires_at: "2026-11-01T00:30:00Z",
};
const binding = {
  id,
  revision_id: command,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  period_id: scope.periodId,
  period_revision_id: scope.periodRevisionId,
  period_version: 1,
  version: 1,
  status: "attached" as const,
  staff_version: 3,
  employment_epoch: 2,
  document_id: file.id,
  document_record_version: 3,
  filename: file.filename,
  content_type: file.content_type,
  size: file.size,
  sha256: file.sha256,
  source_generation: "9007199254740993",
  released_generation: "9007199254740994",
  scan_attempt: 1,
  scanned_at: file.scanned_at,
  proposed_by_subject: scope.subject,
  actor_subject: scope.subject,
  reason: "Synthetic exact terms",
  created_at: "2026-11-01T00:03:00Z",
  updated_at: "2026-11-01T00:03:00Z",
};
const input = { command_id: command, document_id: file.id, reason: binding.reason };
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
it("attaches exact selected terms and file through only the native binding family", async () => {
  const fetcher = vi.fn(async () => Response.json(binding));
  vi.stubGlobal("fetch", fetcher);
  expect(await attachGovernanceEmploymentEvidence(scope, id, input, file)).toEqual(binding);
  expect(await getGovernanceEmploymentEvidence(scope, id)).toEqual(binding);
  for (const call of fetcher.mock.calls) {
    const [url, init] = call as unknown as [string, RequestInit];
    expect(url).toBe(
      `/api/v1/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-preparation-periods/${scope.periodId}/revisions/1/evidence/${id}?staff_version=3&employment_epoch=2`,
    );
    expect(init.cache).toBe("no-store");
    expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
    expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  }
});
it.each([
  { hospital_id: scope.staffId },
  { staff_id: scope.hospitalId },
  { period_id: id },
  { period_revision_id: command },
  { period_version: 2 },
  { staff_version: 4 },
  { employment_epoch: 3 },
  { document_record_version: 4 },
  { version: 2 },
  { status: "withdrawn" },
  { source_generation: "9223372036854775808" },
  { governance_authority: { subject: scope.subject } },
  { size: 10485761 },
  { content_type: "text/html" },
])("rejects foreign, stale, inconsistent or private binding receipts", async (patch) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...binding, ...patch })),
  );
  await expect(getGovernanceEmploymentEvidence(scope, id)).rejects.toThrow();
});
it("rejects a mismatched command, author or selected scanned file", async () => {
  for (const patch of [
    { revision_id: id },
    { actor_subject: "different-subject" },
    { reason: "Different reason" },
    { document_id: id },
    { sha256: "a".repeat(64) },
    { scan_attempt: 2 },
  ]) {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json({ ...binding, ...patch })),
    );
    await expect(attachGovernanceEmploymentEvidence(scope, id, input, file)).rejects.toThrow();
  }
});
it("refuses mixed source, stale terms, unscanned files and unsupported command IDs before network", async () => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  await expect(
    getGovernanceEmploymentEvidence({ ...scope, hrIntake: true } as typeof scope, id),
  ).rejects.toThrow();
  await expect(
    attachGovernanceEmploymentEvidence({ ...scope, periodStaffVersion: 2 }, id, input, file),
  ).rejects.toThrow();
  await expect(
    attachGovernanceEmploymentEvidence(scope, id, input, { ...file, status: "scan_pending" }),
  ).rejects.toThrow();
  await expect(
    attachGovernanceEmploymentEvidence(scope, id, { ...input, command_id: scope.staffId }, file),
  ).rejects.toThrow();
  expect(fetcher).not.toHaveBeenCalled();
});
it("keeps exact historical source versions readable while refusing fresh attachment", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(binding)),
  );
  const latest = { ...scope, staffVersion: 4, employmentEpoch: 3 };
  expect(await getGovernanceEmploymentEvidence(latest, id)).toEqual(binding);
  await expect(attachGovernanceEmploymentEvidence(latest, id, input, file)).rejects.toThrow();
});
it("pages canonical binding and immutable revision history without ambiguous rows", async () => {
  const fetcher = vi.fn(async () => Response.json(page([binding])));
  vi.stubGlobal("fetch", fetcher);
  expect(await listGovernanceEmploymentEvidence(scope)).toEqual(page([binding]));
  expect(await listGovernanceEmploymentEvidenceHistory(scope, id)).toEqual(page([binding]));
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(page([binding, binding]))),
  );
  await expect(listGovernanceEmploymentEvidence(scope)).rejects.toThrow();
  await expect(listGovernanceEmploymentEvidenceHistory(scope, id)).rejects.toThrow();
});
it("withdraws with the original command and retains the exact frozen file generations", async () => {
  const commandId = "019a0000-0000-7000-8000-000000000031";
  const withdrawal = { command_id: commandId, expected_version: 1, reason: "Synthetic withdrawal" };
  const withdrawn = {
    ...binding,
    status: "withdrawn" as const,
    version: 2,
    revision_id: commandId,
    reason: withdrawal.reason,
    updated_at: "2026-11-01T00:04:00Z",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(withdrawn)),
  );
  expect(await withdrawGovernanceEmploymentEvidence(scope, binding, withdrawal)).toEqual(withdrawn);
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...withdrawn, released_generation: "9007199254740995" })),
  );
  await expect(withdrawGovernanceEmploymentEvidence(scope, binding, withdrawal)).rejects.toThrow();
});
it("verifies protected evidence bytes and refuses content for withdrawn bindings", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const fetcher = vi.fn(
    async () =>
      new Response("x", { headers: { "Content-Type": "image/png", "Content-Length": "1" } }),
  );
  vi.stubGlobal("fetch", fetcher);
  const result = await downloadGovernanceEmploymentEvidence(
    scope,
    binding,
    new AbortController().signal,
  );
  expect(result.size).toBe(1);
  expect(result.type).toBe("image/png");
  await expect(
    downloadGovernanceEmploymentEvidence(
      scope,
      { ...binding, status: "withdrawn", version: 2 },
      new AbortController().signal,
    ),
  ).rejects.toThrow();
  expect(fetcher).toHaveBeenCalledTimes(1);
});
it("honors cancellation and never falls back after the native source is denied", async () => {
  const fetcher = vi.fn(async () =>
    Response.json({ error: "forbidden", message: "Denied" }, { status: 403 }),
  );
  vi.stubGlobal("fetch", fetcher);
  await expect(getGovernanceEmploymentEvidence(scope, id)).rejects.toMatchObject({ status: 403 });
  const controller = new AbortController();
  controller.abort();
  await expect(getGovernanceEmploymentEvidence(scope, id, controller.signal)).rejects.toThrow();
  expect(fetcher).toHaveBeenCalledTimes(1);
});

it("rejects unsupported binding identifiers inside an otherwise owned page", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(page([{ ...binding, id: scope.staffId }]))),
  );
  await expect(listGovernanceEmploymentEvidence(scope)).rejects.toThrow();
});
