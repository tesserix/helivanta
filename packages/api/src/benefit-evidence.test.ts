import { afterEach, expect, it, vi } from "vitest";
import { benefitEvidence, benefitEvidenceScope } from "./benefits";

afterEach(() => vi.unstubAllGlobals());

const hospital = "11111111-1111-4111-8111-111111111111";
const staff = "22222222-2222-4222-8222-222222222222";
const enrollment = "33333333-3333-4333-8333-333333333333";
const proposal = "44444444-4444-4444-8444-444444444444";
const other = "55555555-5555-4555-8555-555555555555";
const fileId = "88888888-8888-4888-8888-888888888888";
const stamp = "2026-10-19T00:00:00Z";
const scope = benefitEvidenceScope({ hospitalId: hospital, staffId: staff }, enrollment, proposal);
const root = `/workforce/hospitals/${hospital}/staff/${staff}/benefit-enrollments/${enrollment}/proposals/${proposal}/evidence`;
const saved = (over: object = {}) => ({
  id: fileId,
  hospital_id: hospital,
  staff_id: staff,
  record_id: proposal,
  record_kind: "benefit_proposal",
  record_version: 1,
  category: "benefit_eligibility_evidence",
  filename: "synthetic-certificate.png",
  content_type: "image/png",
  size: 4,
  sha256: "a".repeat(64),
  status: "upload_pending",
  scan_attempt: 1,
  uploaded_at: null,
  scanned_at: null,
  failure_code: null,
  expires_at: stamp,
  created_at: stamp,
  updated_at: stamp,
  ...over,
});
const image = () =>
  new File([new Uint8Array([137, 80, 78, 71])], "synthetic-certificate.png", { type: "image/png" });

it("scopes evidence to the proposal and refuses paths built from unsafe identifiers", () => {
  expect(scope).toEqual({
    recordPath: root.replace(/\/evidence$/, ""),
    staffId: staff,
    recordId: proposal,
  });
  expect(benefitEvidence.key(scope)).toEqual([
    "workforce",
    "benefit-evidence",
    scope.recordPath,
    proposal,
  ]);
  expect(() =>
    benefitEvidenceScope({ hospitalId: hospital, staffId: staff }, "../x", proposal),
  ).toThrow();
});

it("accepts only eligibility evidence of this staff member's proposal", () => {
  expect(benefitEvidence.receipt(scope, saved()).id).toBe(fileId);
  expect(() => benefitEvidence.receipt(scope, saved({ record_id: other }))).toThrow(/ownership/);
  expect(() => benefitEvidence.receipt(scope, saved({ staff_id: other }))).toThrow(/ownership/);
  expect(() => benefitEvidence.receipt(scope, saved({ record_kind: "tax_declaration" }))).toThrow();
  expect(() =>
    benefitEvidence.receipt(scope, saved({ category: "tax_declaration_evidence" })),
  ).toThrow();
  expect(() => benefitEvidence.receipt(scope, saved(), other)).toThrow(/ownership/);
});

it("lists from the proposal's own path and refuses another proposal's file", async () => {
  const urls: string[] = [];
  let listed = [saved()];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      urls.push(url);
      return new Response(JSON.stringify({ evidence: listed, uploads_available: true }));
    }),
  );
  await expect(benefitEvidence.fetch(scope, new AbortController().signal)).resolves.toEqual({
    evidence: [saved()],
    uploads_available: true,
  });
  expect(urls).toEqual([`/api/v1${root}`]);
  listed = [saved({ record_id: other })];
  await expect(benefitEvidence.fetch(scope, new AbortController().signal)).rejects.toThrow(
    /ownership/,
  );
});

it("registers the file under the proposal, then streams its bytes", async () => {
  const upload = await benefitEvidence.prepare(
    scope,
    fileId,
    image(),
    new AbortController().signal,
  );
  expect(upload.input).toEqual({
    filename: "synthetic-certificate.png",
    content_type: "image/png",
    size: 4,
    sha256: "0f4636c78f65d3639ece5a064b5ae753e3408614a14fb18ab4d7540d2c248543",
  });
  const calls: { url: string; method?: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      calls.push({ url, method: init.method, body: init.body });
      const sha256 = upload.input.sha256;
      return new Response(
        JSON.stringify(
          saved({ sha256, status: calls.length === 1 ? "upload_pending" : "scan_pending" }),
        ),
      );
    }),
  );
  await expect(
    benefitEvidence.submit(scope, upload, new AbortController().signal),
  ).resolves.toMatchObject({ status: "scan_pending" });
  expect(calls.map(({ url, method }) => [method, url])).toEqual([
    ["PUT", `/api/v1${root}/${fileId}`],
    ["PUT", `/api/v1${root}/${fileId}/content`],
  ]);
  expect(JSON.parse(String(calls[0]!.body))).toEqual(upload.input);
  expect(calls[1]!.body).toBe(upload.file);
});

it("refuses to send a file prepared for another proposal", async () => {
  const upload = await benefitEvidence.prepare(
    scope,
    fileId,
    image(),
    new AbortController().signal,
  );
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  const elsewhere = benefitEvidenceScope(
    { hospitalId: hospital, staffId: staff },
    enrollment,
    other,
  );
  await expect(
    benefitEvidence.submit(elsewhere, upload, new AbortController().signal),
  ).rejects.toThrow(/different proposal/);
  expect(fetch).not.toHaveBeenCalled();
});
