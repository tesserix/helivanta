import { afterEach, expect, it, vi } from "vitest";
import { staffSetup } from "./staff-setup";

afterEach(() => vi.unstubAllGlobals());

const invitation = "019a7777-7777-7777-8777-777777777777";
const ticket = "A".repeat(43);

it("uses only the same-origin auth handoff and validates the canonical receipt", async () => {
  const fetcher = vi.fn(
    async () => new Response(JSON.stringify({ state: "complete" }), { status: 200 }),
  );
  vi.stubGlobal("fetch", fetcher);
  expect(
    await staffSetup("password", {
      invitation_id: invitation,
      ticket,
      code: "opaque+Code/==#",
      password: "private-password",
    }),
  ).toEqual({ state: "complete" });
  const [path, request] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(path).toBe("/api/v1/auth/staff-setup/password");
  expect(request.method).toBe("POST");
  expect(request.cache).toBe("no-store");
  expect(JSON.parse(request.body as string).invitation_id).toBe(invitation);
});

it.each([
  { state: "owner" },
  { state: "email_action_required" },
  { state: "complete", password: "unexpected" },
  {
    state: "email_action_required",
    invitation_id: "019a8888-8888-7888-8888-888888888888",
    phase: "email",
    expires_at: "2026-11-13T12:00:00Z",
  },
])("rejects an invalid or differently bound owner response", async (receipt) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify(receipt), { status: 200 })),
  );
  await expect(staffSetup("status", { invitation_id: invitation, ticket })).rejects.toThrow();
});
