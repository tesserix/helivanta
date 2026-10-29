import { NavigationGuardProvider } from "@helivanta/ui";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import Workforce from "./page";

const tenant = "11111111-1111-4111-8111-111111111111";
const hospital = "22222222-2222-4222-8222-222222222222";
const id = "33333333-3333-4333-8333-333333333333";
const row = {
  id,
  hospital_id: hospital,
  employee_code: "SYN-SAFE",
  display_name: "Synthetic current worker",
  job_category: "other",
  employment_status: "active",
  created_at: "2026-10-29T00:00:00Z",
  updated_at: "2026-10-29T00:00:00Z",
  version: 7,
  directory_context_version: "v1",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
function setup(
  options: {
    unacknowledged?: boolean;
    error?: string;
    readError?: string;
    wrongHospital?: boolean;
  } = {},
) {
  const writes: Array<{ url: string; headers: Headers; body: Record<string, unknown> }> = [];
  let current = { ...row };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("/iam/me/permissions"))
        return Response.json({ tenant_id: tenant, subject: "basic-manager", data: [] });
      if (url.endsWith("/hospitals"))
        return Response.json(
          page([{ id: hospital, name: "Synthetic safe hospital", access_level: "manager" }]),
        );
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body));
        writes.push({ url, headers: new Headers(init.headers), body });
        if (options.error) {
          current = {
            ...current,
            display_name: "Synthetic concurrent worker",
            employment_status: "inactive",
            version: 8,
          };
          return Response.json(
            { error: options.error, message: "Staff record changed. Your draft has been kept." },
            { status: options.error === "unavailable" ? 503 : 409 },
          );
        }
        return Response.json({
          ...current,
          display_name: body.display_name,
          employee_code: body.employee_code,
          job_category: body.job_category,
          employment_status: body.employment_status,
          id: url.split("/staff/")[1]?.split("?")[0],
          hospital_id: hospital,
          version: 8,
        });
      }
      if (url.includes(`/staff/${id}`)) {
        if (options.readError)
          return Response.json(
            { error: options.readError, message: "Account changed" },
            { status: 409 },
          );
        return Response.json({
          ...current,
          directory_context_version: options.unacknowledged ? undefined : "v1",
          hospital_id: options.wrongHospital ? tenant : hospital,
        });
      }
      return Response.json(
        page([
          {
            ...current,
            display_name:
              current.version === 7 ? "Synthetic stale directory name" : current.display_name,
          },
        ]),
      );
    }),
  );
  return writes;
}
it("opens the current owner snapshot and binds saves to its account and revision", async () => {
  const writes = setup();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Workforce />
    </NavigationGuardProvider>,
  );
  await user.click(
    await screen.findByRole("button", { name: "Edit Synthetic stale directory name" }),
  );
  expect(await screen.findByLabelText("Full name")).toHaveValue(row.display_name);
  await user.clear(screen.getByLabelText("Full name"));
  await user.type(screen.getByLabelText("Full name"), "Synthetic edited worker");
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]?.url).toContain("directory_context=v1");
  expect(writes[0]?.headers.get("X-Helivanta-Expected-Tenant")).toBe(tenant);
  expect(writes[0]?.headers.get("X-Helivanta-Expected-Subject")).toBe("basic-manager");
  expect(writes[0]?.body).toMatchObject({
    display_name: "Synthetic edited worker",
    expected_version: 7,
    directory_context_version: "v1",
  });
});
it.each([{ unacknowledged: true }, { wrongHospital: true }])(
  "keeps editing closed without an acknowledged matching record: %j",
  async (options) => {
    setup(options);
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <Workforce />
      </NavigationGuardProvider>,
    );
    await user.click(
      await screen.findByRole("button", { name: "Edit Synthetic stale directory name" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not verify");
    expect(screen.queryByRole("button", { name: "Save staff" })).not.toBeInTheDocument();
  },
);
it("retains a conflicted draft until explicit discard and reload", async () => {
  setup({ error: "staff_version_conflict" });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Workforce />
    </NavigationGuardProvider>,
  );
  await user.click(
    await screen.findByRole("button", { name: "Edit Synthetic stale directory name" }),
  );
  const name = await screen.findByLabelText("Full name");
  await user.clear(name);
  await user.type(name, "Synthetic unsaved draft");
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  await screen.findByText(/draft has been kept/);
  expect(name).toHaveValue("Synthetic unsaved draft");
  await user.click(screen.getByRole("button", { name: "Reload current record" }));
  const dialog = await screen.findByRole("dialog", { name: "Reload the current Staff record?" });
  await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(name).toHaveValue("Synthetic unsaved draft");
  await user.click(screen.getByRole("button", { name: "Reload current record" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", {
      name: "Discard edits and reload",
    }),
  );
  await waitFor(() =>
    expect(screen.getByLabelText("Full name")).toHaveValue("Synthetic concurrent worker"),
  );
  expect(await screen.findByRole("row", { name: /Synthetic concurrent worker/ })).toHaveTextContent(
    "Inactive",
  );
});
it.each(["session_changed"])("removes a basic draft when a save detects %s", async (error) => {
  setup({ error });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Workforce />
    </NavigationGuardProvider>,
  );
  await user.click(
    await screen.findByRole("button", { name: "Edit Synthetic stale directory name" }),
  );
  await screen.findByLabelText("Full name");
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/account or workspace changed/);
  expect(screen.queryByRole("button", { name: "Save staff" })).not.toBeInTheDocument();
});
