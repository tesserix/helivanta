import { afterEach, expect, it, vi } from "vitest";
import { getCustomerSignup, startCustomerSignup } from "./customer-signup";

afterEach(() => vi.unstubAllGlobals());

it("sends only canonical new-owner intent through the same-origin Global API", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(new Response(JSON.stringify({ state: "provisioning" }), { status: 202 }));
  vi.stubGlobal("fetch", fetcher);
  const input = {
    name: "TEST Hospital",
    kind: "hospital" as const,
    countryCode: "IN" as const,
    stateCode: "MH" as const,
    owner: { givenName: "Test", familyName: "Owner", email: "owner@example.test" },
  };
  expect(await startCustomerSignup(input)).toEqual({ state: "provisioning" });
  expect(fetcher).toHaveBeenCalledWith(
    "/api/v1/auth/signup",
    expect.objectContaining({ method: "POST", body: JSON.stringify(input), cache: "no-store" }),
  );
  const malicious = { ...input, tenantId: "foreign" };
  expect(() => startCustomerSignup(malicious)).toThrow();
  expect(fetcher).toHaveBeenCalledTimes(1);
});

it("refuses a ready receipt that has no completed tenant and business resource", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(new Response(JSON.stringify({ state: "ready" }), { status: 200 })),
  );
  await expect(getCustomerSignup()).rejects.toThrow();
});
