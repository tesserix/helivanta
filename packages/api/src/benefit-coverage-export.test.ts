import { afterEach, expect, it, vi } from "vitest";
import { readAttachmentFile } from "./attachment-file";
import {
  benefitCoverageExportFormSchema,
  fetchBenefitCoverageCSV,
} from "./benefit-coverage-export";

const hospital = "11111111-1111-4111-8111-111111111111";
const plan = "55555555-5555-4555-8555-555555555555";
const body = "as_of,enrollment_id\r\n";
const digest = "9a33cbe1636bbd0a5d334998ee880026952f9652d56e42bfe841149b227f25fa";
afterEach(() => vi.unstubAllGlobals());

const file = (headers: Record<string, string> = {}) =>
  new Response(body, {
    headers: {
      "Content-Type": "text/csv; charset=utf-8",
      "Content-Disposition": 'attachment; filename="benefits-coverage-2024-06-01.csv"',
      "X-Export-Row-Count": "0",
      "X-Export-Sha256": digest,
      ...headers,
    },
  });

it("asks for a date in range and at most one valid plan", () => {
  const message = (value: object) =>
    benefitCoverageExportFormSchema.safeParse({ as_of: "2024-06-01", plan_id: "", ...value }).error
      ?.issues[0]?.message;
  expect(message({})).toBeUndefined();
  expect(message({ plan_id: plan })).toBeUndefined();
  expect(message({ as_of: "" })).toBe("Choose the date the file describes.");
  expect(message({ as_of: "2024-02-30" })).toBe("Use a valid date.");
  expect(message({ as_of: "1999-12-31" })).toBe("Choose a date between 2000-01-01 and 2200-12-31.");
  expect(message({ as_of: "2201-01-01" })).toBe("Choose a date between 2000-01-01 and 2200-12-31.");
  expect(message({ plan_id: "gold" })).toBe("Choose a valid record.");
});

it("downloads the file for the date and plan, reporting its verified row count", async () => {
  const requests: { url: string; cache?: RequestCache }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      requests.push({ url, cache: init.cache });
      return file();
    }),
  );
  const signal = new AbortController().signal;
  const all = await fetchBenefitCoverageCSV(hospital, { as_of: "2024-06-01", plan_id: "" }, signal);
  expect(all.filename).toBe("benefits-coverage-2024-06-01.csv");
  expect(all.rows).toBe(0);
  expect(new TextDecoder().decode(await readAttachmentFile(all.blob, signal))).toBe(body);
  await fetchBenefitCoverageCSV(hospital, { as_of: "2024-06-01", plan_id: plan }, signal);
  expect(requests).toEqual([
    {
      url: `/api/v1/workforce/hospitals/${hospital}/hr/benefits/coverage.csv?as_of=2024-06-01`,
      cache: "no-store",
    },
    {
      url: `/api/v1/workforce/hospitals/${hospital}/hr/benefits/coverage.csv?as_of=2024-06-01&plan_id=${plan}`,
      cache: "no-store",
    },
  ]);
});

it.each<Record<string, string>>([
  { "Content-Type": "text/html" },
  { "Content-Disposition": 'attachment; filename="benefits-coverage-2024-06-02.csv"' },
  { "Content-Disposition": 'attachment; filename="../benefits-coverage-2024-06-01.csv"' },
  { "Content-Disposition": 'inline; filename="benefits-coverage-2024-06-01.csv"' },
  { "Content-Disposition": "" },
  { "X-Export-Row-Count": "" },
  { "X-Export-Row-Count": "-1" },
  { "X-Export-Row-Count": "1.5" },
  { "X-Export-Row-Count": "1001" },
  { "X-Export-Sha256": "" },
  { "X-Export-Sha256": "A".repeat(64) },
])("refuses a response that is not the requested coverage file: %o", async (headers) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(file(headers)));
  await expect(
    fetchBenefitCoverageCSV(
      hospital,
      { as_of: "2024-06-01", plan_id: "" },
      new AbortController().signal,
    ),
  ).rejects.toThrow("The server did not return a valid coverage file. Try again.");
});

it("refuses bytes that do not match the digest the export was audited with", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(file({ "X-Export-Sha256": "b".repeat(64) })));
  await expect(
    fetchBenefitCoverageCSV(
      hospital,
      { as_of: "2024-06-01", plan_id: "" },
      new AbortController().signal,
    ),
  ).rejects.toThrow("The coverage file could not be verified. Try again.");
});

it("refuses a response if the workspace aborts while the body is being read", async () => {
  const controller = new AbortController();
  const response = file();
  const blob = await file().blob();
  vi.spyOn(response, "blob").mockImplementation(async () => {
    controller.abort();
    return blob;
  });
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
  await expect(
    fetchBenefitCoverageCSV(hospital, { as_of: "2024-06-01", plan_id: "" }, controller.signal),
  ).rejects.toMatchObject({ name: "AbortError" });
});
