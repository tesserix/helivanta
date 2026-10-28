import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { POST } from "./route";

let warn: ReturnType<typeof vi.spyOn>;
beforeEach(() => {
  warn = vi.spyOn(console, "warn").mockImplementation(() => {});
});
afterEach(() => warn.mockRestore());

function report(body: BodyInit, type = "application/csp-report") {
  return new Request("https://h/api/csp-report", {
    method: "POST",
    headers: { "content-type": type },
    body,
    duplex: "half",
  } as RequestInit);
}

const violation = {
  "document-uri": "https://h/patients?q=Asha#x",
  "effective-directive": "script-src-elem",
  disposition: "enforce",
  "blocked-uri": "inline",
  "source-file": "https://h/_next/static/chunks/a.js?v=Asha",
  "line-number": 3,
  "column-number": 7,
};

it("logs a sanitised violation and answers 204", async () => {
  const response = await POST(report(JSON.stringify({ "csp-report": violation })));
  expect(response.status).toBe(204);
  expect(warn).toHaveBeenCalledTimes(1);
  const line = warn.mock.calls[0]![0] as string;
  expect(line).not.toContain("Asha");
  expect(JSON.parse(line)).toEqual({
    msg: "csp violation",
    "effective-directive": "script-src-elem",
    disposition: "enforce",
    "document-uri": "https://h/patients",
    "blocked-uri": "inline",
    "source-file": "https://h/_next/static/chunks/a.js",
    "line-number": 3,
    "column-number": 7,
  });
});

it("keeps non-URL blocked-uri keywords and drops anything else", async () => {
  for (const [blocked, logged] of [
    ["eval", "eval"],
    ["javascript:alert(1)", "invalid"],
    ["not a url", "invalid"],
  ]) {
    warn.mockClear();
    await POST(report(JSON.stringify({ "csp-report": { ...violation, "blocked-uri": blocked } })));
    expect(JSON.parse(warn.mock.calls[0]![0] as string)["blocked-uri"]).toBe(logged);
  }
});

it("truncates long strings to 512 characters", async () => {
  const long = "a".repeat(2000);
  await POST(
    report(JSON.stringify({ "csp-report": { ...violation, "effective-directive": long } })),
  );
  expect(JSON.parse(warn.mock.calls[0]![0] as string)["effective-directive"]).toHaveLength(512);
});

it("ignores fields it does not know and numbers given as strings", async () => {
  await POST(
    report(
      JSON.stringify({
        "csp-report": { ...violation, "script-sample": "secret", "line-number": "3" },
      }),
    ),
  );
  const line = warn.mock.calls[0]![0] as string;
  expect(line).not.toContain("secret");
  expect(JSON.parse(line)["line-number"]).toBeUndefined();
});

it("rejects other content types", async () => {
  const response = await POST(report(JSON.stringify({ "csp-report": violation }), "text/plain"));
  expect(response.status).toBe(415);
  expect(warn).not.toHaveBeenCalled();
});

it("rejects bodies over 16 KiB without trusting Content-Length", async () => {
  const chunk = new TextEncoder().encode("x".repeat(4096));
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (let i = 0; i < 5; i++) controller.enqueue(chunk);
      controller.close();
    },
  });
  const response = await POST(report(stream));
  expect(response.status).toBe(413);
  expect(warn).not.toHaveBeenCalled();
});

it("rejects a body that is not a csp-report object", async () => {
  for (const body of ["[]", "{}", '{"csp-report":"x"}', '{"csp-report":[]}', "not json"]) {
    const response = await POST(report(body));
    expect(response.status, body).toBe(400);
  }
  expect(warn).not.toHaveBeenCalled();
});
