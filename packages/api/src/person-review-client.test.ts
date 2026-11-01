import { afterEach, expect, it, vi } from "vitest";
import { createHash } from "node:crypto";
import { readAttachmentFile } from "./attachment-file";
import {
  submitPersonReview,
  decidePersonReview,
  getPersonReview,
  listPersonReviewHistory,
  revokePersonReview,
  downloadPersonReviewEvidence,
} from "./person-reviews";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
  subject: "synthetic-person-manager",
};
const id = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const command = "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb";
const file = "019ccccc-cccc-7ccc-8ccc-cccccccccccc";
const input = {
  command_id: command,
  staff_version: 1,
  employment_epoch: 1,
  identity_hash: "a".repeat(64),
  policy_reference: "SYN-HUMAN-IDENTITY-POLICY",
  finding_reference: "Synthetic human finding",
  observed_on: "2026-10-31",
  valid_until: "2027-01-01T00:00:00Z",
  access_intent: "non_digital" as const,
  declared_subject: "",
  identity_examined: true,
  known_associations_disclosed: true,
  document_ids: [file],
};
const { command_id, document_ids, ...source } = input;
const row = {
  ...source,
  id,
  revision_id: command_id,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  version: 1,
  status: "pending",
  proposed_by_subject: scope.subject,
  actor_subject: scope.subject,
  decision_reason: "",
  reviewer_identity_examined: false,
  independent_person_confirmed: false,
  created_at: "2026-10-31T00:00:00Z",
  updated_at: "2026-10-31T00:00:00Z",
  evidence: [
    {
      document_id: document_ids[0],
      record_version: 1,
      filename: "synthetic-identity.png",
      content_type: "image/png",
      size: 88,
      sha256: "b".repeat(64),
      source_generation: 1,
      released_generation: 2,
      scan_attempt: 1,
      scanned_at: "2026-10-31T00:00:00Z",
      engine_version: "1.5.1",
      definition_version: 28000,
      definitions_updated_at: "2026-10-30T00:00:00Z",
      classification_version: 0,
      classification_revision_id: "",
    },
  ],
};
afterEach(() => vi.unstubAllGlobals());

it("pins the original workspace, source and evidence to the submission receipt", async () => {
  const fetcher = vi.fn(async () => Response.json(row));
  vi.stubGlobal("fetch", fetcher);
  expect(await submitPersonReview(scope, id, input)).toEqual(row);
  const [path, request] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(path).toBe(
    `/api/v1/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/person-reviews/${id}`,
  );
  expect(request.method).toBe("PUT");
  expect(request.cache).toBe("no-store");
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(JSON.parse(String(request.body))).toEqual(input);
});

it.each([
  { hospital_id: "44444444-4444-4444-8444-444444444444" },
  { staff_id: "44444444-4444-4444-8444-444444444444" },
  { revision_id: id },
  { actor_subject: "different-person" },
  { identity_hash: "c".repeat(64) },
  { evidence: [{ ...row.evidence[0], document_id: id }] },
])("refuses a submission receipt from another scope or intent: %o", async (change) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...row, ...change })),
  );
  await expect(submitPersonReview(scope, id, input)).rejects.toThrow(/receipt/i);
});

it("retries an uncertain submission with the same command and original context", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      Response.json({ error: "unavailable", message: "Unavailable" }, { status: 502 }),
    )
    .mockResolvedValueOnce(Response.json(row));
  vi.stubGlobal("fetch", fetcher);
  await expect(submitPersonReview(scope, id, input)).rejects.toThrow();
  await expect(submitPersonReview(scope, id, input)).resolves.toEqual(row);
  const first = fetcher.mock.calls[0] as [string, RequestInit];
  const second = fetcher.mock.calls[1] as [string, RequestInit];
  expect(first[0]).toBe(second[0]);
  expect(first[1].body).toBe(second[1].body);
  expect(new Headers(first[1].headers)).toEqual(new Headers(second[1].headers));
});

it("keeps original approval separate from current source effectiveness", async () => {
  const approved = {
    ...row,
    status: "approved",
    version: 2,
    actor_subject: "synthetic-person-reviewer",
    reviewer_identity_examined: true,
    independent_person_confirmed: true,
    decision_reason: "Independent human finding",
  };
  const view = { review: approved, current_state: "source_changed" };
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(view)),
  );
  expect(await getPersonReview(scope, id)).toEqual(view);
});

it("does not turn a revoked receipt into current approved authority", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({ review: { ...row, status: "revoked" }, current_state: "approved" }),
    ),
  );
  await expect(getPersonReview(scope, id)).rejects.toThrow(/current state/i);
});

it("does not accept an approved label without the recorded independent findings", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({ review: { ...row, status: "approved" }, current_state: "approved" }),
    ),
  );
  await expect(getPersonReview(scope, id)).rejects.toThrow(/receipt/i);
});

it("requires independent examination before sending an approval", async () => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  await expect(
    decidePersonReview(scope, id, {
      command_id: command,
      expected_version: 1,
      decision: "approve",
      reason: "Synthetic decision",
      identity_examined: false,
      independent_person_confirmed: true,
    }),
  ).rejects.toThrow(/examination/i);
  expect(fetcher).not.toHaveBeenCalled();
});

it("validates the immutable revocation receipt without manufacturing a new source", async () => {
  const revoked = {
    ...row,
    status: "revoked",
    version: 3,
    decision_reason: "Synthetic revocation",
  };
  const fetcher = vi.fn(async () => Response.json(revoked));
  vi.stubGlobal("fetch", fetcher);
  expect(
    await revokePersonReview(scope, id, {
      command_id: command,
      expected_version: 2,
      reason: "Synthetic revocation",
    }),
  ).toEqual(revoked);
  expect((fetcher.mock.calls[0] as unknown as [string])[0]).toContain(`/${id}/revocation`);
});

const identityBytes = new Uint8Array([1, 2, 3]);
const downloadable = {
  review: {
    ...row,
    status: "pending" as const,
    access_intent: "non_digital" as const,
    evidence: [
      {
        ...row.evidence[0],
        content_type: "image/png" as const,
        size: identityBytes.byteLength,
        sha256: createHash("sha256").update(identityBytes).digest("hex"),
      },
    ],
  },
  current_state: "pending" as const,
};

it("downloads only the selected review's verified bytes through Global API", async () => {
  const fetcher = vi.fn(
    async () =>
      new Response(identityBytes, {
        headers: { "Content-Type": "image/png", "Content-Length": "3" },
      }),
  );
  vi.stubGlobal("fetch", fetcher);
  const blob = await downloadPersonReviewEvidence(
    scope,
    id,
    downloadable,
    file,
    new AbortController().signal,
  );
  expect(blob.type).toBe("image/png");
  expect(new Uint8Array(await readAttachmentFile(blob, new AbortController().signal))).toEqual(
    identityBytes,
  );
  const [path, request] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(path).toBe(
    `/api/v1/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/person-reviews/${id}/evidence/${file}/content`,
  );
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(request.cache).toBe("no-store");
});

it.each(["hash", "type", "size"])("refuses an evidence response with wrong %s", async (fault) => {
  const bytes = fault === "hash" ? new Uint8Array([3, 2, 1]) : identityBytes;
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(bytes, {
          headers: {
            "Content-Type": fault === "type" ? "application/json" : "image/png",
            "Content-Length": fault === "size" ? "4" : "3",
          },
        }),
    ),
  );
  await expect(
    downloadPersonReviewEvidence(scope, id, downloadable, file, new AbortController().signal),
  ).rejects.toThrow(/evidence/i);
});

it("refuses stale review state and unbound files before fetching", async () => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  await expect(
    downloadPersonReviewEvidence(
      scope,
      id,
      { ...downloadable, current_state: "source_changed" },
      file,
      new AbortController().signal,
    ),
  ).rejects.toThrow(/evidence/i);
  await expect(
    downloadPersonReviewEvidence(scope, id, downloadable, command, new AbortController().signal),
  ).rejects.toThrow(/evidence/i);
  expect(fetcher).not.toHaveBeenCalled();
});

it("does not publish bytes after workspace cancellation during the response", async () => {
  const controller = new AbortController();
  const body = new ReadableStream<Uint8Array>(
    {
      pull(stream) {
        stream.enqueue(identityBytes);
        stream.close();
        controller.abort();
      },
    },
    { highWaterMark: 0 },
  );
  const response = new Response(body, { headers: { "Content-Type": "image/png" } });
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => response),
  );
  await expect(
    downloadPersonReviewEvidence(scope, id, downloadable, file, controller.signal),
  ).rejects.toHaveProperty("name", "AbortError");
});

it("reads ordered historical receipts with original scope and opaque continuation", async () => {
  const approved = {
    ...row,
    status: "approved",
    version: 2,
    revision_id: "019ddddd-dddd-7ddd-8ddd-dddddddddddd",
    actor_subject: "synthetic-independent-reviewer",
    reviewer_identity_examined: true,
    independent_person_confirmed: true,
    decision_reason: "Synthetic historical approval",
    updated_at: "2026-10-31T01:00:00Z",
  };
  const first = { data: [approved], page: { has_more: true, next_cursor: "opaque-history-next" } };
  const last = { data: [row], page: { has_more: false, next_cursor: null } };
  const fetcher = vi
    .fn<typeof fetch>()
    .mockResolvedValueOnce(Response.json(first))
    .mockResolvedValueOnce(Response.json(last));
  vi.stubGlobal("fetch", fetcher);
  expect(await listPersonReviewHistory(scope, id)).toEqual(first);
  expect(await listPersonReviewHistory(scope, id, "opaque-history-next")).toEqual(last);
  expect(fetcher.mock.calls[1][0]).toBe(
    `/api/v1/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/person-reviews/${id}/history?limit=50&cursor=opaque-history-next`,
  );
  const options = fetcher.mock.calls[0][1];
  expect(options?.cache).toBe("no-store");
  expect(new Headers(options?.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

it.each([
  { rows: [{ ...row, id: "019ddddd-dddd-7ddd-8ddd-dddddddddddd" }] },
  { rows: [{ ...row, staff_id: "44444444-4444-4444-8444-444444444444" }] },
  { rows: [{ ...row, private_finding_key: "unrequested-private-value" }] },
  { rows: [row, row] },
  { rows: [row, { ...row, version: 2, revision_id: "019ddddd-dddd-7ddd-8ddd-dddddddddddd" }] },
])(
  "rejects unbound, duplicate, out-of-order or noncanonical historical receipts",
  async ({ rows }) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({ data: rows, page: { has_more: false, next_cursor: null } }),
      ),
    );
    await expect(listPersonReviewHistory(scope, id)).rejects.toThrow(
      /history could not be verified/i,
    );
  },
);

it("does not publish history after response-time cancellation", async () => {
  const controller = new AbortController();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      controller.abort();
      return Response.json({ data: [row], page: { has_more: false, next_cursor: null } });
    }),
  );
  await expect(listPersonReviewHistory(scope, id, null, controller.signal)).rejects.toThrow();
});
