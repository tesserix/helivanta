import { afterEach, expect, it, vi } from "vitest";

let stop: (() => void) | undefined;
afterEach(() => {
  stop?.();
  stop = undefined;
  window.history.replaceState(null, "", "/");
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

async function reporter() {
  vi.resetModules();
  const fetchReport = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(
    async () => new Response(null, { status: 204 }),
  );
  vi.stubGlobal("fetch", fetchReport);
  const logged = vi.spyOn(console, "error").mockImplementation(() => {});
  return { ...(await import("./frontend-errors-client")), fetchReport, logged };
}

it("reports only allowlisted metadata, never private error properties or URL data", async () => {
  const client = await reporter();
  window.history.replaceState(null, "", "/workforce/synthetic-private-id?name=private-marker");
  const failure = Object.assign(new Error("private-marker SQL SELECT credential"), {
    name: "private-marker",
    stack: "private-marker stack",
    cause: { password: "private-marker" },
    patient: "private-marker",
  });
  const report = await client.reportFrontendError(failure, "workspace");
  expect(report).toMatchObject({
    version: 1,
    kind: "unknown",
    area: "workforce",
    source: "workspace",
  });
  expect(report?.reference).toMatch(/^HV-[0-9a-f-]{36}$/);
  const [, init] = client.fetchReport.mock.calls[0]!;
  expect(init).toMatchObject({ credentials: "omit", referrerPolicy: "no-referrer" });
  expect(JSON.parse(String(init?.body))).toEqual(report);
  expect(JSON.stringify(client.logged.mock.calls)).not.toContain("private-marker");
  expect(String(init?.body)).not.toContain("private-marker");
  expect(Object.keys(report!).sort()).toEqual(["area", "kind", "reference", "source", "version"]);
});

it("shares one reference and delivery for repeated observations of the same error", async () => {
  const client = await reporter();
  const failure = new TypeError("private-marker");
  const [first, second] = await Promise.all([
    client.reportFrontendError(failure, "runtime"),
    client.reportFrontendError(failure, "workspace"),
  ]);
  expect(first).toEqual(second);
  expect(first?.kind).toBe("TypeError");
  expect(client.fetchReport).toHaveBeenCalledOnce();
  expect(client.logged).toHaveBeenCalledOnce();
});

it("bounds network reports and keeps subsequent references in safe local logs", async () => {
  vi.useFakeTimers();
  const client = await reporter();
  const reports = [];
  for (let i = 0; i < 6; i++) reports.push(await client.reportFrontendError(new Error(), "root"));
  expect(client.fetchReport).toHaveBeenCalledTimes(5);
  expect(new Set(reports.map((report) => report?.reference)).size).toBe(6);
  expect(client.logged).toHaveBeenCalledTimes(6);
  await vi.advanceTimersByTimeAsync(60_001);
  await client.reportFrontendError(new Error(), "root");
  expect(client.fetchReport).toHaveBeenCalledTimes(6);
});

it("aborts a stalled collector and retains the reference already logged locally", async () => {
  vi.useFakeTimers();
  const client = await reporter();
  let aborted = false;
  client.fetchReport.mockImplementation(
    (_url, init) =>
      new Promise((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () => {
          aborted = true;
          reject(new Error("private transport detail"));
        });
      }),
  );
  const pending = client.reportFrontendError(new Error(), "root");
  await vi.advanceTimersByTimeAsync(1500);
  const result = await pending;
  expect(aborted).toBe(true);
  expect(result?.reference).toMatch(/^HV-/);
  expect(client.logged).toHaveBeenCalledWith(expect.stringContaining(result!.reference));
});

it("does not throw when private getters, cryptography or logging are unavailable", async () => {
  const client = await reporter();
  const failure = Object.defineProperty({}, "name", {
    get: () => {
      throw new Error("private getter");
    },
  });
  expect((await client.reportFrontendError(failure, "runtime"))?.kind).toBe("unknown");
  vi.spyOn(crypto, "randomUUID").mockImplementation(() => {
    throw new Error("private crypto detail");
  });
  expect(await client.reportFrontendError(new Error(), "root")).toBeUndefined();
});

it("installs runtime and rejection listeners without cancelling browser error handling", async () => {
  const client = await reporter();
  const listeners = vi.spyOn(window, "addEventListener");
  stop = client.installClientErrorReporting();
  const runtime = listeners.mock.calls.find(([name]) => name === "error")![1] as EventListener;
  const rejection = listeners.mock.calls.find(
    ([name]) => name === "unhandledrejection",
  )![1] as EventListener;
  const failure = new Error("private-marker");
  const errorEvent = new ErrorEvent("error", { error: failure, cancelable: true });
  runtime(errorEvent);
  const rejected = Object.defineProperty(
    new Event("unhandledrejection", { cancelable: true }),
    "reason",
    { value: "private-marker" },
  );
  rejection(rejected);
  await client.reportFrontendError(failure, "root");
  expect(client.fetchReport).toHaveBeenCalledTimes(2);
  expect(errorEvent.defaultPrevented).toBe(false);
  expect(rejected.defaultPrevented).toBe(false);
  expect(JSON.stringify(client.logged.mock.calls)).not.toContain("private-marker");
});
