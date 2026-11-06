import { act, screen, waitFor } from "@testing-library/react";
import { NavigationGuardProvider } from "@helivanta/ui";
import { beginTenantTransition, cancelTenantTransition } from "@helivanta/api";
import { renderWithProviders } from "@helivanta/api/testing";
import StaffPostings from "./page";

const tenant = "11111111-1111-4111-8111-111111111111";
const hospital = "22222222-2222-4222-8222-222222222222";
const staff = "33333333-3333-4333-8333-333333333333";

afterEach(() => {
  act(() => cancelTenantTransition());
  vi.unstubAllGlobals();
});

function mockWorkspace() {
  let accountReads = 0;
  const fetch = vi.fn(async (url: string) => {
    if (url.endsWith("/iam/me/permissions")) {
      accountReads += 1;
      return Response.json({ tenant_id: tenant, subject: "synthetic-preparer", data: [] });
    }
    if (url.includes("posting-scopes"))
      return Response.json({
        data: [{ hospital_id: hospital, name: "Synthetic Hospital", can_prepare: true }],
        page: { has_more: false, next_cursor: null },
      });
    if (url.includes("posting-targets"))
      return Response.json({
        data: [{ id: staff, employee_code: "TEAM-1", display_name: "Synthetic team member" }],
        page: { has_more: false, next_cursor: null },
      });
    throw new Error(`Unexpected request: ${url}`);
  });
  vi.stubGlobal("fetch", fetch);
  return { fetch, accountReads: () => accountReads };
}

function renderWorkspace() {
  return renderWithProviders(
    <NavigationGuardProvider>
      <StaffPostings />
    </NavigationGuardProvider>,
  );
}

it("removes the previous hospital's staff before the tenant switch completes", async () => {
  mockWorkspace();
  const { user } = renderWorkspace();
  await user.click(
    await screen.findByRole("button", { name: "Open posting hospital Synthetic Hospital" }),
  );
  await screen.findByText("Synthetic team member");

  act(() => beginTenantTransition());

  expect(screen.queryByText("Synthetic team member")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /Open posting hospital/ })).not.toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent("Switching posting workspace");
});

it("does not discover posting hospitals while a tenant transition is pending", () => {
  const requests = mockWorkspace();
  act(() => beginTenantTransition());

  renderWorkspace();

  expect(screen.getByRole("status")).toHaveTextContent("Switching posting workspace");
  expect(requests.fetch).not.toHaveBeenCalled();
});

it("rechecks identity and clears selected staff when a tenant switch is cancelled", async () => {
  const requests = mockWorkspace();
  const { user } = renderWorkspace();
  await user.click(
    await screen.findByRole("button", { name: "Open posting hospital Synthetic Hospital" }),
  );
  await screen.findByText("Synthetic team member");

  act(() => beginTenantTransition());
  act(() => cancelTenantTransition());

  await waitFor(() => expect(requests.accountReads()).toBe(2));
  await screen.findByRole("button", { name: "Open posting hospital Synthetic Hospital" });
  expect(screen.queryByText("Synthetic team member")).not.toBeInTheDocument();
});
