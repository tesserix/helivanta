import { NavigationGuardProvider } from "@helivanta/ui";
import { chooseOption } from "@/test/select";
import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import Workforce from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
it("lets a hospital manager without static permissions save basic staff", async () => {
  let saved: Record<string, unknown> | null = null;
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    let body: unknown = { data: [] };
    if (url.includes("/iam/me/permissions"))
      body = {
        tenant_id: hospital,
        subject: "synthetic-staff-admin",
        data: [],
      };
    if (url.endsWith("/workforce/hospitals"))
      body = page([{ id: hospital, name: "YES HOSPITAL", access_level: "manager" }]);
    if (url.includes("/staff")) body = page(saved ? [{ id: "staff-1", ...saved }] : []);
    if (init?.method === "PUT") {
      saved = JSON.parse(String(init.body));
      body = {
        id: url.split("/staff/")[1]?.split("?")[0],
        hospital_id: hospital,
        employee_code: saved?.employee_code,
        display_name: saved?.display_name,
        job_category: saved?.job_category,
        employment_status: saved?.employment_status,
        created_at: "2026-10-29T00:00:00Z",
        updated_at: "2026-10-29T00:00:00Z",
        version: 1,
        directory_context_version: "v1",
      };
    }
    return new Response(JSON.stringify(body), { status: 200 });
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Workforce />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Add staff" }));
  expect(screen.queryByLabelText("Given name")).not.toBeInTheDocument();
  await user.type(screen.getByLabelText("Full name"), "Synthetic Nurse");
  await user.type(screen.getByLabelText("Employee code"), "YES-101");
  await chooseOption(user, screen.getByLabelText("Job category"), "Nurse");
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  await waitFor(() =>
    expect(fetcher).toHaveBeenCalledWith(
      expect.stringContaining(`/hospitals/${hospital}/staff/`),
      expect.objectContaining({ method: "PUT" }),
    ),
  );
  expect(saved).toEqual({
    display_name: "Synthetic Nurse",
    employee_code: "YES-101",
    job_category: "nurse",
    employment_status: "active",
    expected_version: 0,
    directory_context_version: "v1",
  });
  expect(await screen.findByText("Synthetic Nurse")).toBeInTheDocument();
});
it("shows an actionable API failure rather than an empty directory", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url.includes("/iam/me/permissions")
        ? Response.json({
            tenant_id: hospital,
            subject: "synthetic-staff-admin",
            data: [],
          })
        : new Response(JSON.stringify({ message: "Unavailable" }), { status: 503 }),
    ),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <Workforce />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not load hospitals");
  expect(screen.queryByRole("button", { name: "Add staff" })).not.toBeInTheDocument();
});

it("lets a tenant administrator explicitly create and grant their first hospital", async () => {
  const requests: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") requests.push(url);
      const body = url.includes("permissions")
        ? { data: ["workforce.hospital.manage"], subject: "owner-1", tenant_id: hospital }
        : url.includes("hospitals")
          ? page([])
          : {};
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Workforce />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByText("Add a hospital"));
  await user.type(screen.getByLabelText("Hospital name"), "YES Main Hospital");
  await user.click(screen.getByLabelText("Grant me staff manager access to this hospital"));
  await user.click(screen.getByRole("button", { name: "Create hospital" }));
  await waitFor(() => expect(requests).toHaveLength(2));
  expect(requests[1]).toMatch(/\/access\/owner-1$/);
});

it("offers identity detail checks to directory readers without enabling editing", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("/iam/me/permissions"))
        return Response.json({ tenant_id: hospital, subject: "scoped-manager", data: [] });
      if (url.includes("/staff"))
        return Response.json(
          page([
            {
              id: "33333333-3333-4333-8333-333333333333",
              employee_code: "SYN-READER",
              display_name: "Synthetic Reader Target",
              job_category: "other",
              employment_status: "active",
            },
          ]),
        );
      return Response.json(
        page([{ id: hospital, name: "Synthetic Reader Hospital", access_level: "reader" }]),
      );
    }),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <Workforce />
    </NavigationGuardProvider>,
  );
  expect(
    await screen.findByRole("button", { name: "Identity details for Synthetic Reader Target" }),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Edit Synthetic Reader Target" }),
  ).not.toBeInTheDocument();
});

it("opens scoped HR onboarding without a manager grant or tenant-wide business permissions", async () => {
  const staff = "33333333-3333-4333-8333-333333333333";
  const fetcher = vi.fn(async (url: string) => {
    if (url.includes("/iam/me/permissions"))
      return Response.json({ tenant_id: hospital, subject: "synthetic-scoped-hr", data: [] });
    if (url.includes("/workforce/hr-intake-scopes?"))
      return Response.json(
        page([
          {
            hospital_id: hospital,
            name: "Synthetic Scoped HR Hospital",
            can_prepare_staff: true,
            can_manage_staff_documents: true,
          },
        ]),
      );
    if (url.includes(`/hospitals/${hospital}/staff?`))
      return Response.json(
        page([
          {
            id: staff,
            hospital_id: hospital,
            employee_code: "SYN-INTAKE",
            display_name: "Synthetic Intake Target",
            job_category: "other",
            employment_status: "inactive",
          },
        ]),
      );
    return Response.json(page([]));
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Workforce />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByText("Synthetic Intake Target")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Edit Synthetic Intake Target" })).toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "Documents for Synthetic Intake Target" }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "Employment terms for Synthetic Intake Target" }),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Credentials for Synthetic Intake Target" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Account link for Synthetic Intake Target" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Identity details for Synthetic Intake Target" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Add staff" }));
  expect(await screen.findByLabelText("Given name")).toBeInTheDocument();
  expect(screen.getByLabelText("Employment status")).toBeDisabled();
  expect(screen.getByLabelText("Employment status")).toHaveTextContent("Inactive");
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([url]) => url.includes("/hr/masters?hr_intake=v1"))).toBe(true),
  );
  const protectedReads = fetcher.mock.calls.filter(
    ([url]) => url.includes(`/hospitals/${hospital}/staff?`) || url.includes("/hr/masters?"),
  );
  for (const [url, init] of protectedReads as unknown as Array<[string, RequestInit]>) {
    expect(new URL(url, "https://example.invalid").searchParams.get("hr_intake")).toBe("v1");
    expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(hospital);
    expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(
      "synthetic-scoped-hr",
    );
  }
});

it("reports unavailable onboarding authority instead of a successful empty hospital list", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("/iam/me/permissions"))
        return Response.json({ tenant_id: hospital, subject: "synthetic-scoped-hr", data: [] });
      if (url.includes("/hr-intake-scopes?"))
        return Response.json(
          { error: "scoped_authorization_unavailable", message: "Synthetic authority unavailable" },
          { status: 503 },
        );
      return Response.json(page([]));
    }),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <Workforce />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Could not verify Staff onboarding hospitals",
  );
  expect(screen.queryByText("No hospital access yet")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add staff" })).not.toBeInTheDocument();
});

it("clears a scoped HR directory when the owner denies the protected profile read", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("/iam/me/permissions"))
        return Response.json({ tenant_id: hospital, subject: "synthetic-scoped-hr", data: [] });
      if (url.includes("/hr-intake-scopes?"))
        return Response.json(
          page([
            {
              hospital_id: hospital,
              name: "Synthetic Scoped HR Hospital",
              can_prepare_staff: true,
              can_manage_staff_documents: true,
            },
          ]),
        );
      if (url.includes("/enrollment?"))
        return Response.json(
          { error: "forbidden", message: "Synthetic reviewed actor cutoff" },
          { status: 403 },
        );
      if (url.includes(`/hospitals/${hospital}/staff?`))
        return Response.json(
          page([
            {
              id: "33333333-3333-4333-8333-333333333333",
              hospital_id: hospital,
              employee_code: "SYN-PRIVATE",
              display_name: "Synthetic Private Intake Staff",
              job_category: "other",
              employment_status: "inactive",
            },
          ]),
        );
      return Response.json(page([]));
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Workforce />
    </NavigationGuardProvider>,
  );
  await user.click(
    await screen.findByRole("button", { name: "Edit Synthetic Private Intake Staff" }),
  );
  expect(await screen.findByRole("alert")).toHaveTextContent("Your account or workspace changed");
  expect(screen.queryByText("Synthetic Private Intake Staff")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("Given name")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add staff" })).not.toBeInTheDocument();
});
