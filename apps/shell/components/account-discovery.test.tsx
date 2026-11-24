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
  window.localStorage.clear();
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
    await waitFor(() => expect(redirect).toHaveBeenCalledWith(tenant, "owner@example.test"));
    // Remembered at selection, so a start-again landing offers it without another code.
    expect(window.localStorage.getItem("helivanta.login.workspace.v1")).toBeNull();
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
    expect(redirect).toHaveBeenCalledWith(other, "owner@example.test");
    expect(redirect).not.toHaveBeenCalledWith(tenant, expect.anything());
    expect(window.localStorage.getItem("helivanta.login.workspace.v1")).toBeNull();
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

it("explains an incomplete email code without exposing validation internals", async () => {
  stub([]);
  const { user } = renderWithProviders(<AccountDiscovery onSelect={redirect} />);
  await user.type(screen.getByLabelText("Email"), "owner@example.test");
  await user.click(screen.getByRole("button", { name: "Sign in" }));
  await user.type(await screen.findByLabelText("Email verification code"), "1234");
  await user.click(screen.getByRole("button", { name: "Verify email" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Enter the eight-digit code from your email.",
  );
  expect(redirect).not.toHaveBeenCalled();
});

// A workspace stored by an earlier version is never offered (#1820 spec D2).
it("asks for the email even when an earlier version remembered a workspace", async () => {
  window.localStorage.setItem(
    "helivanta.login.workspace.v1",
    JSON.stringify({ tenantId: tenant, expiresAt: Date.now() + 60000 }),
  );
  stub([]);
  renderWithProviders(<AccountDiscovery onSelect={redirect} />);
  expect(await screen.findByLabelText("Email")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Sign in to your previous account" })).toBeNull();
  expect(redirect).not.toHaveBeenCalled();
  expect(fetch).not.toHaveBeenCalled();
});
