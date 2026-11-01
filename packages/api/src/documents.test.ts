import { afterEach, describe, expect, it, vi } from "vitest";
import { webcrypto } from "node:crypto";
import { documentQueryKey } from "./document-queries";
import {
  prepareDocumentUpload,
  submitDocumentUpload,
  retryDocumentScan,
  revokeStaffDocument,
  downloadStaffDocument,
} from "./documents";
import {
  documentInputSchema,
  MAX_DOCUMENT_BYTES,
  documentRecordChoiceSchema,
  type DocumentInput,
  type StaffDocument,
} from "./documents-fields";

const record = "11111111-1111-4111-8111-111111111111";
const input: DocumentInput = {
  record_kind: "enrollment",
  record_id: record,
  category: "identity_evidence",
  filename: "synthetic-evidence.png",
  content_type: "image/png",
  size: 1,
  sha256: "a".repeat(64),
};
const scope = {
  subject: "synthetic-document-account",
  tenantId: "33333333-3333-4333-8333-333333333333",
  hospitalId: "44444444-4444-4444-8444-444444444444",
  staffId: record,
};
const fileID = "55555555-5555-4555-8555-555555555555";
const receipt: StaffDocument = {
  ...input,
  id: fileID,
  hospital_id: scope.hospitalId,
  staff_id: record,
  record_version: 1,
  status: "upload_pending",
  scan_attempt: 1,
  uploaded_at: null,
  scanned_at: null,
  failure_code: null,
  created_at: "2026-10-15T00:00:00Z",
  updated_at: "2026-10-15T00:00:00Z",
  expires_at: "2026-10-15T00:30:00Z",
};
afterEach(() => vi.unstubAllGlobals());

describe("staff document contract", () => {
  it("accepts a bounded immutable intent and rejects mismatched purposes and unsafe filenames", () => {
    expect(documentInputSchema.parse(input)).toEqual(input);
    expect(MAX_DOCUMENT_BYTES).toBe(10 * 1024 * 1024);
    for (const patch of [
      { size: 0 },
      { size: MAX_DOCUMENT_BYTES + 1 },
      { record_id: "00000000-0000-0000-0000-000000000000" },
      { category: "qualification_evidence" },
      { content_type: "text/html" },
      { filename: "../evidence.png" },
      { filename: "evidence\n.png" },
      { sha256: "A".repeat(64) },
      { verified: true },
    ]) {
      expect(documentInputSchema.safeParse({ ...input, ...patch }).success).toBe(false);
    }
    expect(
      documentInputSchema.safeParse({ ...input, filename: "🩺".repeat(176) + ".png" }).success,
    ).toBe(true);
  });
  it("does not let a choice offer purposes belonging to another record kind", () => {
    expect(
      documentRecordChoiceSchema.safeParse({
        record_kind: "credential",
        record_id: record,
        label: "Synthetic registration",
        categories: ["credential_evidence"],
      }).success,
    ).toBe(true);
    expect(
      documentRecordChoiceSchema.safeParse({
        record_kind: "credential",
        record_id: record,
        label: "Synthetic registration",
        categories: ["identity_evidence"],
      }).success,
    ).toBe(false);
  });
});

describe("private document requests", () => {
  it("separates private document caches for two accounts in the same tenant", () => {
    expect(documentQueryKey(scope)).not.toEqual(
      documentQueryKey({ ...scope, subject: "synthetic-other-account" }),
    );
  });

  it("reuses the explicit intent after a network error and pins every request to its tenant", async () => {
    vi.stubGlobal("crypto", webcrypto);
    const data = new TextEncoder().encode("synthetic image bytes");
    const file = {
      name: input.filename,
      type: input.content_type,
      size: data.byteLength,
      arrayBuffer: async () => data.buffer,
    } as File;
    const controller = new AbortController();
    const prepared = await prepareDocumentUpload(
      fileID,
      file,
      {
        record_kind: "enrollment",
        record_id: record,
        label: "Staff enrollment",
        categories: ["identity_evidence"],
      },
      "identity_evidence",
      controller.signal,
    );
    let contentCalls = 0;
    const requests: { url: string; init: RequestInit }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init: RequestInit) => {
        requests.push({ url, init });
        if (url.endsWith("/content")) {
          if (contentCalls++ === 0) throw new TypeError("Synthetic lost response");
          return new Response(
            JSON.stringify({ ...receipt, ...prepared.input, status: "scan_pending" }),
            { status: 202 },
          );
        }
        return new Response(JSON.stringify({ ...receipt, ...prepared.input }), { status: 201 });
      }),
    );
    await expect(submitDocumentUpload(scope, prepared, controller.signal)).rejects.toThrow(
      "Synthetic lost response",
    );
    expect((await submitDocumentUpload(scope, prepared, controller.signal)).status).toBe(
      "scan_pending",
    );
    expect(requests).toHaveLength(4);
    expect(requests[0].url).toBe(requests[2].url);
    expect(requests[0].init.body).toBe(requests[2].init.body);
    for (const request of requests)
      expect(new Headers(request.init.headers).get("X-Helivanta-Expected-Tenant")).toBe(
        scope.tenantId,
      );
    for (const request of requests)
      expect(new Headers(request.init.headers).get("X-Helivanta-Expected-Subject")).toBe(
        scope.subject,
      );
    expect(new Headers(requests[1].init.headers).get("Content-Type")).toBe("image/png");
    expect(requests[1].init.body).toBe(file);
  });
  it("does not submit an aborted file operation", async () => {
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);
    const controller = new AbortController();
    controller.abort();
    await expect(
      submitDocumentUpload(scope, { id: fileID, input, file: {} as File }, controller.signal),
    ).rejects.toMatchObject({ name: "AbortError" });
    expect(fetcher).not.toHaveBeenCalled();
  });
  it("reuses a caller-chosen scan attempt and refuses a non-available download", async () => {
    const fetcher = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(
      async () =>
        new Response(JSON.stringify({ ...receipt, status: "scan_pending", scan_attempt: 2 })),
    );
    vi.stubGlobal("fetch", fetcher);
    const controller = new AbortController();
    await retryDocumentScan(scope, fileID, 2, controller.signal);
    await retryDocumentScan(scope, fileID, 2, controller.signal);
    expect(fetcher.mock.calls[0][0]).toBe(fetcher.mock.calls[1][0]);
    expect(fetcher.mock.calls[0][0]).toContain("/retries/2");
    await expect(
      downloadStaffDocument(scope, { ...receipt, status: "scan_pending" }, controller.signal),
    ).rejects.toThrow("not available");
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("downloads only a ready file from this hospital and rejects incomplete bytes", async () => {
    const fetcher = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(
      async () => new Response(new Uint8Array([1]), { headers: { "Content-Type": "image/png" } }),
    );
    vi.stubGlobal("fetch", fetcher);
    const controller = new AbortController();
    const ready = { ...receipt, status: "available" as const };
    expect((await downloadStaffDocument(scope, ready, controller.signal)).size).toBe(1);
    expect(new Headers(fetcher.mock.calls[0][1]?.headers).get("X-Helivanta-Expected-Tenant")).toBe(
      scope.tenantId,
    );
    await expect(
      downloadStaffDocument(
        scope,
        { ...ready, hospital_id: "66666666-6666-4666-8666-666666666666" },
        controller.signal,
      ),
    ).rejects.toThrow("ownership");
    expect(fetcher).toHaveBeenCalledTimes(1);
    await expect(
      downloadStaffDocument(scope, { ...ready, size: 2 }, controller.signal),
    ).rejects.toThrow("could not be verified");
  });

  it("does not retain prepared bytes when cancellation arrives during file reading", async () => {
    vi.stubGlobal("crypto", webcrypto);
    const controller = new AbortController();
    let finishRead!: (value: ArrayBuffer) => void;
    const file = {
      name: input.filename,
      type: input.content_type,
      size: 1,
      arrayBuffer: () =>
        new Promise<ArrayBuffer>((resolve) => {
          finishRead = resolve;
        }),
    } as File;
    const prepared = prepareDocumentUpload(
      fileID,
      file,
      {
        record_kind: "enrollment",
        record_id: record,
        label: "Staff enrollment",
        categories: ["identity_evidence"],
      },
      "identity_evidence",
      controller.signal,
    );
    controller.abort();
    finishRead(new Uint8Array([1]).buffer);
    await expect(prepared).rejects.toMatchObject({ name: "AbortError" });
  });
});

it("keeps scoped intake on every document command and byte request", async () => {
  const requests: Array<{ url: string; init: RequestInit }> = [];
  const intake = { ...scope, hrIntake: true };
  const signal = new AbortController().signal;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      requests.push({ url, init });
      const path = new URL(url, "https://example.invalid").pathname;
      if (path.endsWith("/content") && init.method !== "PUT")
        return new Response(new Uint8Array([1]), { headers: { "Content-Type": "image/png" } });
      return Response.json({
        ...receipt,
        status: path.endsWith("/revocation")
          ? "revoked"
          : path.endsWith("/content") || path.includes("/retries/")
            ? "scan_pending"
            : "upload_pending",
        scan_attempt: path.includes("/retries/") ? 2 : 1,
      });
    }),
  );
  await submitDocumentUpload(
    intake,
    {
      id: fileID,
      input,
      file: new File([new Uint8Array([1])], input.filename, { type: input.content_type }),
    },
    signal,
  );
  await retryDocumentScan(intake, fileID, 2, signal);
  await revokeStaffDocument(intake, fileID, signal);
  await downloadStaffDocument(intake, { ...receipt, status: "available" }, signal);
  expect(requests).toHaveLength(5);
  for (const { url, init } of requests) {
    expect(new URL(url, "https://example.invalid").searchParams.get("hr_intake")).toBe("v1");
    expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
    expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  }
  expect(new URL(requests[1]!.url, "https://example.invalid").pathname).toMatch(/\/content$/);
  expect(documentQueryKey(intake)).not.toEqual(documentQueryKey(scope));
});
