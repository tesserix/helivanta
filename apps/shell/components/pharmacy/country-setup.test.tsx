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
it("registers Pakistan with Karachi timezone and separate unapproved regulatory status", async () => {
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
  await user.click(screen.getByText("Pharmacy and outlet administration"));
  await chooseOption(user, screen.getByLabelText("Country"), "Pakistan");
  expect(screen.getByLabelText("IANA time zone")).toHaveValue("Asia/Karachi");
  expect(screen.getByText(/PKR.*regulatory review/i)).toBeInTheDocument();
  await user.type(screen.getByLabelText("Outlet code"), "TEST-PK");
  await user.type(screen.getByLabelText("Outlet name"), "TEST Karachi");
  await user.click(screen.getByRole("button", { name: "Register outlet" }));
  await waitFor(() =>
    expect(submitted).toMatchObject({
      country_code: "PK",
      timezone: "Asia/Karachi",
      code: "TEST-PK",
    }),
  );
  expect(submitted).not.toHaveProperty("licence_approved");
});
