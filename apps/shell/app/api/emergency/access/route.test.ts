import { describe, expect, it } from "vitest";
import { GET } from "./route";

const request = (query = "") => new Request(`https://helivanta.app/api/emergency/access${query}`);

describe("public emergency access API", () => {
  it("serves only public source-backed India calling information without a session", async () => {
    const response = GET(request("?country=IN"));
    expect(response.status).toBe(200);
    expect(response.headers.get("Cache-Control")).toBe("no-store");
    expect(response.headers.get("Referrer-Policy")).toBe("no-referrer");
    expect(response.headers.has("Set-Cookie")).toBe(false);
    const body = await response.json();
    expect(body.countryCode).toBe("IN");
    expect(body.services[0].dialUrl).toBe("tel:112");
    expect(body.bookingEnabled).toBe(false);
    expect(JSON.stringify(body)).not.toMatch(/tenant_id|patient_id|coordinates|requester_phone/);
  });

  it("defaults to an unsupported country rather than guessing a local emergency service", async () => {
    expect((await GET(request()).json()).services).toEqual([]);
    expect((await GET(request("?country=OTHER")).json()).services).toEqual([]);
  });

  it.each([
    "?country=AU",
    "?country=IN&country=OTHER",
    "?country=",
    "?country=in",
    "?tenant_id=private",
    "?country=IN&phone=999",
    "?country=IN&country=IN",
  ])("rejects malformed or unrecognised query %s", async (query) => {
    const response = GET(request(query));
    expect(response.status).toBe(400);
    expect(await response.json()).toEqual({
      error: "invalid_query",
      message: "Choose a supported country.",
    });
  });
});
