import { afterEach, expect, it, vi } from "vitest";
import { ApiError } from "./client";
import {
  getPharmacyEmployer,
  pharmacyEmployerPath,
  registerPharmacyEmployer,
} from "./pharmacy-employers";

const pharmacy = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const employer = {
  pharmacy_id: pharmacy,
  name: "Synthetic Independent Pharmacy",
  ownership_type: "independent",
  created_at: "2026-11-05T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());

it("registers with an empty body and returns only the verified registration", async () => {
  const fetcher = vi.fn().mockResolvedValue(Response.json(employer));
  vi.stubGlobal("fetch", fetcher);
  expect(await registerPharmacyEmployer(pharmacy.toUpperCase())).toEqual(employer);
  expect(fetcher.mock.calls[0]?.[0]).toBe(`/api/v1/workforce/pharmacies/${pharmacy}`);
  expect(fetcher.mock.calls[0]?.[1]).toMatchObject({
    method: "PUT",
    body: "{}",
    cache: "no-store",
  });
});

it("reads a registration and treats 404 as not registered", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(employer)));
  expect(await getPharmacyEmployer(pharmacy)).toEqual(employer);
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        Response.json({ error: "not_found", message: "pharmacy not found" }, { status: 404 }),
      ),
  );
  expect(await getPharmacyEmployer(pharmacy)).toBeNull();
});

it("surfaces every other failure rather than reporting not registered", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        Response.json(
          { error: "authz_unavailable", message: "authorization is temporarily unavailable" },
          { status: 503 },
        ),
      ),
  );
  await expect(getPharmacyEmployer(pharmacy)).rejects.toMatchObject({ status: 503 });
  await expect(getPharmacyEmployer(pharmacy)).rejects.toBeInstanceOf(ApiError);
});

it.each([
  ["another pharmacy", { ...employer, pharmacy_id: "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb" }],
  ["hospital-owned ownership", { ...employer, ownership_type: "hospital_owned" }],
  ["an unexpected field", { ...employer, establishment_kind: "pharmacy" }],
  ["a missing creation time", { ...employer, created_at: undefined }],
])("refuses a response naming %s", async (_, body) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(body)));
  await expect(registerPharmacyEmployer(pharmacy)).rejects.toThrow();
  await expect(getPharmacyEmployer(pharmacy)).rejects.toThrow();
});

it("refuses an identifier that cannot be a pharmacy before any request", () => {
  for (const id of ["", "not-an-id", "22222222-2222-4222-8222-222222222222", `${pharmacy}/x`])
    expect(() => pharmacyEmployerPath(id)).toThrow();
});
