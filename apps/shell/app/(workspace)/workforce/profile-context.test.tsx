import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { chooseOption } from "@/test/select";
import { renderWithProviders } from "@helivanta/api/testing";
import { EMPTY_PROFILE } from "@helivanta/api";
import Workforce from "./page";

const tenant = "11111111-1111-4111-8111-111111111111";
const hospital = "22222222-2222-4222-8222-222222222222";
const employee = {
  id: "33333333-3333-4333-8333-333333333333",
  employee_code: "SYN-PROFILE-EMPLOYEE",
  display_name: "Synthetic profile employee",
  job_category: "other",
  employment_status: "active",
};
const supervisor = {
  ...employee,
  id: "44444444-4444-4444-8444-444444444444",
  employee_code: "SYN-PROFILE-SUPERVISOR",
  display_name: "Synthetic supervisor",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
function setup(
  options: {
    context?: boolean;
    wrongRecord?: boolean;
    code?: string;
    permissions?: string[];
    choiceCode?: string;
    inactiveSupervisor?: boolean;
    status?: number;
  } = {},
) {
  const requests: Array<{
    url: string;
    method: string;
    headers: Headers;
    body?: unknown;
    cache?: RequestCache;
  }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      requests.push({
        url,
        method: init?.method ?? "GET",
        headers: new Headers(init?.headers),
        body: init?.body ? JSON.parse(String(init.body)) : undefined,
        cache: init?.cache,
      });
      if (url.includes("/iam/me/permissions"))
        return Response.json({
          tenant_id: tenant,
          subject: "profile-admin",
          data: options.permissions ?? ["workforce.enrollment.manage"],
        });
      if (url.includes("/enrollment")) {
        if (init?.method === "PUT")
          return Response.json(
            {
              error: options.code ?? "unavailable",
              message:
                options.code === "session_changed"
                  ? "Your signed-in account changed."
                  : "Synthetic failure",
            },
            { status: options.status ?? (options.code ? 409 : 503) },
          );
        return Response.json({
          staff: options.wrongRecord ? supervisor : employee,
          version: 1,
          profile: { ...EMPTY_PROFILE, given_name: "Private synthetic name" },
          addresses: [],
          emergency_contacts: [],
          credentials: [],
          qualifications: [],
          experiences: [],
          profile_masters_version: "v1",
          qualification_masters_version: "v1",
          contact_masters_version: "v1",
          ...(options.context === false ? {} : { request_context_version: "v1" }),
        });
      }
      if (url.includes("/hr/masters"))
        return options.choiceCode
          ? Response.json({ error: options.choiceCode }, { status: 409 })
          : Response.json(page([]));
      if (url.endsWith("/hospitals"))
        return Response.json(
          page([{ id: hospital, name: "Synthetic profile hospital", access_level: "manager" }]),
        );
      return Response.json(
        page([
          employee,
          { ...supervisor, employment_status: options.inactiveSupervisor ? "inactive" : "active" },
        ]),
      );
    }),
  );
  return requests;
}
it("pins private profile and supervisor choice requests to the opening account", async () => {
  const requests = setup();
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic profile employee" }));
  await chooseOption(
    user,
    await screen.findByLabelText("Supervisor"),
    "Synthetic supervisor · SYN-PROFILE-SUPERVISOR",
  );
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  await waitFor(() => expect(requests.filter((r) => r.method === "PUT")).toHaveLength(1));
  const writes = requests.filter((r) => r.method === "PUT");
  expect(writes[0]?.headers.get("X-Helivanta-Expected-Tenant")).toBe(tenant);
  expect(writes[0]?.headers.get("X-Helivanta-Expected-Subject")).toBe("profile-admin");
  expect(writes[0]?.url).toContain("request_context=v1");
  expect(writes[0]?.body).toMatchObject({
    request_context_version: "v1",
    expected_version: 1,
    profile: { supervisor_id: supervisor.id },
  });
  const reads = requests.filter((r) => r.url.includes("/enrollment") && r.method === "GET");
  expect(reads[0]?.headers.get("X-Helivanta-Expected-Subject")).toBe("profile-admin");
  expect(reads[0]?.cache).toBe("no-store");
});
it("removes private form values when the signed-in account changes during save", async () => {
  setup({ code: "session_changed" });
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic profile employee" }));
  expect(await screen.findByLabelText("Given name")).toHaveValue("Private synthetic name");
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  await screen.findByText(/account or workspace changed/i);
  expect(screen.queryByDisplayValue("Private synthetic name")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Save staff" })).not.toBeInTheDocument();
});
it.each([{ context: false }, { wrongRecord: true }])(
  "keeps editing closed without a matching context acknowledgment and record: %j",
  async (options) => {
    setup(options);
    const { user } = renderWithProviders(<Workforce />);
    await user.click(
      await screen.findByRole("button", { name: "Edit Synthetic profile employee" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(/not available|could not verify/i);
    expect(screen.queryByRole("button", { name: "Save staff" })).not.toBeInTheDocument();
  },
);
it("lets a hospital manager edit directory fields without private enrollment access", async () => {
  const requests = setup({ permissions: [] });
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic profile employee" }));
  expect(screen.getByLabelText("Full name")).toHaveValue(employee.display_name);
  expect(screen.queryByLabelText("Given name")).not.toBeInTheDocument();
  expect(requests.some((request) => request.url.includes("/enrollment"))).toBe(false);
  expect(screen.getByRole("button", { name: "Save staff" })).toBeInTheDocument();
});

it("closes protected editing when choice loading detects an account change", async () => {
  setup({ choiceCode: "session_changed" });
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic profile employee" }));
  await screen.findByText(/account or workspace changed/i);
  expect(screen.queryByDisplayValue("Private synthetic name")).not.toBeInTheDocument();
});
it("labels inactive supervisors without silently excluding recorded relationships", async () => {
  setup({ inactiveSupervisor: true });
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic profile employee" }));
  await chooseOption(
    user,
    await screen.findByLabelText("Supervisor"),
    "Synthetic supervisor · SYN-PROFILE-SUPERVISOR · Inactive",
  );
  expect(screen.getByLabelText("Supervisor")).toHaveTextContent("Inactive");
});
it.each([401, 403, 404])(
  "removes private values when save loses access with %i",
  async (status) => {
    setup({ status });
    const { user } = renderWithProviders(<Workforce />);
    await user.click(
      await screen.findByRole("button", { name: "Edit Synthetic profile employee" }),
    );
    await screen.findByLabelText("Given name");
    await user.click(screen.getByRole("button", { name: "Save staff" }));
    await screen.findByText(/account or workspace changed/i);
    expect(screen.queryByDisplayValue("Private synthetic name")).not.toBeInTheDocument();
  },
);
it.each([{ status: 409, code: "version_conflict" }, { status: 503 }])(
  "retains the draft after an ordinary conflict or temporary failure: %j",
  async (options) => {
    setup(options);
    const { user } = renderWithProviders(<Workforce />);
    await user.click(
      await screen.findByRole("button", { name: "Edit Synthetic profile employee" }),
    );
    const field = await screen.findByLabelText("Given name");
    await user.clear(field);
    await user.type(field, "Synthetic unsaved draft");
    await user.click(screen.getByRole("button", { name: "Save staff" }));
    await screen.findByRole("alert");
    expect(screen.getByLabelText("Given name")).toHaveValue("Synthetic unsaved draft");
    expect(screen.getByRole("button", { name: "Save staff" })).toBeEnabled();
  },
);

it("preserves document management and basic editing without private enrollment access", async () => {
  const requests = setup({ permissions: ["workforce.documents.manage"] });
  const { user } = renderWithProviders(<Workforce />);
  expect(
    await screen.findByRole("button", { name: "Documents for Synthetic profile employee" }),
  ).toBeEnabled();
  await user.click(screen.getByRole("button", { name: "Edit Synthetic profile employee" }));
  expect(screen.getByLabelText("Full name")).toHaveValue(employee.display_name);
  expect(screen.queryByLabelText("Given name")).not.toBeInTheDocument();
  expect(requests.some((request) => request.url.includes("/enrollment"))).toBe(false);
});
