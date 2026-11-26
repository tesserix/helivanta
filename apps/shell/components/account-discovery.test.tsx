import { screen, waitFor, within } from "@testing-library/react";
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
async function firstCodeDigit() {
  const group = await screen.findByRole("group", { name: "Email verification code" });
  return within(group).getByRole("textbox", { name: "Digit 1" });
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
    await user.click(screen.getByRole("button", { name: "Send code" }));
    // Auto-submits on the eighth digit, like every MfaCodeForm.
    await user.type(await firstCodeDigit(), "12345678");
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
    await user.click(screen.getByRole("button", { name: "Send code" }));
    // Auto-submits on the eighth digit, like every MfaCodeForm.
    await user.type(await firstCodeDigit(), "12345678");
    await user.click(await screen.findByRole("button", { name: /Independent pharmacy/ }));
    expect(redirect).toHaveBeenCalledWith(other, "owner@example.test");
    expect(redirect).not.toHaveBeenCalledWith(tenant, expect.anything());
    expect(window.localStorage.getItem("helivanta.login.workspace.v1")).toBeNull();
  });
  it("offers resend after expiry without starting a native login", async () => {
    stub([], 404);
    const { user } = renderWithProviders(<AccountDiscovery onSelect={redirect} />);
    await user.type(screen.getByLabelText("Email"), "owner@example.test");
    await user.click(screen.getByRole("button", { name: "Send code" }));
    // Auto-submits on the eighth digit, like every MfaCodeForm.
    await user.type(await firstCodeDigit(), "12345678");
    // The API's own wording still reaches the reader (#1856 D2).
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "The code expired. Request a new code.",
    );
    expect(screen.getByRole("button", { name: "Send a new code" })).toBeEnabled();
    expect(redirect).not.toHaveBeenCalled();
  });
});

it("explains an incomplete email code without exposing validation internals", async () => {
  stub([]);
  const { user } = renderWithProviders(<AccountDiscovery onSelect={redirect} />);
  await user.type(screen.getByLabelText("Email"), "owner@example.test");
  await user.click(screen.getByRole("button", { name: "Send code" }));
  await user.type(await firstCodeDigit(), "1234");
  await user.keyboard("{Enter}");
  expect(screen.getByRole("button", { name: "Verify email" })).toBeDisabled();
  expect(screen.queryByText(/regex|invalid_string|too_small/i)).not.toBeInTheDocument();
  expect(fetch).not.toHaveBeenCalledWith(
    expect.stringContaining("/auth/accounts/verify"),
    expect.anything(),
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

// #1854: the button says what it does, focus follows the phase, and the code is MfaCodeForm.
describe("Account discovery steps (#1854)", () => {
  it("names the email step's action Send code", async () => {
    stub([]);
    renderWithProviders(<AccountDiscovery onSelect={redirect} />);
    expect(screen.getByRole("button", { name: "Send code" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Sign in" })).not.toBeInTheDocument();
  });

  it("sends exactly the eight digits entered, once", async () => {
    stub([]);
    const { user } = renderWithProviders(<AccountDiscovery onSelect={redirect} />);
    await user.type(screen.getByLabelText("Email"), "owner@example.test");
    await user.click(screen.getByRole("button", { name: "Send code" }));
    const group = await screen.findByRole("group", { name: "Email verification code" });
    expect(within(group).getAllByRole("textbox")).toHaveLength(8);
    await user.type(within(group).getByRole("textbox", { name: "Digit 1" }), "12345678");
    await screen.findByRole("heading", { name: "Choose an account" });
    const verifies = vi
      .mocked(fetch)
      .mock.calls.filter(([url]) => String(url).endsWith("/auth/accounts/verify"));
    expect(verifies).toHaveLength(1);
    expect(JSON.parse(String(verifies[0][1]?.body))).toEqual({ code: "12345678" });
  });

  it("moves focus with each phase, forward and back", async () => {
    stub([
      { tenantId: tenant, name: "Yes Hospital", account: "owner@example.test" },
      { tenantId: other, name: "Independent pharmacy", account: "owner@example.test" },
    ]);
    const outside = document.createElement("button");
    document.body.append(outside);
    outside.focus();
    const { user } = renderWithProviders(<AccountDiscovery onSelect={redirect} />);
    // The first phase takes nothing on arrival.
    expect(outside).toHaveFocus();
    outside.remove();
    await user.type(screen.getByLabelText("Email"), "owner@example.test");
    await user.click(screen.getByRole("button", { name: "Send code" }));
    const digit = await firstCodeDigit();
    await waitFor(() => expect(digit).toHaveFocus());
    await user.click(screen.getByRole("button", { name: "Use another email" }));
    await waitFor(() => expect(screen.getByLabelText("Email")).toHaveFocus());
    await user.click(screen.getByRole("button", { name: "Send code" }));
    await user.type(await firstCodeDigit(), "12345678");
    const heading = await screen.findByRole("heading", { name: "Choose an account" });
    await waitFor(() => expect(heading).toHaveFocus());
    expect(heading).toHaveAttribute("tabindex", "-1");
    await user.click(screen.getByRole("button", { name: "Use another email" }));
    await waitFor(() => expect(screen.getByLabelText("Email")).toHaveFocus());
  });
});

// #1856 D2: a failure Helivanta did not word never reaches the reader.
it("shows a fixed sentence, never the raw text, when opening an account fails", async () => {
  stub([{ tenantId: tenant, name: "Yes Hospital", account: "owner@example.test" }]);
  const failing = vi.fn().mockRejectedValue(new TypeError("Failed to fetch"));
  const { user } = renderWithProviders(<AccountDiscovery onSelect={failing} />);
  await user.type(screen.getByLabelText("Email"), "owner@example.test");
  await user.click(screen.getByRole("button", { name: "Send code" }));
  expect(await screen.findByRole("button", { name: "Show my accounts again" })).toBeVisible();
  await user.type(await firstCodeDigit(), "12345678");
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Check your connection and try again.",
  );
  expect(screen.queryByText(/Failed to fetch/)).not.toBeInTheDocument();
});
