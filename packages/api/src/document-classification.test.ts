import { afterEach, describe, expect, it, vi } from "vitest";
import {
  readDocumentClassification,
  saveDocumentClassification,
  readDocumentClassificationHistory,
} from "./document-classification";
import {
  documentClassificationInputSchema,
  documentClassificationSchema,
} from "./documents-fields";

const scope = {
  subject: "synthetic-document-account",
  tenantId: "11111111-1111-4111-8111-111111111111",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
};
const file = "44444444-4444-4444-8444-444444444444";
const reference = "55555555-5555-4555-8555-555555555555";
const saved = {
  file_id: file,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  version: 1,
  classification_revision_id: reference,
  classification_name: "Synthetic category",
  updated_at: "2026-10-28T00:00:00Z",
};
const controller = new AbortController();
afterEach(() => vi.unstubAllGlobals());
function mockResponse(value: unknown) {
  const fetch = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(value), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }),
  );
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

describe("staff file classification contract", () => {
  it("requires an explicit bounded command and consistent classification state", () => {
    expect(
      documentClassificationInputSchema.parse({
        expected_version: 0,
        classification_revision_id: "",
      }),
    ).toEqual({ expected_version: 0, classification_revision_id: "" });
    for (const input of [
      { expected_version: 0 },
      { expected_version: 0, classification_revision_id: null },
      { expected_version: -1, classification_revision_id: "" },
      { expected_version: Number.MAX_SAFE_INTEGER + 1, classification_revision_id: "" },
      { expected_version: 0, classification_revision_id: "", category: "identity_evidence" },
    ])
      expect(documentClassificationInputSchema.safeParse(input).success).toBe(false);
    for (const patch of [
      { version: 0 },
      { updated_at: null },
      { classification_revision_id: "" },
      { classification_name: "" },
    ])
      expect(documentClassificationSchema.safeParse({ ...saved, ...patch }).success).toBe(false);
    expect(
      documentClassificationSchema.parse({
        ...saved,
        version: 0,
        classification_revision_id: "",
        classification_name: "",
        updated_at: null,
      }).version,
    ).toBe(0);
  });
  it("uses Global API and binds requests to the active tenant and exact file", async () => {
    const fetch = mockResponse(saved);
    await expect(readDocumentClassification(scope, file, controller.signal)).resolves.toEqual(
      saved,
    );
    const [url, options] = fetch.mock.calls[0] as [string, RequestInit];
    expect(url).toContain(
      `/api/v1/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/documents/${file}/classification`,
    );
    expect(new Headers(options.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
    expect(new Headers(options.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
    expect(options.cache).toBe("no-store");
  });
  it("rejects stale or substituted resource responses", async () => {
    for (const patch of [
      { file_id: reference },
      { hospital_id: reference },
      { staff_id: reference },
      { file_id: "00000000-0000-0000-0000-000000000000" },
    ]) {
      mockResponse({ ...saved, ...patch });
      await expect(readDocumentClassification(scope, file, controller.signal)).rejects.toThrow();
    }
  });
  it("preserves the exact explicit command on uncertain retry", async () => {
    const input = { expected_version: 0, classification_revision_id: reference };
    const fetch = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("connection closed"))
      .mockResolvedValueOnce(new Response(JSON.stringify(saved), { status: 200 }));
    vi.stubGlobal("fetch", fetch);
    await expect(
      saveDocumentClassification(scope, file, input, controller.signal),
    ).rejects.toThrow();
    await expect(
      saveDocumentClassification(scope, file, input, controller.signal),
    ).resolves.toEqual(saved);
    expect(fetch.mock.calls[0][1].body).toBe(fetch.mock.calls[1][1].body);
    expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual(input);
  });
  it("does not accept a different selection or unexplained receipt version", async () => {
    for (const patch of [{ classification_revision_id: file }, { version: 9 }]) {
      mockResponse({ ...saved, ...patch });
      await expect(
        saveDocumentClassification(
          scope,
          file,
          { expected_version: 0, classification_revision_id: reference },
          controller.signal,
        ),
      ).rejects.toThrow();
    }
  });
  it("bounds history and validates every row and continuation", async () => {
    const { updated_at, ...current } = saved;
    const row = {
      ...current,
      id: reference,
      actor_subject: "synthetic-admin",
      created_at: updated_at,
    };
    const fetch = mockResponse({ data: [row], page: { has_more: false, next_cursor: null } });
    await expect(
      readDocumentClassificationHistory(scope, file, null, controller.signal),
    ).resolves.toEqual({ data: [row], page: { has_more: false, next_cursor: null } });
    expect(fetch.mock.calls[0][0]).toContain("/classification/history?limit=20");
    for (const response of [
      { data: [{ ...row, staff_id: reference }], page: { has_more: false, next_cursor: null } },
      { data: [row], page: { has_more: true, next_cursor: null } },
      { data: Array(21).fill(row), page: { has_more: false, next_cursor: null } },
    ]) {
      mockResponse(response);
      await expect(
        readDocumentClassificationHistory(scope, file, null, controller.signal),
      ).rejects.toThrow();
    }
  });
});
