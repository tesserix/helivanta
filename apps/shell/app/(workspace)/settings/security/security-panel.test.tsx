import { afterEach, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@helivanta/api/testing";
import { SecurityPanel } from "./security-panel";

const tenant = "11111111-1111-4111-8111-111111111111";
const view = {
  state: "drifted",
  tenantId: tenant,
  subject: "owner",
  revision: "a".repeat(64),
  requireMfa: false,
  externalMfa: true,
  noGrace: true,
  canReconcile: true,
};
afterEach(() => vi.unstubAllGlobals());
it("shows pending after requesting enforcement and waits for native confirmation", async () => {
  let requested = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      if (path.endsWith("/permissions"))
        return new Response(
          JSON.stringify({ tenant_id: tenant, subject: "owner", data: ["iam.member.manage"] }),
        );
      if (init?.method === "POST") requested = true;
      return new Response(JSON.stringify({ ...view, state: requested ? "pending" : "drifted" }), {
        status: init?.method === "POST" ? 202 : 200,
      });
    }),
  );
  renderWithProviders(<SecurityPanel />);
  await userEvent
    .setup()
    .click(await screen.findByRole("button", { name: "Reapply required MFA" }));
  expect(await screen.findByText("Waiting for identity confirmation")).toBeVisible();
  expect(screen.queryByText("Required MFA is active")).not.toBeInTheDocument();
});
it("rejects a foreign-tenant response and does not offer a security change", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (path: string) =>
        new Response(
          JSON.stringify(
            path.endsWith("/permissions")
              ? { tenant_id: tenant, subject: "owner", data: ["iam.member.manage"] }
              : { ...view, tenantId: "22222222-2222-4222-8222-222222222222" },
          ),
        ),
    ),
  );
  renderWithProviders(<SecurityPanel />);
  expect(await screen.findByText("Could not verify workspace security.")).toBeVisible();
  expect(screen.queryByRole("button", { name: "Reapply required MFA" })).not.toBeInTheDocument();
});
