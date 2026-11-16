import { afterEach, expect, it, vi } from "vitest";
import { releaseDefinitions } from "./release-capabilities-contract";
import { capabilityForPage, readReleaseCapabilities } from "./release-capabilities";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "owner" };
const now = new Date("2026-11-16T00:00:00Z");
const snapshot = () => ({
  tenant_id: scope.tenantId,
  subject: scope.subject,
  policy_version: "2026-11-16-r1",
  issued_at: now.toISOString(),
  expires_at: new Date(now.getTime() + 60_000).toISOString(),
  capabilities: releaseDefinitions.map((definition) => ({ key: definition.key, available: false })),
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

it("reads a bounded snapshot with the current account precondition", async () => {
  vi.setSystemTime(now);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      expect(url).toBe("/api/v1/workspace/release-capabilities");
      expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
      expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
      return new Response(JSON.stringify(snapshot()));
    }),
  );
  expect(await readReleaseCapabilities(scope)).toEqual(snapshot());
});

it.each([
  { tenant_id: "22222222-2222-4222-8222-222222222222" },
  { subject: "another-account" },
  { policy_version: "unknown-policy" },
  { expires_at: "2026-11-16T00:00:00Z" },
  { expires_at: "2026-11-16T01:00:00Z" },
  { issued_at: "2026-11-16T00:10:00Z" },
  { capabilities: [] },
  { capabilities: [...snapshot().capabilities.slice(1), snapshot().capabilities[1]] },
])("refuses stale, malformed or misbound snapshots: %j", async (change) => {
  vi.setSystemTime(now);
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify({ ...snapshot(), ...change }))),
  );
  await expect(readReleaseCapabilities(scope)).rejects.toThrow();
});

it("an enabled child cannot bypass a disabled required feature", async () => {
  vi.setSystemTime(now);
  const value = snapshot();
  value.capabilities.find((row) => row.key === "staff_core")!.available = true;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify(value))),
  );
  await expect(readReleaseCapabilities(scope)).rejects.toThrow();
});

it("maps pages exactly, so an unregistered path does not inherit staff access", () => {
  expect(capabilityForPage("/workforce")).toBe("staff_core");
  expect(capabilityForPage("/workforce/leave-requests")).toBe("workforce_scheduling");
  expect(capabilityForPage("/workforce/new-untested-page")).toBeUndefined();
  expect(capabilityForPage("/workforce-leak")).toBeUndefined();
  expect(capabilityForPage("/settings/security")).toBe("baseline");
});
