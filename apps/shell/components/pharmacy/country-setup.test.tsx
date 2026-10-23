import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../test/select";
import { PharmacyRegistrySetup } from "./pharmacy-registry-setup";
const pharmacy = {
  id: "0199e000-0000-7000-8000-000000000010",
  code: "TEST",
  name: "TEST Pharmacy",
  ownership_type: "independent" as const,
  onboarding_status: "draft" as const,
  created_at: "2026-10-16T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());
it("registers an Indian outlet, the only country offered, with unapproved regulatory status", async () => {
  let submitted: Record<string, unknown> | undefined;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        submitted = JSON.parse(init.body as string);
        return new Response(
          JSON.stringify({
            id: "0199e000-0000-7000-8000-000000000011",
            ...submitted,
            regulatory_status: "pending_review",
          }),
        );
      }
      return new Response(
        JSON.stringify({ data: [], page: { has_more: false, next_cursor: null } }),
      );
    }),
  );
  const { user } = renderWithProviders(
    <PharmacyRegistrySetup tenantId="TEST-tenant" pharmacy={pharmacy} />,
  );
  // #1287: India is the only country offered.
  await user.click(screen.getByLabelText("Country"));
  expect((await screen.findAllByRole("option")).map((o) => o.textContent)).toEqual(["India"]);
  await user.keyboard("{Escape}");
  await chooseOption(user, screen.getByLabelText("Country"), "India");
  expect(screen.getByLabelText("IANA time zone")).toHaveValue("Asia/Kolkata");
  expect(screen.getByText(/INR.*regulatory review/i)).toBeInTheDocument();
  await user.type(screen.getByLabelText("Outlet code"), "TEST-IN");
  await user.type(screen.getByLabelText("Outlet name"), "TEST Pune");
  await user.click(screen.getByRole("button", { name: "Register outlet" }));
  await waitFor(() =>
    expect(submitted).toMatchObject({
      country_code: "IN",
      timezone: "Asia/Kolkata",
      code: "TEST-IN",
    }),
  );
  expect(submitted).not.toHaveProperty("licence_approved");
});
