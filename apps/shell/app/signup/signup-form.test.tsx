import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@helivanta/api/testing";
import { SignupForm } from "./signup-form";

afterEach(() => vi.unstubAllGlobals());

it("submits an independent pharmacy with its owner and waits for verified provisioning", async () => {
  const calls: Array<{ path: string; body: unknown }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      if (init?.method === "POST") calls.push({ path, body: JSON.parse(String(init.body)) });
      return new Response(
        JSON.stringify({ state: init?.method === "POST" ? "provisioning" : "not_started" }),
        { status: 200 },
      );
    }),
  );
  renderWithProviders(<SignupForm />);
  const user = userEvent.setup();
  await user.type(await screen.findByLabelText("Organisation name"), "TEST Pharmacy");
  await user.click(screen.getByRole("combobox", { name: "Workspace type" }));
  await user.click(screen.getByRole("option", { name: "Independent pharmacy" }));
  await user.click(screen.getByRole("combobox", { name: "State" }));
  await user.click(screen.getByRole("option", { name: "Odisha" }));
  await user.type(screen.getByLabelText("Given name"), "Test");
  await user.type(screen.getByLabelText("Family name"), "Owner");
  await user.type(screen.getByLabelText("Owner email"), "owner@example.test");
  await user.click(screen.getByRole("button", { name: "Create workspace" }));
  await waitFor(() =>
    expect(calls).toEqual([
      {
        path: "/api/v1/auth/signup",
        body: {
          name: "TEST Pharmacy",
          kind: "pharmacy",
          countryCode: "IN",
          stateCode: "OD",
          owner: { givenName: "Test", familyName: "Owner", email: "owner@example.test" },
        },
      },
    ]),
  );
  expect(await screen.findByRole("heading", { name: "Setting up your workspace" })).toBeVisible();
  expect(screen.queryByText("Your workspace is ready")).not.toBeInTheDocument();
});

it("does not offer another signup or show readiness when progress is malformed", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify({ state: "ready" }), { status: 200 })),
  );
  renderWithProviders(<SignupForm />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Workspace setup is unavailable");
  expect(screen.queryByRole("button", { name: "Create workspace" })).not.toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: "Your workspace is ready" }),
  ).not.toBeInTheDocument();
});
