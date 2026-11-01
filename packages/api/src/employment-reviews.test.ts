import { afterEach, expect, it, vi } from "vitest";
import { createHash } from "node:crypto";
import { readAttachmentFile } from "./attachment-file";
import {
  getEmploymentReview,
  submitEmploymentReview,
  employmentReviewsKey,
  decideEmploymentReview,
  withdrawEmploymentReview,
  revokeEmploymentReview,
  listEmploymentReviewHistory,
  downloadEmploymentReviewEvidence,
} from "./employment-reviews";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
  subject: "synthetic-employment-preparer",
  hrIntake: true as const,
};
const id = "019a0000-0000-7000-8000-000000000001";
const command = "019a0000-0000-7000-8000-000000000002";
const period = "019a0000-0000-7000-8000-000000000003";
const revision = "019a0000-0000-7000-8000-000000000004";
const person = "019a0000-0000-7000-8000-000000000005";
const personRevision = "019a0000-0000-7000-8000-000000000006";
const bindingId = "019a0000-0000-7000-8000-000000000007";
const bindingRevision = "019a0000-0000-7000-8000-000000000008";
const file = "019a0000-0000-7000-8000-000000000009";
const at = "2026-10-31T00:00:00Z";
const input = {
  command_id: command,
  staff_version: 1,
  employment_epoch: 1,
  identity_hash: "a".repeat(64),
  period_id: period,
  period_revision_id: revision,
  period_version: 1,
  person_review_id: person,
  person_review_revision_id: personRevision,
  person_review_version: 2,
  evidence: [{ id: bindingId, revision_id: bindingRevision, version: 1 }],
  policy_reference: "SYN-APPOINTMENT-POLICY",
  reason: "Synthetic frozen appointment",
  terms_examined: true,
  known_associations_disclosed: true,
};
const terms = {
  id: period,
  revision_id: revision,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  version: 1,
  status: "draft",
  staff_version: 1,
  employment_epoch: 1,
  engagement_kind: "employee",
  duration: "ongoing",
  starts_at: at,
  contracting_party: "",
  sponsor_id: "",
  reason: "Synthetic appointment terms",
  evidence_reference: "SYN-APPOINTMENT",
  proposed_by_subject: scope.subject,
  actor_subject: scope.subject,
  created_at: at,
  updated_at: at,
};
const binding = {
  id: bindingId,
  revision_id: bindingRevision,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  period_id: period,
  period_revision_id: revision,
  period_version: 1,
  version: 1,
  status: "attached",
  staff_version: 1,
  employment_epoch: 1,
  document_id: file,
  document_record_version: 1,
  filename: "synthetic-appointment.png",
  content_type: "image/png",
  size: 88,
  sha256: "b".repeat(64),
  source_generation: "10",
  released_generation: "11",
  scan_attempt: 1,
  scanned_at: at,
  proposed_by_subject: scope.subject,
  actor_subject: scope.subject,
  reason: "Synthetic selected evidence",
  created_at: at,
  updated_at: at,
};
const { command_id, evidence, ...source } = input;
const row = {
  ...source,
  id,
  revision_id: command_id,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  version: 1,
  status: "pending",
  access_intent: "non_digital",
  declared_subject: "",
  proposed_by_subject: scope.subject,
  actor_subject: scope.subject,
  decision_reason: "",
  reviewer_terms_examined: false,
  independent_person_confirmed: false,
  terms,
  evidence: [{ binding, classification_version: 0, classification_revision_id: "" }],
  created_at: at,
  updated_at: at,
};
const capabilities = {
  can_approve: false,
  can_reject: false,
  can_withdraw: true,
  can_revoke: false,
};
afterEach(() => vi.unstubAllGlobals());

const specimenBytes = new Uint8Array([1, 2, 3]);
const downloadable = {
  review: {
    ...row,
    evidence: [
      {
        ...row.evidence[0],
        binding: {
          ...row.evidence[0].binding,
          size: specimenBytes.byteLength,
          sha256: createHash("sha256").update(specimenBytes).digest("hex"),
        },
      },
    ],
  },
  current_state: "pending" as const,
  ...capabilities,
};

it("downloads only selected employment-review bytes through the original scoped Global API workspace", async () => {
  const fetcher = vi.fn(
    async () =>
      new Response(specimenBytes, {
        headers: { "Content-Type": "image/png", "Content-Length": "3" },
      }),
  );
  vi.stubGlobal("fetch", fetcher);
  const controller = new AbortController();
  const blob = await downloadEmploymentReviewEvidence(
    scope,
    id,
    downloadable,
    file,
    controller.signal,
  );
  expect(new Uint8Array(await readAttachmentFile(blob, controller.signal))).toEqual(specimenBytes);
  const [path, request] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(path).toBe(
    `/api/v1/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-reviews/${id}/evidence/${file}/content?hr_intake=v1`,
  );
  expect(request.cache).toBe("no-store");
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

it.each([
  { headers: new Headers({ "Content-Type": "image/jpeg" }), body: specimenBytes },
  {
    headers: new Headers({ "Content-Type": "image/png", "Content-Length": "4" }),
    body: specimenBytes,
  },
  { headers: new Headers({ "Content-Type": "image/png" }), body: new Uint8Array([3, 2, 1]) },
])("refuses inconsistent employment bytes or headers %#", async ({ headers, body }) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(body, { headers })),
  );
  await expect(
    downloadEmploymentReviewEvidence(scope, id, downloadable, file, new AbortController().signal),
  ).rejects.toThrow(/evidence/i);
});

it("cancels an oversized employment stream before buffering an unbounded body", async () => {
  let cancelled = false;
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new Uint8Array([1, 2, 3, 4]));
    },
    cancel() {
      cancelled = true;
    },
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(body, { headers: { "Content-Type": "image/png" } })),
  );
  await expect(
    downloadEmploymentReviewEvidence(scope, id, downloadable, file, new AbortController().signal),
  ).rejects.toThrow(/evidence/i);
  expect(cancelled).toBe(true);
});

it.each([
  { ...downloadable, current_state: "source_changed" },
  { ...downloadable, review: { ...downloadable.review, hospital_id: scope.staffId } },
])("refuses unavailable or foreign employment view before requesting bytes %#", async (view) => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  await expect(
    downloadEmploymentReviewEvidence(scope, id, view, file, new AbortController().signal),
  ).rejects.toThrow();
  expect(fetcher).not.toHaveBeenCalled();
});

it("refuses an unselected employment file before requesting bytes", async () => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  await expect(
    downloadEmploymentReviewEvidence(
      scope,
      id,
      downloadable,
      command,
      new AbortController().signal,
    ),
  ).rejects.toThrow();
  expect(fetcher).not.toHaveBeenCalled();
});

it("discards cancelled employment byte responses", async () => {
  const controller = new AbortController();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      controller.abort();
      return new Response(specimenBytes, { headers: { "Content-Type": "image/png" } });
    }),
  );
  await expect(
    downloadEmploymentReviewEvidence(scope, id, downloadable, file, controller.signal),
  ).rejects.toThrow();
});

it("reads protected employment history with the original explicit workspace and cursor", async () => {
  const history = { data: [row], page: { has_more: false, next_cursor: null } };
  const fetcher = vi.fn(async () => Response.json(history));
  vi.stubGlobal("fetch", fetcher);
  expect(await listEmploymentReviewHistory(scope, id, "opaque-position")).toEqual(history);
  const [path, request] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(path).toBe(
    `/api/v1/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-reviews/${id}/history?hr_intake=v1&limit=50&cursor=opaque-position`,
  );
  expect(request.cache).toBe("no-store");
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
});

it("pins the original workspace and exact reviewed source to immutable submission", async () => {
  const fetcher = vi.fn(async () => Response.json(row));
  vi.stubGlobal("fetch", fetcher);
  expect(await submitEmploymentReview(scope, id, input)).toEqual(row);
  const [path, request] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(path).toBe(
    `/api/v1/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-reviews/${id}?hr_intake=v1`,
  );
  expect(request.method).toBe("PUT");
  expect(request.cache).toBe("no-store");
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(JSON.parse(String(request.body))).toEqual(input);
  expect(employmentReviewsKey(scope)).not.toEqual(
    employmentReviewsKey({ ...scope, subject: "other-actor" }),
  );
});

it.each([
  { hospital_id: scope.staffId },
  { staff_id: scope.hospitalId },
  { revision_id: id },
  { person_review_revision_id: id },
  { actor_subject: "other-actor" },
  { terms_examined: false },
  { identity_hash: "c".repeat(64) },
  { terms: { ...terms, staff_version: 2 } },
  { terms: { ...terms, revision_id: id } },
  { evidence: [{ ...row.evidence[0], binding: { ...binding, staff_id: scope.hospitalId } }] },
  { evidence: [{ ...row.evidence[0], binding: { ...binding, id } }] },
  { evidence: [{ ...row.evidence[0], classification_version: 1 }] },
  { evidence: [row.evidence[0], row.evidence[0]] },
])("refuses another scope, source or evidence receipt: %o", async (change) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...row, ...change })),
  );
  await expect(submitEmploymentReview(scope, id, input)).rejects.toThrow(/receipt/i);
});
it("validates required attestations and unique selections before dispatch", async () => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  await expect(
    submitEmploymentReview(scope, id, { ...input, terms_examined: false }),
  ).rejects.toThrow();
  await expect(
    submitEmploymentReview(scope, id, { ...input, evidence: [...evidence, ...evidence] }),
  ).rejects.toThrow();
  await expect(
    submitEmploymentReview({ ...scope, hrIntake: false } as unknown as typeof scope, id, input),
  ).rejects.toThrow();
  expect(fetcher).not.toHaveBeenCalled();
});
it("returns historical receipt with unavailable current evidence without implying activation", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({ review: row, current_state: "evidence_unavailable", ...capabilities }),
    ),
  );
  expect(await getEmploymentReview(scope, id)).toEqual({
    review: row,
    current_state: "evidence_unavailable",
    ...capabilities,
  });
});
it("rejects a foreign terms projection even in a current-state view", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({
        review: { ...row, terms: { ...terms, hospital_id: scope.staffId } },
        current_state: "pending",
        ...capabilities,
      }),
    ),
  );
  await expect(getEmploymentReview(scope, id)).rejects.toThrow(/receipt/i);
});
it("does not publish a late receipt after cancellation", async () => {
  const controller = new AbortController();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      controller.abort();
      return Response.json(row);
    }),
  );
  await expect(submitEmploymentReview(scope, id, input, controller.signal)).rejects.toThrow();
});

const reviewerScope = {
  tenantId: scope.tenantId,
  subject: "synthetic-employment-reviewer",
  hospitalId: scope.hospitalId,
  staffId: scope.staffId,
  employmentReview: true as const,
};
const decision = {
  command_id: id,
  expected_version: 1,
  decision: "approve" as const,
  reason: "Synthetic independent terms decision",
  terms_examined: true,
  independent_person_confirmed: true,
};
const approved = {
  ...row,
  revision_id: decision.command_id,
  version: 2,
  status: "approved",
  actor_subject: reviewerScope.subject,
  decision_reason: decision.reason,
  reviewer_terms_examined: true,
  independent_person_confirmed: true,
};

it("rejects history that changes the frozen employment source across receipts", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({
        data: [approved, { ...row, identity_hash: "c".repeat(64) }],
        page: { has_more: false, next_cursor: null },
      }),
    ),
  );
  await expect(listEmploymentReviewHistory(reviewerScope, id)).rejects.toThrow(/history/i);
});

it.each([
  { data: [row, row], page: { has_more: false, next_cursor: null } },
  { data: [row, approved], page: { has_more: false, next_cursor: null } },
  { data: [{ ...row, hospital_id: scope.staffId }], page: { has_more: false, next_cursor: null } },
  { data: [{ ...row, staff_id: scope.hospitalId }], page: { has_more: false, next_cursor: null } },
  { data: [{ ...row, id: period }], page: { has_more: false, next_cursor: null } },
  { data: [row], page: { has_more: true, next_cursor: null } },
  { data: [row], page: { has_more: false, next_cursor: "unexpected" } },
])("refuses inconsistent or foreign employment history %#", async (response) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(response)),
  );
  await expect(listEmploymentReviewHistory(reviewerScope, id)).rejects.toThrow(/history/i);
});

it("discards employment history when its request is cancelled", async () => {
  const controller = new AbortController();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      controller.abort();
      return Response.json({ data: [row], page: { has_more: false, next_cursor: null } });
    }),
  );
  await expect(listEmploymentReviewHistory(scope, id, null, controller.signal)).rejects.toThrow();
});
it("uses separate reviewer authority and verifies the immutable decision receipt", async () => {
  const fetcher = vi.fn(async () => Response.json(approved));
  vi.stubGlobal("fetch", fetcher);
  expect(await decideEmploymentReview(reviewerScope, id, decision)).toEqual(approved);
  const [path, request] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(path).toBe(
    `/api/v1/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-reviews/${id}/decision?employment_review=v1`,
  );
  expect(request.method).toBe("POST");
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Subject")).toBe(
    reviewerScope.subject,
  );
  expect(employmentReviewsKey(reviewerScope)).not.toEqual(employmentReviewsKey(scope));
});
it.each([
  { version: 3 },
  { revision_id: command },
  { status: "rejected" },
  { actor_subject: scope.subject },
  { decision_reason: "Other intent" },
  { reviewer_terms_examined: false },
  { independent_person_confirmed: false },
])("rejects an inconsistent reviewer receipt: %o", async (change) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...approved, ...change })),
  );
  await expect(decideEmploymentReview(reviewerScope, id, decision)).rejects.toThrow(/receipt/i);
});
it("requires the appropriate explicit command mode and attestations before dispatch", async () => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  await expect(
    decideEmploymentReview(scope as unknown as typeof reviewerScope, id, decision),
  ).rejects.toThrow();
  await expect(
    decideEmploymentReview(reviewerScope, id, { ...decision, terms_examined: false }),
  ).rejects.toThrow();
  await expect(
    withdrawEmploymentReview(reviewerScope as unknown as typeof scope, id, {
      command_id: id,
      expected_version: 1,
      reason: "Synthetic withdrawal",
    }),
  ).rejects.toThrow();
  await expect(
    submitEmploymentReview(reviewerScope as unknown as typeof scope, id, input),
  ).rejects.toThrow();
  expect(fetcher).not.toHaveBeenCalled();
});
it("reads an approved historical terms record with owner-computed reviewer capabilities", async () => {
  const view = {
    review: approved,
    current_state: "approved",
    can_approve: false,
    can_reject: false,
    can_withdraw: false,
    can_revoke: true,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(view)),
  );
  expect(await getEmploymentReview(reviewerScope, id)).toEqual(view);
});
it.each([
  { current_state: "pending" },
  { can_withdraw: true },
  { can_approve: true },
  { can_reject: true },
])("refuses inconsistent current state or command capabilities: %o", async (change) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({
        review: approved,
        current_state: "approved",
        can_approve: false,
        can_reject: false,
        can_withdraw: false,
        can_revoke: true,
        ...change,
      }),
    ),
  );
  await expect(getEmploymentReview(reviewerScope, id)).rejects.toThrow();
});
it("withdraws and revokes with distinct authority without borrowing earlier attestations", async () => {
  const action = { command_id: id, expected_version: 1, reason: "Synthetic ending" };
  const withdrawn = {
    ...row,
    status: "withdrawn",
    revision_id: id,
    version: 2,
    decision_reason: action.reason,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(withdrawn)),
  );
  expect(await withdrawEmploymentReview(scope, id, action)).toEqual(withdrawn);
  const revoked = {
    ...approved,
    status: "revoked",
    revision_id: command,
    version: 3,
    decision_reason: action.reason,
    reviewer_terms_examined: false,
    independent_person_confirmed: false,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(revoked)),
  );
  expect(
    await revokeEmploymentReview(reviewerScope, id, {
      ...action,
      command_id: command,
      expected_version: 2,
    }),
  ).toEqual(revoked);
});
