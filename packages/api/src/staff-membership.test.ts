import { afterEach, expect, it, vi } from "vitest";
import { enrollStaffMember } from "./staff-membership";

const tenant = "11111111-1111-4111-8111-111111111111";
const id = "22222222-2222-4222-8222-222222222222";
afterEach(() => vi.unstubAllGlobals());

it("enrolls only staff_member through Global API with a tenant precondition", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json({ id }));
  vi.stubGlobal("fetch", fetch);
  expect(await enrollStaffMember(tenant, "synthetic-reviewer")).toEqual({ id });
  const [url, init] = fetch.mock.calls[0]!;
  expect(url).toBe("/api/v1/iam/members");
  expect(init.method).toBe("POST");
  expect(JSON.parse(init.body)).toEqual({
    subject: "synthetic-reviewer",
    role_key: "staff_member",
  });
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenant);
  expect(init.cache).toBe("no-store");
});

it("refuses malformed subjects before transmitting a membership command", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  for (const subject of ["", " ", "*", "a#b", "a:b", "a\0b", "a b", "a".repeat(201)]) {
    await expect(enrollStaffMember(tenant, subject)).rejects.toThrow();
  }
  expect(fetch).not.toHaveBeenCalled();
});

it("does not present an invalid accepted response as enrollment success", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ id: "not-an-id" })));
  await expect(enrollStaffMember(tenant, "synthetic-reviewer")).rejects.toThrow();
});
