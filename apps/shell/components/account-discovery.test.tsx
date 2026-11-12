import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { AccountDiscovery } from "./account-discovery";

const tenant = "11111111-1111-4111-8111-111111111111";
const other = "22222222-2222-4222-8222-222222222222";
const redirect = vi.fn();
function stub(accounts: unknown[], verifyStatus = 200) {
  return vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/auth/accounts/verify"))
        return Response.json(
          verifyStatus === 200
            ? { state: "verified" }
            : {
                error: "account_discovery_expired",
                message: "The code expired. Request a new code.",
              },
          { status: verifyStatus },
        );
      if (url.endsWith("/auth/accounts") && init?.method === "POST")
        return Response.json({ state: "check_email" }, { status: 202 });
      if (url.endsWith("/auth/accounts")) return Response.json({ accounts });
      throw new Error(`unexpected request ${url}`);
    }),
  );
}
beforeEach(() => {
  vi.unstubAllGlobals();
  redirect.mockReset();
});
describe("Account discovery", () => {
  it("requests mailbox proof before disclosing choices, then opens the single account", async () => {
    stub([{ tenantId: tenant, name: "Yes Hospital", account: "owner@example.test" }]);
    const { user } = renderWithProviders(<AccountDiscovery onSelect={redirect} />);
    expect(screen.queryByLabelText(/workspace id/i)).not.toBeInTheDocument();
    expect(screen.queryByText("Yes Hospital")).not.toBeInTheDocument();
    await user.type(screen.getByLabelText("Email"), "owner@example.test");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    await user.type(await screen.findByLabelText("Email verification code"), "12345678");
    await user.click(screen.getByRole("button", { name: "Verify email" }));
    await waitFor(() => expect(redirect).toHaveBeenCalledWith(tenant));
  });
  it("shows business names for separate hospital and pharmacy accounts", async () => {
    stub([
      { tenantId: tenant, name: "Yes Hospital", account: "owner@example.test" },
      { tenantId: other, name: "Independent pharmacy", account: "owner@example.test" },
    ]);
    const { user } = renderWithProviders(<AccountDiscovery onSelect={redirect} />);
    await user.type(screen.getByLabelText("Email"), "owner@example.test");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    await user.type(await screen.findByLabelText("Email verification code"), "12345678");
    await user.click(screen.getByRole("button", { name: "Verify email" }));
    await user.click(await screen.findByRole("button", { name: /Independent pharmacy/ }));
    expect(redirect).toHaveBeenCalledWith(other);
    expect(redirect).not.toHaveBeenCalledWith(tenant);
  });
  it("offers resend after expiry without starting a native login", async () => {
    stub([], 404);
    const { user } = renderWithProviders(<AccountDiscovery onSelect={redirect} />);
    await user.type(screen.getByLabelText("Email"), "owner@example.test");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    await user.type(await screen.findByLabelText("Email verification code"), "12345678");
    await user.click(screen.getByRole("button", { name: "Verify email" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/expired/);
    expect(screen.getByRole("button", { name: "Send a new code" })).toBeEnabled();
    expect(redirect).not.toHaveBeenCalled();
  });
});
