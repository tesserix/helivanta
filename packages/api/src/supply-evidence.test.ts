import { webcrypto } from "node:crypto";
import { afterEach, expect, it, vi } from "vitest";
import {
  prepareSupplyEvidence,
  submitSupplyEvidence,
  supplyEvidenceFormSchema,
} from "./supply-evidence";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-owner",
  partyId: "0199e000-0000-7000-8000-000000000001",
  licenseId: "0199e000-0000-7000-8000-000000000002",
};
const id = "0199e000-0000-7000-8000-000000000003";
afterEach(() => vi.unstubAllGlobals());
it("keeps the same licence revision and file identifier across uncertain upload retries", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const bytes = new TextEncoder().encode("TEST image");
  const file = {
    name: "TEST.png",
    type: "image/png",
    size: bytes.length,
    arrayBuffer: async () => bytes.buffer,
  } as File;
  const signal = new AbortController().signal;
  const upload = await prepareSupplyEvidence(id, file, 2, signal);
  const receipt = {
    id,
    party_id: scope.partyId,
    license_id: scope.licenseId,
    license_version: 2,
    filename: file.name,
    content_type: file.type,
    size: file.size,
    status: "upload_pending",
    scan_attempt: 1,
    uploaded_at: null,
    scanned_at: null,
    failure_code: null,
    uploader_subject: "TEST-owner",
    created_at: "2026-10-16T00:00:00Z",
    updated_at: "2026-10-16T00:00:00Z",
    expires_at: "2026-10-16T00:30:00Z",
  };
  let fail = true;
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    expect(new Headers(init?.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
    if (url.endsWith("/content")) {
      if (fail) {
        fail = false;
        throw new Error("TEST uncertain upload");
      }
      return new Response(JSON.stringify({ ...receipt, status: "scan_pending" }));
    }
    expect(JSON.parse(init?.body as string).expected_license_version).toBe(2);
    return new Response(JSON.stringify(receipt));
  });
  vi.stubGlobal("fetch", fetcher);
  await expect(submitSupplyEvidence(scope, upload, signal)).rejects.toThrow("TEST uncertain");
  await expect(submitSupplyEvidence(scope, upload, signal)).resolves.toMatchObject({
    id,
    status: "scan_pending",
    license_version: 2,
  });
  const writes = fetcher.mock.calls.filter(([url]) => !url.endsWith("/content"));
  expect(writes).toHaveLength(2);
  expect(writes[0][0]).toBe(writes[1][0]);
});
it("refuses foreign receipts before uploading bytes", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const data = new Uint8Array([1]);
  const upload = await prepareSupplyEvidence(
    id,
    { name: "TEST.png", type: "image/png", size: 1, arrayBuffer: async () => data.buffer } as File,
    1,
    new AbortController().signal,
  );
  const fetcher = vi.fn(
    async () =>
      new Response(
        JSON.stringify({
          id,
          party_id: scope.partyId,
          license_id: "0199e000-0000-7000-8000-000000000099",
        }),
      ),
  );
  vi.stubGlobal("fetch", fetcher);
  await expect(submitSupplyEvidence(scope, upload, new AbortController().signal)).rejects.toThrow();
  expect(fetcher).toHaveBeenCalledTimes(1);
});
it("rejects unsafe filenames and active content from the shared evidence schema", () => {
  const input = {
    expected_license_version: 1,
    filename: "TEST.png",
    content_type: "image/png",
    size: 1,
    sha256: "a".repeat(64),
  };
  expect(supplyEvidenceFormSchema.safeParse(input).success).toBe(true);
  for (const patch of [
    { filename: "../TEST.png" },
    { filename: "TEST\n.png" },
    { filename: " TEST.png" },
    { content_type: "application/pdf" },
    { size: 10485761 },
    { expected_license_version: 0 },
    { sha256: "A".repeat(64) },
    { verified: true },
  ])
    expect(supplyEvidenceFormSchema.safeParse({ ...input, ...patch }).success).toBe(false);
});
