import { afterEach, expect, it, vi } from "vitest";

const report = {
  version: 1,
  reference: "HV-11111111-1111-4111-8111-111111111111",
  source: "workspace",
  kind: "TypeError",
  area: "workforce",
};

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  vi.useRealTimers();
});

async function collector() {
  vi.resetModules();
  const logged = vi.spyOn(console, "error").mockImplementation(() => {});
  return { ...(await import("./route")), logged };
}

function request(body: unknown = report, overrides: Record<string, string> = {}) {
  return new Request("https://helivanta.example.test/api/client-errors", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Host: "helivanta.example.test",
      Origin: "https://helivanta.example.test",
      "Sec-Fetch-Site": "same-origin",
      ...overrides,
    },
    body: JSON.stringify(body),
  });
}

it("logs exactly the safe report with the same support reference", async () => {
  const { POST, logged } = await collector();
  const response = await POST(
    request(report, {
      Cookie: "synthetic-private-cookie",
      Authorization: "Bearer synthetic-private-token",
      "X-Helivanta-Expected-Tenant": "synthetic-private-tenant",
    }),
  );
  expect(response.status).toBe(204);
  expect(response.headers.get("cache-control")).toBe("no-store");
  expect(response.headers.get("referrer-policy")).toBe("no-referrer");
  expect(logged).toHaveBeenCalledOnce();
  expect(JSON.parse(String(logged.mock.calls[0]![0]))).toEqual({
    ...report,
    severity: "ERROR",
    event: "frontend_error",
  });
  expect(JSON.stringify(logged.mock.calls)).not.toContain("synthetic-private");
});

it.each<Record<string, string>>([
  { Origin: "https://foreign.example.test" },
  { Origin: "null" },
  { Origin: "https://helivanta.example.test/private-marker" },
  { "Sec-Fetch-Site": "cross-site" },
  { "Sec-Fetch-Site": "same-site" },
  { Host: "foreign.example.test", "X-Forwarded-Host": "helivanta.example.test" },
])("rejects foreign or ambiguous origin without logging: %j", async (headers) => {
  const { POST, logged } = await collector();
  expect((await POST(request(report, headers))).status).toBe(403);
  expect(logged).not.toHaveBeenCalled();
});

it.each([
  { ...report, message: "synthetic-private-marker" },
  { ...report, tenant_id: "synthetic-private-marker" },
  { ...report, stack: "synthetic-private-marker" },
  { ...report, kind: "synthetic-private-marker" },
  { ...report, area: "synthetic-private-marker" },
  { ...report, reference: "synthetic-private-marker" },
  { ...report, version: 2 },
  [report],
])("rejects unknown/private or invalid payload fields without logging", async (body) => {
  const { POST, logged } = await collector();
  const response = await POST(request(body));
  expect(response.status).toBe(400);
  expect(await response.text()).not.toContain("synthetic-private-marker");
  expect(logged).not.toHaveBeenCalled();
});

it("deduplicates the same report and refuses conflicting reference reuse", async () => {
  const { POST, logged } = await collector();
  expect((await POST(request())).status).toBe(204);
  expect((await POST(request())).status).toBe(204);
  expect((await POST(request({ ...report, kind: "RangeError" }))).status).toBe(409);
  expect(logged).toHaveBeenCalledOnce();
});

it("caps concurrent attempts and recovers capacity after its window", async () => {
  vi.useFakeTimers();
  const { POST, logged } = await collector();
  const responses = await Promise.all(
    Array.from({ length: 62 }, () =>
      POST(
        request({
          ...report,
          reference: `HV-${crypto.randomUUID()}`,
        }),
      ),
    ),
  );
  expect(responses.filter((response) => response.status === 204)).toHaveLength(60);
  const refused = responses.filter((response) => response.status === 429);
  expect(refused).toHaveLength(2);
  expect(Number(refused[0]!.headers.get("retry-after"))).toBeGreaterThan(0);
  expect(refused[0]!.headers.get("ratelimit-limit")).toBe("60");
  expect(refused[0]!.headers.get("ratelimit-remaining")).toBe("0");
  expect(refused[0]!.headers.get("ratelimit-reset")).toMatch(/^\d+$/);
  expect(logged).toHaveBeenCalledTimes(60);
  await vi.advanceTimersByTimeAsync(60_001);
  expect((await POST(request())).status).toBe(204);
});

it("bounds chunked bytes even when Content-Length understates the body", async () => {
  const { POST, logged } = await collector();
  let cancelled = false;
  const stream = new ReadableStream({
    pull(controller) {
      controller.enqueue(new Uint8Array(600));
    },
    cancel() {
      cancelled = true;
    },
  });
  const input = request();
  const response = await POST(
    new Request(input.url, {
      method: "POST",
      headers: { ...Object.fromEntries(input.headers), "Content-Length": "1" },
      body: stream,
      duplex: "half",
    } as RequestInit),
  );
  expect(response.status).toBe(413);
  expect(cancelled).toBe(true);
  expect(logged).not.toHaveBeenCalled();
});

it("terminates a stalled body instead of holding the collector open", async () => {
  vi.useFakeTimers();
  const { POST, logged } = await collector();
  let cancelled = false;
  const input = request();
  const stream = new ReadableStream({
    cancel() {
      cancelled = true;
    },
  });
  const pending = POST(
    new Request(input.url, {
      method: "POST",
      headers: input.headers,
      body: stream,
      duplex: "half",
    } as RequestInit),
  );
  await vi.advanceTimersByTimeAsync(2000);
  expect((await pending).status).toBe(408);
  expect(cancelled).toBe(true);
  expect(logged).not.toHaveBeenCalled();
});

it("does not acknowledge a report when its server log sink fails", async () => {
  const { POST, logged } = await collector();
  logged.mockImplementation(() => {
    throw new Error("synthetic-private-sink");
  });
  const response = await POST(request());
  expect(response.status).toBe(503);
  expect(await response.text()).not.toContain("synthetic-private-sink");
  logged.mockImplementation(() => {});
  expect((await POST(request())).status).toBe(204);
});

it("rejects malformed, oversized and non-JSON reports without logging raw input", async () => {
  const { POST, logged } = await collector();
  expect((await POST(request(report, { "Content-Type": "text/plain" }))).status).toBe(415);
  expect((await POST(request(report, { "Content-Length": "1025" }))).status).toBe(413);
  const input = request();
  expect(
    (
      await POST(
        new Request(input.url, {
          method: "POST",
          headers: input.headers,
          body: "{synthetic-private-malformed",
        }),
      )
    ).status,
  ).toBe(400);
  const unreadable = new ReadableStream({
    pull(controller) {
      controller.error(new Error("synthetic-private-stream"));
    },
  });
  expect(
    (
      await POST(
        new Request(input.url, {
          method: "POST",
          headers: input.headers,
          body: unreadable,
          duplex: "half",
        } as RequestInit),
      )
    ).status,
  ).toBe(400);
  expect(logged).not.toHaveBeenCalled();
});

it("requires secure production origins and never trusts forwarding headers", async () => {
  vi.stubEnv("NODE_ENV", "production");
  const { POST, logged } = await collector();
  expect(
    (
      await POST(
        request(report, { Origin: "http://helivanta.example.test", "X-Forwarded-Proto": "https" }),
      )
    ).status,
  ).toBe(403);
  const missing = request();
  missing.headers.delete("Origin");
  expect((await POST(missing)).status).toBe(403);
  expect(logged).not.toHaveBeenCalled();
  expect(
    (
      await POST(
        new Request("http://127.0.0.1:4344/api/client-errors", {
          method: "POST",
          headers: {
            Host: "127.0.0.1:4344",
            Origin: "http://127.0.0.1:4344",
            "Content-Type": "application/json",
          },
          body: JSON.stringify(report),
        }),
      )
    ).status,
  ).toBe(204);
});
