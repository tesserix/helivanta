import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import {
  pharmacyEnrollmentKey,
  usePharmacyEnrollment,
  usePharmacySupervisorChoices,
} from "./pharmacy-enrollment-queries";

const pharmacy = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const staffId = "019ccccc-cccc-7ccc-8ccc-cccccccccccc";
const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "user-admin" };
const document = {
  staff: {
    id: staffId,
    pharmacy_id: pharmacy,
    employee_code: "SYN-PH-1",
    display_name: "Synthetic pharmacist",
    job_category: "pharmacist",
    employment_status: "active",
    created_at: "2026-11-08T00:00:00Z",
    updated_at: "2026-11-08T00:00:00Z",
  },
  version: 1,
  request_context_version: "v1",
  profile: {
    given_name: "",
    family_name: "",
    preferred_name: "",
    date_of_birth: "",
    gender: "",
    work_email: "",
    personal_email: "",
    mobile_phone: "",
    work_phone: "",
    preferred_language: "",
    blood_group: "",
    engagement_type: "employee",
    department: "",
    designation: "",
    grade: "",
    department_revision_id: "",
    designation_revision_id: "",
    grade_revision_id: "",
    supervisor_id: "",
    start_date: "",
    end_date: "",
    probation_end_date: "",
    confirmation_date: "",
    exit_reason: "",
  },
  addresses: [],
  emergency_contacts: [],
  credentials: [],
  qualifications: [],
  experiences: [],
};
afterEach(() => vi.unstubAllGlobals());

it("keys the enrollment by account, pharmacy and Staff record", () => {
  const key = pharmacyEnrollmentKey(scope, pharmacy.toUpperCase(), staffId.toUpperCase());
  expect(key).toEqual([
    "workforce",
    "pharmacy-enrollment",
    scope.tenantId,
    scope.subject,
    pharmacy,
    staffId,
  ]);
  for (const changed of [
    pharmacyEnrollmentKey({ ...scope, subject: "user-other" }, pharmacy, staffId),
    pharmacyEnrollmentKey({ ...scope, tenantId: pharmacy }, pharmacy, staffId),
    pharmacyEnrollmentKey(scope, staffId, staffId),
    pharmacyEnrollmentKey(scope, pharmacy, pharmacy),
  ])
    expect(changed).not.toEqual(key);
});

it("reads once per attempt without caching, retrying or refetching on focus", async () => {
  const fetcher = vi.fn().mockImplementation(() => Promise.resolve(Response.json(document)));
  vi.stubGlobal("fetch", fetcher);
  const client = new QueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const { result, rerender } = renderHook(
    ({ attempt }) => usePharmacyEnrollment(scope, pharmacy, staffId, attempt),
    { wrapper, initialProps: { attempt: 0 } },
  );
  await waitFor(() => expect(result.current.data?.version).toBe(1));
  const query = client
    .getQueryCache()
    .find({ queryKey: [...pharmacyEnrollmentKey(scope, pharmacy, staffId), 0] });
  expect(query?.options).toMatchObject({ gcTime: 0, retry: false, refetchOnWindowFocus: false });
  expect(fetcher.mock.calls[0]?.[1]).toMatchObject({ cache: "no-store" });
  rerender({ attempt: 1 });
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));

  fetcher.mockImplementation(() =>
    Promise.resolve(
      Response.json({ error: "not_found", message: "staff not found" }, { status: 404 }),
    ),
  );
  rerender({ attempt: 2 });
  await waitFor(() => expect(result.current.isError).toBe(true));
  expect(fetcher).toHaveBeenCalledTimes(3);
});

it("accumulates this pharmacy's validated Staff pages as supervisor choices", async () => {
  const row = (id: string, pharmacy_id = pharmacy) => ({ ...document.staff, id, pharmacy_id });
  const second = "019ddddd-dddd-7ddd-8ddd-dddddddddddd";
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      Response.json({ data: [row(staffId)], page: { has_more: true, next_cursor: "c2" } }),
    )
    .mockResolvedValueOnce(
      Response.json({ data: [row(second)], page: { has_more: false, next_cursor: null } }),
    );
  vi.stubGlobal("fetch", fetcher);
  const client = new QueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const { result } = renderHook(() => usePharmacySupervisorChoices(scope, pharmacy), { wrapper });
  await waitFor(() => expect(result.current.hasMore).toBe(true));
  expect(fetcher.mock.calls[0]?.[0]).toBe(
    `/api/v1/workforce/pharmacies/${pharmacy}/staff?limit=50`,
  );
  expect(new Headers(fetcher.mock.calls[0]?.[1].headers).get("X-Helivanta-Expected-Subject")).toBe(
    scope.subject,
  );
  act(() => result.current.loadMore());
  await waitFor(() =>
    expect(result.current.items.map((item) => item.id)).toEqual([staffId, second]),
  );
  expect(fetcher.mock.calls[1]?.[0]).toContain("cursor=c2");
  expect(client.getQueryCache().getAll()[0]?.options).toMatchObject({ gcTime: 0, retry: false });

  fetcher.mockResolvedValueOnce(
    Response.json({ data: [row(staffId, staffId)], page: { has_more: false, next_cursor: null } }),
  );
  const foreign = renderHook(
    () => usePharmacySupervisorChoices({ ...scope, subject: "user-b" }, pharmacy),
    {
      wrapper,
    },
  );
  await waitFor(() => expect(foreign.result.current.isError).toBe(true));
  expect(foreign.result.current.items).toEqual([]);
  expect(fetcher).toHaveBeenCalledTimes(3);
});
