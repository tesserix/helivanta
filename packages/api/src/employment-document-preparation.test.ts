import type { StaffDocument } from "./documents-fields";
import { webcrypto } from "node:crypto";
import {
  prepareGovernanceEmploymentDocument,
  submitGovernanceEmploymentDocument,
  revokeGovernanceEmploymentDocument,
  retryGovernanceEmploymentDocument,
  downloadGovernanceEmploymentDocument,
  getGovernanceEmploymentDocument,
  listGovernanceEmploymentDocuments,
  registerGovernanceEmploymentDocument,
} from "./employment-document-preparation";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-employment-preparer",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
  staffVersion: 3,
  employmentEpoch: 2,
};
const id = "019a0000-0000-7000-8000-000000000021";
const input = {
  record_kind: "enrollment" as const,
  record_id: scope.staffId,
  category: "employment_evidence" as const,
  filename: "synthetic-appointment.png",
  content_type: "image/png" as const,
  size: 1,
  sha256: "a".repeat(64),
};
const file: StaffDocument = {
  ...input,
  id,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  record_version: 3,
  status: "upload_pending",
  scan_attempt: 1,
  uploaded_at: null,
  scanned_at: null,
  failure_code: null,
  created_at: "2026-11-01T00:00:00Z",
  updated_at: "2026-11-01T00:00:00Z",
  expires_at: "2026-11-01T00:30:00Z",
};
afterEach(() => vi.unstubAllGlobals());
it("uses only the native employment-document family with original workspace and source basis", async () => {
  const fetcher = vi.fn(async () => Response.json(file));
  vi.stubGlobal("fetch", fetcher);
  expect(await registerGovernanceEmploymentDocument(scope, id, input)).toEqual(file);
  expect(await getGovernanceEmploymentDocument(scope, id)).toEqual(file);
  for (const call of fetcher.mock.calls) {
    const [url, init] = call as unknown as [string, RequestInit];
    expect(url).toBe(
      `/api/v1/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-preparation-documents/${id}?staff_version=3&employment_epoch=2`,
    );
    expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
    expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
    expect(init.cache).toBe("no-store");
  }
});

it.each([
  { patch: { hospital_id: scope.staffId } },
  { patch: { staff_id: scope.hospitalId } },
  { patch: { record_id: scope.hospitalId } },
  { patch: { record_version: 4 } },
  { patch: { category: "identity_evidence" } },
  { patch: { id: "019a0000-0000-7000-8000-000000000022" } },
  { patch: { governance_authority: { actor_subject: scope.subject } } },
  { patch: { source_generation: 42 } },
])("rejects a foreign, stale, mismatched or private document receipt", async ({ patch }) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...file, ...patch })),
  );
  await expect(getGovernanceEmploymentDocument(scope, id)).rejects.toThrow();
});
it("refuses mixed authority scope or document purposes before network", async () => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  await expect(
    getGovernanceEmploymentDocument({ ...scope, hrIntake: true } as typeof scope, id),
  ).rejects.toThrow();
  await expect(
    registerGovernanceEmploymentDocument(scope, id, { ...input, category: "identity_evidence" }),
  ).rejects.toThrow();
  expect(fetcher).not.toHaveBeenCalled();
});

const page = (data: unknown[], more = false) => ({
  data,
  page: { has_more: more, next_cursor: more ? "sealed-next" : null },
});
it("pages minimal document receipts with sealed continuation", async () => {
  const fetcher = vi.fn(async () => Response.json(page([file])));
  vi.stubGlobal("fetch", fetcher);
  expect(await listGovernanceEmploymentDocuments(scope, "sealed-next")).toEqual(page([file]));
  expect(String((fetcher.mock.calls[0] as unknown as [string])[0])).toContain(
    "limit=20&cursor=sealed-next",
  );
});
it.each([
  { rows: [file, { ...file, id: file.id.toUpperCase() }] },
  { rows: Array.from({ length: 21 }, () => file) },
])("rejects ambiguous or oversized document pages", async ({ rows }) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(page(rows))),
  );
  await expect(listGovernanceEmploymentDocuments(scope)).rejects.toThrow();
});

it("prepares canonical employment bytes and preserves the file identifier through submit", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const controller = new AbortController();
  const selected = new File(["x"], input.filename, { type: input.content_type });
  const prepared = await prepareGovernanceEmploymentDocument(
    scope,
    id,
    selected,
    controller.signal,
  );
  expect(prepared.input).toMatchObject({
    record_id: scope.staffId,
    record_kind: "enrollment",
    category: "employment_evidence",
    sha256: "2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881",
  });
  const fetcher = vi.fn(async (url: RequestInfo | URL) =>
    Response.json({
      ...file,
      ...prepared.input,
      ...(String(url).includes("/content?")
        ? {
            status: "scan_pending",
            uploaded_at: "2026-11-01T00:01:00Z",
            updated_at: "2026-11-01T00:01:00Z",
          }
        : {}),
    }),
  );
  vi.stubGlobal("fetch", fetcher);
  expect(
    (await submitGovernanceEmploymentDocument(scope, prepared, controller.signal)).status,
  ).toBe("scan_pending");
  expect(fetcher).toHaveBeenCalledTimes(2);
  const second = (fetcher.mock.calls[1] as unknown as [string, RequestInit])[1];
  expect(second.body).toBe(selected);
  expect(new Headers(second.headers).get("Content-Type")).toBe(input.content_type);
});
it("does not upload bytes or fall back when registration is refused", async () => {
  const fetcher = vi.fn(async () =>
    Response.json({ error: "forbidden", message: "Denied" }, { status: 403 }),
  );
  vi.stubGlobal("fetch", fetcher);
  await expect(
    submitGovernanceEmploymentDocument(
      scope,
      { id, input, file: new File(["x"], input.filename, { type: input.content_type }) },
      new AbortController().signal,
    ),
  ).rejects.toThrow();
  expect(fetcher).toHaveBeenCalledTimes(1);
});

it("recovers the next scan attempt and preserves canonical revocation receipts", async () => {
  const uploaded = {
    ...file,
    uploaded_at: "2026-11-01T00:01:00Z",
    updated_at: "2026-11-01T00:01:00Z",
    status: "scan_error" as const,
    failure_code: "scanner_unavailable" as const,
  };
  const fetcher = vi.fn(async (url: RequestInfo | URL) =>
    Response.json({
      ...uploaded,
      scan_attempt: String(url).includes("/retries/") ? 2 : 1,
      status: String(url).includes("/retries/") ? "scan_pending" : "revoked",
      scanned_at: String(url).includes("/retries/") ? null : "2026-11-01T00:02:00Z",
      failure_code: null,
    }),
  );
  vi.stubGlobal("fetch", fetcher);
  expect((await retryGovernanceEmploymentDocument(scope, uploaded)).scan_attempt).toBe(2);
  expect(String(fetcher.mock.calls[0][0])).toContain(
    `/retries/2?staff_version=3&employment_epoch=2`,
  );
  const available = {
    ...uploaded,
    status: "available" as const,
    failure_code: null,
    scanned_at: "2026-11-01T00:02:00Z",
  };
  expect((await revokeGovernanceEmploymentDocument(scope, available)).status).toBe("revoked");
});
it("refuses an unsafe scan attempt or an unscanned revoke before network", async () => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  await expect(
    retryGovernanceEmploymentDocument(scope, {
      ...file,
      status: "scan_error",
      uploaded_at: "2026-11-01T00:01:00Z",
      scan_attempt: Number.MAX_SAFE_INTEGER,
    }),
  ).rejects.toThrow();
  await expect(revokeGovernanceEmploymentDocument(scope, file)).rejects.toThrow();
  expect(fetcher).not.toHaveBeenCalled();
});

it("verifies released bytes against MIME, size and checksum", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const available: StaffDocument = {
    ...file,
    status: "available",
    uploaded_at: "2026-11-01T00:01:00Z",
    scanned_at: "2026-11-01T00:02:00Z",
    sha256: "2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response("x", { headers: { "Content-Type": "image/png", "Content-Length": "1" } }),
    ),
  );
  const blob = await downloadGovernanceEmploymentDocument(
    scope,
    available,
    new AbortController().signal,
  );
  expect(blob.size).toBe(1);
  expect(blob.type).toBe("image/png");
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response("y", { headers: { "Content-Type": "image/png", "Content-Length": "1" } }),
    ),
  );
  await expect(
    downloadGovernanceEmploymentDocument(scope, available, new AbortController().signal),
  ).rejects.toThrow("could not be verified");
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response("xx", { headers: { "Content-Type": "image/png", "Content-Length": "1" } }),
    ),
  );
  await expect(
    downloadGovernanceEmploymentDocument(scope, available, new AbortController().signal),
  ).rejects.toThrow("could not be verified");
});
it("honors cancellation before network and after metadata before binary upload", async () => {
  const before = new AbortController();
  before.abort();
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  await expect(getGovernanceEmploymentDocument(scope, id, before.signal)).rejects.toThrow();
  expect(fetcher).not.toHaveBeenCalled();
  const controller = new AbortController();
  const responder = vi.fn(async () => {
    controller.abort();
    return Response.json(file);
  });
  vi.stubGlobal("fetch", responder);
  await expect(
    submitGovernanceEmploymentDocument(
      scope,
      { id, input, file: new File(["x"], input.filename, { type: input.content_type }) },
      controller.signal,
    ),
  ).rejects.toThrow();
  expect(responder).toHaveBeenCalledTimes(1);
});

it("rejects a scan verdict dated before upload", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({
        ...file,
        status: "available",
        uploaded_at: "2026-11-01T00:02:00Z",
        scanned_at: "2026-11-01T00:01:00Z",
        updated_at: "2026-11-01T00:03:00Z",
      }),
    ),
  );
  await expect(getGovernanceEmploymentDocument(scope, id)).rejects.toThrow("could not be verified");
});
