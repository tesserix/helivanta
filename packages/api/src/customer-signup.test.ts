import { afterEach, expect, it, vi } from "vitest";
import {
  getCustomerSignup,
  startCustomerSignup,
  getCustomerBusinessProfile,
  continueCustomerSignup,
  CustomerBusinessAddressContract,
  CustomerOrganizationProfileContract,
} from "./customer-signup";

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

it("loads owner business details through a tenant-bound Global API request", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(new Response(JSON.stringify({ profile: null }), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  const tenant = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
  const id = "0199e000-0000-7000-8000-000000000010";
  expect(await getCustomerBusinessProfile(tenant, "pharmacy", id)).toEqual({ profile: null });
  expect(fetcher).toHaveBeenCalledWith(
    `/api/v1/pharmacy/pharmacies/${id}/business-profile`,
    expect.objectContaining({
      cache: "no-store",
    }),
  );
  expect(new Headers(fetcher.mock.calls[0][1].headers).get("X-Helivanta-Expected-Tenant")).toBe(
    tenant,
  );
  fetcher.mockResolvedValue(new Response(JSON.stringify({}), { status: 200 }));
  await expect(getCustomerBusinessProfile(tenant, "hospital", id)).rejects.toThrow();
});

it("counts address lengths in Unicode code points and rejects unclean contacts", () => {
  const address = {
    line1: "10 Test Road",
    line2: "",
    city: "😀".repeat(100),
    district: "Pune",
    postalCode: "411001",
    stateCode: "MH",
    countryCode: "IN",
  };
  expect(CustomerBusinessAddressContract.safeParse(address).success).toBe(true);
  expect(
    CustomerBusinessAddressContract.safeParse({ ...address, city: address.city + "😀" }).success,
  ).toBe(false);
  expect(
    CustomerBusinessAddressContract.safeParse({ ...address, line1: "\ufeff10 Test Road" }).success,
  ).toBe(false);
  const profile = {
    contact: { phone: "+919876543210", email: "office@example" },
    headOffice: address,
    mainBranch: { name: "Main branch", address },
  };
  expect(CustomerOrganizationProfileContract.safeParse(profile).success).toBe(false);
  expect(
    CustomerOrganizationProfileContract.safeParse({
      ...profile,
      contact: { ...profile.contact, email: "office@example.test" },
    }).success,
  ).toBe(true);
});

it("submits native signup authentication through the receipt-bound Global API", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(new Response(JSON.stringify({ state: "provisioning" }), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  expect(await continueCustomerSignup({ idToken: "native-proof" })).toEqual({
    state: "provisioning",
  });
  expect(fetcher).toHaveBeenCalledWith(
    "/api/v1/auth/signup/sign-in",
    expect.objectContaining({
      method: "POST",
      body: JSON.stringify({ idToken: "native-proof" }),
      cache: "no-store",
    }),
  );
  expect(() =>
    continueCustomerSignup({ idToken: "native-proof", subject: "foreign" } as never),
  ).toThrow();
  expect(fetcher).toHaveBeenCalledTimes(1);
});
