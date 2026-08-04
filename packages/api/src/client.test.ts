import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, POLL_INTERVAL_MS, apiFetch } from "./client";

function mockFetch(status: number, body: unknown) {
  const fn = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    }),
  );
  vi.stubGlobal("fetch", fn);
  return fn;
}

afterEach(() => vi.unstubAllGlobals());

describe("apiFetch", () => {
  it("prefixes /api/v1 and returns parsed JSON", async () => {
    const fn = mockFetch(200, { data: [{ id: "1" }] });
    const body = await apiFetch<{ data: { id: string }[] }>("/medicore/visits");
    expect(fn).toHaveBeenCalledWith("/api/v1/medicore/visits", expect.any(Object));
    expect(body.data[0].id).toBe("1");
  });

  it("sends JSON bodies with the right content type", async () => {
    const fn = mockFetch(202, { id: "abc" });
    await apiFetch("/medicore/visits", {
      method: "POST",
      body: JSON.stringify({ patient_name: "X" }),
    });
    const init = fn.mock.calls[0][1] as RequestInit;
    expect(new Headers(init.headers).get("Content-Type")).toBe("application/json");
  });

  it("preserves custom headers when passed as Headers instance", async () => {
    const fn = mockFetch(200, { id: "1" });
    const customHeaders = new Headers({ "X-Custom": "1" });
    await apiFetch("/medicore/visits", { headers: customHeaders });
    const init = fn.mock.calls[0][1] as RequestInit;
    const outgoingHeaders = new Headers(init.headers);
    expect(outgoingHeaders.get("X-Custom")).toBe("1");
    expect(outgoingHeaders.get("Content-Type")).toBe("application/json");
  });

  it("does not overwrite caller-provided Content-Type", async () => {
    const fn = mockFetch(200, { id: "1" });
    await apiFetch("/medicore/visits", {
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
    });
    const init = fn.mock.calls[0][1] as RequestInit;
    expect(new Headers(init.headers).get("Content-Type")).toBe("application/x-www-form-urlencoded");
  });

  it("throws a typed ApiError from the platform envelope", async () => {
    mockFetch(409, { error: "conflict", message: "already dispensed" });
    const err = await apiFetch("/x").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).code).toBe("conflict");
    expect((err as ApiError).status).toBe(409);
    expect((err as ApiError).message).toBe("already dispensed");
  });

  it("falls back to a generic error when the body is not the envelope", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("boom", { status: 500 })));
    const err = await apiFetch("/x").catch((e: unknown) => e);
    expect((err as ApiError).code).toBe("internal");
  });

  it("exports the shared poll interval", () => {
    expect(POLL_INTERVAL_MS).toBe(3000);
  });
});
