import { afterEach, expect, it, vi } from "vitest";
import { getAccountChoices, requestAccountCode, verifyAccountCode } from "./account-discovery";

afterEach(() => vi.unstubAllGlobals());
it("uses only the same-origin Global API and no-store responses for discovery", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(Response.json({ state: "check_email" }, { status: 202 }))
    .mockResolvedValueOnce(Response.json({ state: "verified" }))
    .mockResolvedValueOnce(Response.json({ accounts: [] }));
  vi.stubGlobal("fetch", fetcher);
  await requestAccountCode("owner@example.test");
  await verifyAccountCode("12345678");
  expect(await getAccountChoices()).toEqual({ accounts: [] });
  expect(fetcher.mock.calls.map(([url]) => url)).toEqual([
    "/api/v1/auth/accounts",
    "/api/v1/auth/accounts/verify",
    "/api/v1/auth/accounts",
  ]);
  for (const [, init] of fetcher.mock.calls) expect(init.cache).toBe("no-store");
  expect(JSON.parse(fetcher.mock.calls[0][1].body)).toEqual({ email: "owner@example.test" });
});
it("rejects malformed public proofs before making a request", async () => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  await expect(requestAccountCode("invalid")).rejects.toThrow();
  await expect(verifyAccountCode("1234")).rejects.toThrow();
  expect(fetcher).not.toHaveBeenCalled();
});
it("rejects oversized account lists and secret-bearing response fields", async () => {
  const account = {
    tenantId: "11111111-1111-4111-8111-111111111111",
    name: "Hospital",
    account: "owner@example.test",
  };
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(Response.json({ accounts: Array.from({ length: 21 }, () => account) }))
    .mockResolvedValueOnce(Response.json({ accounts: [{ ...account, password: "unexpected" }] }));
  vi.stubGlobal("fetch", fetcher);
  await expect(getAccountChoices()).rejects.toThrow();
  await expect(getAccountChoices()).rejects.toThrow();
});

it.each(["अ", "ଅ", "😀"])("preserves the canonical Unicode name limit for %s", async (letter) => {
  const account = {
    tenantId: "11111111-1111-4111-8111-111111111111",
    name: letter.repeat(200),
    account: "owner@example.test",
  };
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValueOnce(Response.json({ accounts: [account] }))
      .mockResolvedValueOnce(
        Response.json({ accounts: [{ ...account, name: letter.repeat(201) }] }),
      ),
  );
  expect((await getAccountChoices()).accounts[0].name).toBe(account.name);
  await expect(getAccountChoices()).rejects.toThrow();
});
