import { describe, expect, it, vi, afterEach } from "vitest";
import { NextRequest } from "next/server";
import { POST } from "./route";

function request(headers: Record<string, string>, sessionCookie?: string): NextRequest {
  return new NextRequest("http://localhost/logout", {
    method: "POST",
    headers: {
      ...headers,
      ...(sessionCookie ? { cookie: `helivanta_session=${sessionCookie}` } : {}),
    },
  });
}

describe("POST /logout", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("refuses a cross-site POST", async () => {
    const res = await POST(request({ "sec-fetch-site": "cross-site" }));
    expect(res.status).toBe(403);
  });

  it("allows a request with no sec-fetch-site header (older browsers)", async () => {
    const res = await POST(request({}));
    expect(res.status).toBe(200);
  });

  it("clears the cookie without calling the API when there is no session to revoke", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    const res = await POST(request({ "sec-fetch-site": "same-origin" }));

    expect(fetchMock).not.toHaveBeenCalled();
    expect(res.status).toBe(200);
    expect(res.cookies.get("helivanta_session")?.value).toBe("");
  });

  // The literal regression test for #781: revoking server-side must
  // complete, and be seen to complete, before the transport cookie is
  // cleared. Telling the caller they are signed out while the session is
  // still live server-side is exactly the failure this endpoint exists to
  // prevent.
  it("does not clear the cookie when server-side revocation fails", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: false, status: 500 }),
    );

    const res = await POST(request({ "sec-fetch-site": "same-origin" }, "a-session-token"));

    expect(res.status).toBe(502);
    expect(res.cookies.get("helivanta_session")?.value).toBeUndefined();
  });

  it("does not clear the cookie when the revoke call itself throws", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("network down")));

    const res = await POST(request({ "sec-fetch-site": "same-origin" }, "a-session-token"));

    expect(res.status).toBe(502);
    expect(res.cookies.get("helivanta_session")?.value).toBeUndefined();
  });

  it("revokes server-side with the session bearer token, then clears the cookie", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200 });
    vi.stubGlobal("fetch", fetchMock);

    const res = await POST(request({ "sec-fetch-site": "same-origin" }, "a-session-token"));

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining("/v1/iam/me/sign-out"),
      expect.objectContaining({
        method: "POST",
        headers: { Authorization: "Bearer a-session-token" },
      }),
    );
    expect(res.status).toBe(200);
    expect(res.cookies.get("helivanta_session")?.value).toBe("");
  });
});
