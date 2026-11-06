import { screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { PartnerGrantDraftWorkspace } from "./grant-drafts";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-owner" };
const id = "019a55c0-1111-7111-8111-111111111111";
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());

it("recovers a lost committed proposal without a second write or operating success", async () => {
  let retained: Record<string, unknown> | null = null;
  let writes = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.startsWith("/api/v1/pharmacy/pharmacies") && url.includes("/outlets"))
        return Response.json(
          page([
            {
              id,
              pharmacy_id: id,
              name: "Synthetic Pune outlet",
              code: "PUNE",
              country_code: "IN",
              regulatory_status: "pending_review",
            },
          ]),
        );
      if (url.startsWith("/api/v1/pharmacy/pharmacies"))
        return Response.json(
          page([
            {
              id,
              name: "Synthetic independent pharmacy",
              code: "TEST",
              ownership_type: "independent",
            },
          ]),
        );
      if (url.startsWith("/api/v1/iam/partner-grant-drafts?"))
        return Response.json(page(retained ? [retained] : []));
      if (url.includes("/api/v1/iam/partner-grant-drafts/") && init?.method === "PUT") {
        writes++;
        retained = {
          id: url.split("/").at(-1),
          owner_tenant_id: scope.tenantId,
          input: JSON.parse(String(init.body)),
          status: "pending_verification",
          operating_access_enabled: false,
          prepared_by: scope.subject,
          created_at: new Date().toISOString(),
        };
        throw new TypeError("Synthetic lost committed response");
      }
      if (retained && url.endsWith(`/partner-grant-drafts/${retained.id}`))
        return Response.json(retained);
      throw new Error(`Unexpected request ${url}`);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PartnerGrantDraftWorkspace scope={scope} onChanged={vi.fn()} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Prepare supplier access" }));
  await user.click(await screen.findByRole("combobox", { name: "Supplier organization" }));
  await user.click(await screen.findByRole("option", { name: "Synthetic independent pharmacy" }));
  await user.click(await screen.findByRole("combobox", { name: "Supplying location" }));
  await user.click(await screen.findByRole("option", { name: "Synthetic Pune outlet" }));
  for (const [label, value] of [
    ["Relationship reference", id],
    ["Hospital workspace reference", "22222222-2222-4222-8222-222222222222"],
    ["Hospital reference", "33333333-3333-4333-8333-333333333333"],
    ["Published hospital team reference", id],
    ["Reason", "Synthetic receiving access nomination"],
  ])
    await user.type(screen.getByRole("textbox", { name: label }), value);
  await user.click(screen.getByRole("button", { name: "Save supplier access proposal" }));
  expect(await screen.findByText(/The save may have been recorded/)).toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "Reason" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Check saved proposal" }));
  expect(await screen.findByText(/Supplier access proposal recovered/)).toBeInTheDocument();
  expect(screen.getAllByText(/Verification pending/).length).toBeGreaterThan(0);
  expect(writes).toBe(1);
  expect(screen.queryByText("Access active")).not.toBeInTheDocument();
});
