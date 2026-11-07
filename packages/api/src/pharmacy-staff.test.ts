import { afterEach, expect, it, vi } from "vitest";
import {
  getPharmacyStaffRecord,
  grantPharmacyManagerAccess,
  listPharmacyStaffPage,
  listPharmacyStaffSourcesPage,
  savePharmacyStaffRecord,
} from "./pharmacy-staff";

const pharmacy = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const other = "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb";
const staffId = "019ccccc-cccc-7ccc-8ccc-cccccccccccc";
const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "user-admin" };
const row = {
  id: staffId,
  pharmacy_id: pharmacy,
  employee_code: "SYN-PH-1",
  display_name: "Synthetic pharmacist",
  job_category: "pharmacist",
  employment_status: "active",
  created_at: "2026-11-07T00:00:00Z",
  updated_at: "2026-11-07T00:00:00Z",
};
const record = { ...row, version: 2, directory_context_version: "v1" };
const input = {
  employee_code: "SYN-PH-1",
  display_name: "Synthetic pharmacist",
  job_category: "pharmacist",
  employment_status: "active",
} as const;
const page = (data: unknown[], next: string | null = null) => ({
  data,
  page: { has_more: next !== null, next_cursor: next },
});
const respond = (body: unknown) => {
  const fetcher = vi.fn().mockResolvedValue(Response.json(body));
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
};
afterEach(() => vi.unstubAllGlobals());

it("lists the caller's pharmacies with their access level under the expected account", async () => {
  const source = {
    pharmacy_id: pharmacy,
    name: "Synthetic Pharmacy",
    access_level: "manager",
    created_at: "2026-11-07T00:00:00Z",
  };
  const fetcher = respond(page([source]));
  expect((await listPharmacyStaffSourcesPage(scope)).data).toEqual([source]);
  expect(fetcher.mock.calls[0]?.[0]).toBe("/api/v1/workforce/pharmacies?limit=50");
  const init = fetcher.mock.calls[0]?.[1];
  expect(init).toMatchObject({ cache: "no-store" });
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  respond(page([source, source]));
  await expect(listPharmacyStaffSourcesPage(scope)).rejects.toThrow();
  respond({ ...page([source]), page: { has_more: true, next_cursor: null } });
  await expect(listPharmacyStaffSourcesPage(scope)).rejects.toThrow();
  respond(page([{ ...source, hospital_id: pharmacy }]));
  await expect(listPharmacyStaffSourcesPage(scope)).rejects.toThrow();
});

it("lists one pharmacy's Staff and refuses a row from another establishment", async () => {
  const fetcher = respond(page([row], "next"));
  expect((await listPharmacyStaffPage(scope, pharmacy.toUpperCase())).data).toEqual([row]);
  expect(fetcher.mock.calls[0]?.[0]).toBe(
    `/api/v1/workforce/pharmacies/${pharmacy}/staff?limit=50`,
  );
  respond(page([{ ...row, pharmacy_id: other }]));
  await expect(listPharmacyStaffPage(scope, pharmacy)).rejects.toThrow(
    "The Staff source does not match the requested directory.",
  );
  respond(page([{ ...row, hospital_id: pharmacy }]));
  await expect(listPharmacyStaffPage(scope, pharmacy)).rejects.toThrow();
  respond(page([row, row]));
  await expect(listPharmacyStaffPage(scope, pharmacy)).rejects.toThrow();
  await expect(listPharmacyStaffPage(scope, "not-a-pharmacy")).rejects.toThrow();
});

it("reads the versioned record and refuses another record or pharmacy", async () => {
  const fetcher = respond(record);
  expect(await getPharmacyStaffRecord(scope, pharmacy, staffId)).toEqual(record);
  expect(fetcher.mock.calls[0]?.[0]).toBe(
    `/api/v1/workforce/pharmacies/${pharmacy}/staff/${staffId}?directory_context=v1`,
  );
  respond({ ...record, pharmacy_id: other });
  await expect(getPharmacyStaffRecord(scope, pharmacy, staffId)).rejects.toThrow();
  respond({ ...record, id: other });
  await expect(getPharmacyStaffRecord(scope, pharmacy, staffId)).rejects.toThrow();
  respond({ ...row });
  await expect(getPharmacyStaffRecord(scope, pharmacy, staffId)).rejects.toThrow();
});

it("saves against the expected version and verifies what was stored", async () => {
  const fetcher = respond(record);
  expect(await savePharmacyStaffRecord(scope, pharmacy, staffId, input, 1)).toEqual(record);
  expect(fetcher.mock.calls[0]?.[1]).toMatchObject({ method: "PUT", cache: "no-store" });
  expect(JSON.parse(fetcher.mock.calls[0]?.[1].body)).toEqual({
    ...input,
    expected_version: 1,
    directory_context_version: "v1",
  });
  for (const stored of [
    { ...record, pharmacy_id: other },
    { ...record, version: 1 },
    { ...record, display_name: "Another name" },
  ]) {
    respond(stored);
    await expect(savePharmacyStaffRecord(scope, pharmacy, staffId, input, 2)).rejects.toThrow(
      "Could not verify the saved Staff record.",
    );
  }
});

it("accepts an unchanged save, which leaves the stored version as it was", async () => {
  respond(record);
  expect(await savePharmacyStaffRecord(scope, pharmacy, staffId, input, 2)).toEqual(record);
});

it("grants the caller manager access and verifies the grant", async () => {
  const grant = { pharmacy_id: pharmacy, subject: "user a/b", level: "manager" };
  const fetcher = respond(grant);
  expect(await grantPharmacyManagerAccess(pharmacy, "user a/b")).toEqual(grant);
  expect(fetcher.mock.calls[0]?.[0]).toBe(
    `/api/v1/workforce/pharmacies/${pharmacy}/access/user%20a%2Fb`,
  );
  expect(fetcher.mock.calls[0]?.[1]).toMatchObject({
    method: "PUT",
    body: '{"level":"manager"}',
    cache: "no-store",
  });
  for (const stored of [
    { ...grant, level: "reader" },
    { ...grant, subject: "user-other" },
    { ...grant, pharmacy_id: other },
  ]) {
    respond(stored);
    await expect(grantPharmacyManagerAccess(pharmacy, "user a/b")).rejects.toThrow(
      "The Staff access grant could not be verified.",
    );
  }
});
