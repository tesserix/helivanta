import { afterEach, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@helivanta/api/testing";
import { BusinessProfilePanel } from "./business-profile";

afterEach(() => vi.unstubAllGlobals());

it("reads back persisted business details only when the owner opens the panel", async () => {
  const address = {
    line1: "10 Test Road",
    line2: "HQ Building",
    city: "Pune",
    district: "Pune",
    postalCode: "411001",
    stateCode: "MH",
    countryCode: "IN",
  };
  const fetcher = vi.fn(
    async () =>
      new Response(
        JSON.stringify({
          profile: {
            contact: { phone: "+919876543210", email: "office@example.test" },
            headOffice: address,
            mainBranch: {
              name: "Test Main Branch",
              address: { ...address, line1: "20 Branch Road", line2: "Branch Building" },
            },
          },
        }),
        { status: 200 },
      ),
  );
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(
    <BusinessProfilePanel
      tenantId="aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
      kind="hospital"
      resourceId="0199e000-0000-7000-8000-000000000010"
    />,
  );
  expect(fetcher).not.toHaveBeenCalled();
  await userEvent.setup().click(screen.getByRole("button", { name: "View contact and addresses" }));
  expect(await screen.findByText("office@example.test")).toBeVisible();
  expect(screen.getByText("10 Test Road")).toBeVisible();
  expect(screen.getByText("20 Branch Road")).toBeVisible();
  expect(screen.getByText("Test Main Branch")).toBeVisible();
});
