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
  const { user } = renderWithProviders(<Workforce />);
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
  renderWithProviders(<Workforce />);
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
  const { user } = renderWithProviders(<Workforce />);
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
  renderWithProviders(<Workforce />);
  expect(
    await screen.findByRole("button", { name: "Identity details for Synthetic Reader Target" }),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Edit Synthetic Reader Target" }),
  ).not.toBeInTheDocument();
});
