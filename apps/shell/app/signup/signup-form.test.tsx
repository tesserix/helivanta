import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@helivanta/api/testing";
import type { CustomerSignupInput } from "@helivanta/api";
import { SignupForm } from "./signup-form";

const signinRedirect = vi.hoisted(() => vi.fn());
vi.mock("@/lib/oidc", () => ({ getUserManager: () => ({ signinRedirect }) }));

afterEach(() => {
  vi.unstubAllGlobals();
  signinRedirect.mockReset();
});

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
  await user.type(screen.getByLabelText("Organisation phone"), "+919876543210");
  await user.type(screen.getByLabelText("Organisation email"), "office@example.test");
  await user.type(screen.getByLabelText("Head office address line 1"), "10 Test Road");
  await user.type(screen.getByLabelText("Head office city"), "Bhubaneswar");
  await user.type(screen.getByLabelText("Head office district"), "Khordha");
  await user.type(screen.getByLabelText("Head office PIN code"), "751001");
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
          profile: {
            contact: { phone: "+919876543210", email: "office@example.test" },
            headOffice: {
              line1: "10 Test Road",
              line2: "",
              city: "Bhubaneswar",
              district: "Khordha",
              postalCode: "751001",
              stateCode: "OD",
              countryCode: "IN",
            },
            mainBranch: {
              name: "Main branch",
              address: {
                line1: "10 Test Road",
                line2: "",
                city: "Bhubaneswar",
                district: "Khordha",
                postalCode: "751001",
                stateCode: "OD",
                countryCode: "IN",
              },
            },
          },
        },
      },
    ]),
  );
  expect(await screen.findByRole("heading", { name: "Setting up your workspace" })).toHaveFocus();
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

it("offers password setup after email proof instead of resending verification", async () => {
  const calls: Array<{ path: string; body: unknown }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      if (init?.method === "POST") calls.push({ path, body: JSON.parse(String(init.body)) });
      return new Response(JSON.stringify({ state: "password_action_required" }), { status: 200 });
    }),
  );
  renderWithProviders(<SignupForm />);
  expect(await screen.findByRole("heading", { name: "Set your password" })).toBeVisible();
  await userEvent.setup().click(screen.getByRole("button", { name: "Send password setup email" }));
  await waitFor(() =>
    expect(calls).toEqual([{ path: "/api/v1/auth/signup/password-link", body: {} }]),
  );
  expect(
    screen.queryByRole("button", { name: "Resend verification email" }),
  ).not.toBeInTheDocument();
});

it("requires business contact and headquarters details before submitting", async () => {
  const fetcher = vi
    .fn<(path: string, init?: RequestInit) => Promise<Response>>()
    .mockResolvedValue(new Response(JSON.stringify({ state: "not_started" }), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<SignupForm />);
  const user = userEvent.setup();
  await user.type(await screen.findByLabelText("Organisation name"), "TEST Hospital");
  await user.type(screen.getByLabelText("Given name"), "Test");
  await user.type(screen.getByLabelText("Family name"), "Owner");
  await user.type(screen.getByLabelText("Owner email"), "owner@example.test");
  await user.click(screen.getByRole("button", { name: "Create workspace" }));
  expect((await screen.findAllByRole("alert")).length).toBeGreaterThan(0);
  expect(fetcher.mock.calls.every((call) => !(call[1] as RequestInit | undefined)?.method)).toBe(
    true,
  );
});

it("preserves a branch address in a different supported state", async () => {
  let submitted: CustomerSignupInput | undefined;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_path: string, init?: RequestInit) => {
      if (init?.method === "POST") submitted = JSON.parse(String(init.body));
      return new Response(
        JSON.stringify({ state: init?.method === "POST" ? "provisioning" : "not_started" }),
        { status: 200 },
      );
    }),
  );
  renderWithProviders(<SignupForm />);
  const user = userEvent.setup();
  await user.type(await screen.findByLabelText("Organisation name"), "TEST Hospital");
  await user.type(screen.getByLabelText("Organisation phone"), "+919876543210");
  await user.type(screen.getByLabelText("Organisation email"), "office@example.test");
  await user.type(screen.getByLabelText("Head office address line 1"), "10 Test Road");
  await user.type(screen.getByLabelText("Head office city"), "Pune");
  await user.type(screen.getByLabelText("Head office district"), "Pune");
  await user.type(screen.getByLabelText("Head office PIN code"), "411001");
  await user.click(
    screen.getByRole("checkbox", { name: "Main branch uses the head office address" }),
  );
  await user.type(screen.getByLabelText("Main branch address line 1"), "20 Test Road");
  await user.type(screen.getByLabelText("Main branch city"), "Bhubaneswar");
  await user.type(screen.getByLabelText("Main branch district"), "Khordha");
  await user.type(screen.getByLabelText("Main branch PIN code"), "751001");
  await user.click(screen.getByRole("combobox", { name: "Main branch state" }));
  await user.click(screen.getByRole("option", { name: "Odisha" }));
  await user.type(screen.getByLabelText("Given name"), "Test");
  await user.type(screen.getByLabelText("Family name"), "Owner");
  await user.type(screen.getByLabelText("Owner email"), "owner@example.test");
  await user.click(screen.getByRole("button", { name: "Create workspace" }));
  await waitFor(() =>
    expect(submitted).toMatchObject({
      stateCode: "MH",
      profile: {
        headOffice: { line1: "10 Test Road", stateCode: "MH" },
        mainBranch: { address: { line1: "20 Test Road", stateCode: "OD" } },
      },
    }),
  );
});

it("continues a retained signup through fresh native authentication", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ state: "email_action_required" }), { status: 200 }),
      ),
  );
  renderWithProviders(<SignupForm />);
  expect(await screen.findByRole("heading", { name: "Verify your account" })).toHaveFocus();
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "Continue with an existing account" }));
  expect(signinRedirect).toHaveBeenCalledWith({
    state: { purpose: "customer-signup" },
    prompt: "login",
    max_age: 0,
  });
  expect(screen.getByRole("button", { name: "Resend verification email" })).toBeVisible();
});

it("opens the newly created workspace using its exact tenant", async () => {
  const tenantId = "11111111-1111-4111-8111-111111111111";
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          state: "ready",
          name: "TEST Hospital",
          kind: "hospital",
          tenantId,
          resourceId: "0199e000-0000-7000-8000-000000000010",
        }),
        { status: 200 },
      ),
    ),
  );
  renderWithProviders(<SignupForm />);
  await userEvent.setup().click(await screen.findByRole("button", { name: "Open workspace" }));
  expect(signinRedirect).toHaveBeenCalledWith({
    state: { purpose: "customer-signup-login", tenantId },
    prompt: "login",
    max_age: 0,
  });
});
