import { afterEach, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { StaffIdentitySourcePanel } from "./identity-source-panel";

const tenant = "11111111-1111-4111-8111-111111111111";
const hospital = "22222222-2222-4222-8222-222222222222";
const staff = "33333333-3333-4333-8333-333333333333";
afterEach(() => vi.unstubAllGlobals());
function setup(status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("/iam/me/permissions"))
        return Response.json({ tenant_id: tenant, subject: "synthetic-manager", data: [] });
      return status === 200
        ? Response.json({
            hospital_id: hospital,
            staff_id: staff,
            employee_code: "SYN-STAFF-1",
            display_name: "Synthetic Person",
            given_name: "Synthetic",
            family_name: "Person",
            date_of_birth: "1990-01-02",
            employment_status: "active",
            employment_epoch: 1,
            staff_version: 4,
            identity_hash: "a".repeat(64),
          })
        : Response.json(
            { error: "forbidden", message: "Staff identity action not permitted." },
            { status },
          );
    }),
  );
}
it("loads owner-authorized identity details with tenant-pinned private requests", async () => {
  setup();
  renderWithProviders(
    <StaffIdentitySourcePanel hospitalId={hospital} staffId={staff} onClose={() => {}} />,
  );
  expect(await screen.findByText("SYN-STAFF-1")).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "Identity review basis" })).toHaveFocus();
  expect(screen.getByText("1990-01-02")).toBeInTheDocument();
  expect(screen.getByText(/Viewing these details does not approve/)).toBeInTheDocument();
  const call = vi
    .mocked(fetch)
    .mock.calls.find(([url]) => String(url).includes("/identity-staff/"));
  expect(new Headers(call?.[1]?.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenant);
  expect(call?.[1]?.cache).toBe("no-store");
});
it("does not show identity details when a current assignment is denied", async () => {
  setup(403);
  renderWithProviders(
    <StaffIdentitySourcePanel hospitalId={hospital} staffId={staff} onClose={() => {}} />,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "current hospital identity manager or reviewer assignment",
  );
  expect(screen.queryByText("1990-01-02")).not.toBeInTheDocument();
});

it("explains an unavailable staff record or active account link without showing identity data", async () => {
  setup(404);
  renderWithProviders(
    <StaffIdentitySourcePanel hospitalId={hospital} staffId={staff} onClose={() => {}} />,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent("staff account link");
  expect(screen.queryByText("1990-01-02")).not.toBeInTheDocument();
});
