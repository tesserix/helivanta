import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { GovernanceOperatingAccess } from "./governance-operating-access";
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/workforce/operating-access",
  useSearchParams: () => new URLSearchParams(),
}));
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-governance-preparer",
};
const employer = {
  employer_kind: "supplier",
  employer_id: "019a55c0-1111-7111-8111-111111111112",
  name: "Synthetic supplier",
  can_manage: true,
  can_review: true,
};
const staff = {
  staff_id: "019a55c0-1111-7111-8111-111111111113",
  display_name: "Synthetic staff",
  staff_version: 3,
  subject: "TEST-receiver",
  link_id: "019a55c0-1111-7111-8111-111111111114",
  link_version: 2,
};
const page = (data: unknown[]) =>
  Response.json({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
it("opens a native reviewed Staff scope through qualified employer and Staff choices", async () => {
  const request = vi.fn(async (url: string) => {
    if (url.endsWith("permissions"))
      return Response.json({
        tenant_id: scope.tenantId,
        subject: scope.subject,
        data: ["iam.governance.operating.manage", "iam.governance.operating.review"],
      });
    if (url.includes("/operating-authority-scopes")) return page([employer]);
    if (url.includes("/staff-choices")) return page([staff]);
    if (url.includes("/operating-governance-employers/") && url.includes("/duties"))
      return page([]);
    throw new Error(`Unexpected legacy request: ${url}`);
  });
  vi.stubGlobal("fetch", request);
  renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceOperatingAccess scope={scope} accessSuspended={false} />
    </NavigationGuardProvider>,
  );
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Manage Synthetic supplier" }));
  await user.click(await screen.findByRole("button", { name: "Open Synthetic staff" }));
  await screen.findByRole("button", { name: "Assign operating duty" });
  expect(
    request.mock.calls.some(([url]) =>
      url.includes(`/supplier/${employer.employer_id}/staff/${staff.staff_id}/duties`),
    ),
  ).toBe(true);
  expect(
    request.mock.calls.some(
      ([url]) => url.includes("/supply/parties/") || url.includes("/account-links"),
    ),
  ).toBe(false);
  expect(screen.queryByLabelText(/subject ID|Staff ID|employer ID/i)).not.toBeInTheDocument();
});
