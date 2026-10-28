import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { AccountMenu } from "./account-menu";

const PROFILE = {
  subject: "user-jane",
  tenant_id: "tenant-a",
  display_name: "Jane Doctor",
  email: "jane@example.test",
  roles: [
    { key: "tenant_admin", label: "Tenant administrator" },
    { key: "pharmacist", label: "Pharmacist" },
  ],
};

function serveProfile(status: number, body: unknown) {
  const fetchMock = vi.fn(async (url: string) =>
    url === "/api/v1/iam/me/profile"
      ? new Response(JSON.stringify(body), { status })
      : new Response(JSON.stringify({ data: [] })),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function renderMenu(onSignOut = vi.fn((e) => e.preventDefault())) {
  const result = renderWithProviders(
    <AccountMenu hospitalName="Yes Hospital" onSignOut={onSignOut} />,
  );
  return { ...result, onSignOut };
}

describe("AccountMenu", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("names the signed-in person and lists their roles at this hospital", async () => {
    serveProfile(200, PROFILE);
    const { user } = renderMenu();

    const trigger = await screen.findByRole("button", { name: "Account: Jane Doctor" });
    expect(trigger).toHaveTextContent("JD");
    // Named by its content, so the name holds every visible word (WCAG 2.5.3) and a
    // field label lookup such as getByLabel("Tenant") never lands on a person's name.
    expect(trigger).not.toHaveAttribute("aria-label");
    expect(screen.queryByLabelText(/doctor/i)).toBeNull();
    await user.click(trigger);

    const panel = screen.getByRole("dialog", { name: "Account" });
    expect(within(panel).getByText("Jane Doctor")).toBeVisible();
    expect(within(panel).getByText("jane@example.test")).toBeVisible();
    const roles = within(panel).getByRole("list", { name: "Role at Yes Hospital" });
    expect(
      within(roles)
        .getAllByRole("listitem")
        .map((li) => li.textContent),
    ).toEqual(["Tenant administrator", "Pharmacist"]);
  });

  it("names the person by email when the profile has no display name", async () => {
    serveProfile(200, { ...PROFILE, display_name: "  " });
    renderMenu();

    expect(
      await screen.findByRole("button", { name: "Account: jane@example.test" }),
    ).toHaveTextContent("J");
  });

  it("says plainly when the person holds no role at this hospital", async () => {
    serveProfile(200, { ...PROFILE, roles: [] });
    const { user } = renderMenu();

    await user.click(await screen.findByRole("button", { name: "Account: Jane Doctor" }));

    const panel = screen.getByRole("dialog", { name: "Account" });
    expect(within(panel).getByText("No role at this hospital")).toBeVisible();
    expect(within(panel).queryByRole("list")).toBeNull();
  });

  it("reports an unavailable profile without a placeholder identity and still signs out", async () => {
    serveProfile(503, { error: "identity_unavailable", message: "identity unavailable" });
    const { user, onSignOut } = renderMenu();

    await user.click(screen.getByRole("button", { name: "Account" }));
    const panel = screen.getByRole("dialog", { name: "Account" });
    expect(await within(panel).findByText(/profile is unavailable/i)).toBeVisible();
    expect(within(panel).queryByText("Jane Doctor")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();

    await user.click(within(panel).getByRole("button", { name: "Sign out" }));
    expect(onSignOut).toHaveBeenCalledOnce();
  });

  it("treats a malformed profile as unavailable", async () => {
    serveProfile(200, { display_name: "Jane Doctor" });
    const { user } = renderMenu();

    await user.click(screen.getByRole("button", { name: "Account" }));

    const panel = screen.getByRole("dialog", { name: "Account" });
    expect(await within(panel).findByText(/profile is unavailable/i)).toBeVisible();
    expect(within(panel).queryByText("Jane Doctor")).toBeNull();
  });

  it("offers Sign out while the profile is still loading", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise<Response>(() => {})),
    );
    const { user, onSignOut } = renderMenu();

    await user.click(screen.getByRole("button", { name: "Account" }));
    const panel = screen.getByRole("dialog", { name: "Account" });
    expect(within(panel).getByText(/loading/i)).toBeVisible();

    await user.click(within(panel).getByRole("button", { name: "Sign out" }));
    expect(onSignOut).toHaveBeenCalledOnce();
  });
});
