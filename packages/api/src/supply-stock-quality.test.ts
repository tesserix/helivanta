import { afterEach, expect, it, vi } from "vitest";
import {
  recordWholesaleQuality,
  recoverWholesaleQuality,
  getWholesaleInspection,
} from "./supply-stock-quality";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-reviewer",
  partyId: "019a55c0-1111-7111-8111-111111111112",
  premiseId: "019a55c0-1111-7111-8111-111111111113",
};
const receipt = "019a55c0-1111-7111-8111-111111111114";
const command = "019a55c0-1111-7111-8111-111111111115";
const input = {
  expected_version: 0,
  identity_check_id: "019a55c0-1111-7111-8111-111111111116",
  identity_version: 1,
  decision: "release" as const,
  reason: "TEST inspection",
};
const result = {
  id: command,
  owner_tenant_id: scope.tenantId,
  party_id: scope.partyId,
  premise_id: scope.premiseId,
  receipt_id: receipt,
  input,
  version: 1,
  status: "released",
  released_base_units: 100,
  actor_subject: scope.subject,
  created_at: "2026-11-08T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());
it("records and recovers exact source-scoped quality intent with one PUT", async () => {
  const request = vi.fn<typeof fetch>().mockImplementation(async () => Response.json(result));
  vi.stubGlobal("fetch", request);
  expect(await recordWholesaleQuality(scope, receipt, command, input)).toEqual(result);
  expect(await recoverWholesaleQuality(scope, receipt, command, input)).toEqual(result);
  expect(request.mock.calls.map(([, init]) => init?.method ?? "GET")).toEqual(["PUT", "GET"]);
  expect(request.mock.calls[0]![0]).toBe(
    `/api/v1/supply/parties/${scope.partyId}/operating-locations/${scope.premiseId}/stock/receipts/${receipt}/quality-decisions/${command}`,
  );
  const headers = new Headers(request.mock.calls[0]![1]?.headers);
  expect(headers.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});
it.each([
  { premise_id: receipt },
  { owner_tenant_id: receipt },
  { receipt_id: command },
  { id: receipt },
  { actor_subject: "TEST-other" },
  { version: 2 },
  { status: "held" },
  { released_base_units: 0 },
  { input: { ...input, reason: "changed" } },
])("refuses mismatched quality evidence %j", async (change) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ ...result, ...change })));
  await expect(recoverWholesaleQuality(scope, receipt, command, input)).rejects.toThrow();
});
it("refuses nested evidence for another physical receipt", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      Response.json({
        owner_tenant_id: scope.tenantId,
        party_id: scope.partyId,
        premise_id: scope.premiseId,
        receipt_id: receipt,
        quality: { ...result, receipt_id: command },
      }),
    ),
  );
  await expect(getWholesaleInspection(scope, receipt)).rejects.toThrow();
});
