import { releasePolicyVersionForTest } from "@helivanta/api/testing";
import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@helivanta/api/testing";
import type { CustomerSignupInput } from "@helivanta/api";
import { SignupForm } from "./signup-form";

function publicRelease(onboarding = true) {
  const now = Date.now();
  return new Response(
    JSON.stringify({
      policy_version: releasePolicyVersionForTest,
      issued_at: new Date(now).toISOString(),
      expires_at: new Date(now + 60_000).toISOString(),
      onboarding,
    }),
  );
}

function stubSignupApi(
  name: "fetch",
  handler: (path: string, init?: RequestInit) => Promise<Response>,
) {
  vi.stubGlobal(name, (path: string, init?: RequestInit) =>
    path === "/api/v1/auth/release-capabilities"
      ? Promise.resolve(publicRelease())
      : handler(path, init),
  );
}

const signinRedirect = vi.hoisted(() => vi.fn());
vi.mock("@/lib/oidc", () => ({ getUserManager: () => ({ signinRedirect }) }));

afterEach(() => {
  vi.unstubAllGlobals();
  signinRedirect.mockReset();
});

it("submits an independent pharmacy with its owner and waits for verified provisioning", async () => {
  const calls: Array<{ path: string; body: unknown }> = [];
  stubSignupApi(
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
  expect(screen.queryByText(/is ready$/)).not.toBeInTheDocument();
});

it("does not offer another signup or show readiness when progress is malformed", async () => {
  stubSignupApi(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify({ state: "ready" }), { status: 200 })),
  );
  renderWithProviders(<SignupForm />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Workspace setup is unavailable");
  expect(screen.queryByRole("button", { name: "Create workspace" })).not.toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: /is ready$/ })).not.toBeInTheDocument();
});

it("offers password setup after email proof instead of resending verification", async () => {
  const calls: Array<{ path: string; body: unknown }> = [];
  stubSignupApi(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      if (init?.method === "POST") calls.push({ path, body: JSON.parse(String(init.body)) });
      return new Response(JSON.stringify({ state: "password_action_required" }), { status: 200 });
    }),
  );
  renderWithProviders(<SignupForm />);
  expect(await screen.findByRole("heading", { name: "Your email is verified" })).toBeVisible();
  await userEvent.setup().click(screen.getByRole("button", { name: "Send password link" }));
  await waitFor(() =>
    expect(calls).toEqual([{ path: "/api/v1/auth/signup/password-link", body: {} }]),
  );
  expect(screen.queryByRole("button", { name: "Resend email" })).not.toBeInTheDocument();
});

// #1762: the email limits are not preamble; they are told when a resend is refused.
it("tells the email limits only when a resend is refused", async () => {
  stubSignupApi(
    "fetch",
    vi.fn(async (_path: string, init?: RequestInit) =>
      init?.method === "POST"
        ? new Response(JSON.stringify({ error: "rate_limited", message: "Too many" }), {
            status: 429,
          })
        : new Response(JSON.stringify({ state: "email_action_required" }), { status: 200 }),
    ),
  );
  renderWithProviders(<SignupForm />);
  expect(await screen.findByRole("heading", { name: "Check your email" })).toBeVisible();
  expect(screen.queryByText(/emails per hour|several emails/)).not.toBeInTheDocument();
  await userEvent.setup().click(screen.getByRole("button", { name: "Resend email" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "You've requested several emails. Try again in about an hour.",
  );
});

it("requires business contact and headquarters details before submitting", async () => {
  const fetcher = vi
    .fn<(path: string, init?: RequestInit) => Promise<Response>>()
    .mockResolvedValue(new Response(JSON.stringify({ state: "not_started" }), { status: 200 }));
  stubSignupApi("fetch", fetcher);
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

// The contract leaves the owner email a bare string; the form checks its shape
// in zod, as type="email" did natively before #1855.
it("refuses a malformed owner email inline without a request", async () => {
  const fetcher = vi
    .fn<(path: string, init?: RequestInit) => Promise<Response>>()
    .mockResolvedValue(new Response(JSON.stringify({ state: "not_started" }), { status: 200 }));
  stubSignupApi("fetch", fetcher);
  renderWithProviders(<SignupForm />);
  const user = userEvent.setup();
  const email = await screen.findByLabelText("Owner email");
  expect(email).toHaveAttribute("type", "text");
  await user.type(email, "not-an-email");
  await user.click(screen.getByRole("button", { name: "Create workspace" }));
  await waitFor(() => expect(email).toHaveAttribute("aria-invalid", "true"));
  expect(email).toHaveAttribute("aria-describedby", "signup-email-error");
  expect(document.getElementById("signup-email-error")).toHaveTextContent(
    "Enter a valid email address.",
  );
  expect(fetcher.mock.calls.every((call) => !(call[1] as RequestInit | undefined)?.method)).toBe(
    true,
  );
});

it("preserves a branch address in a different supported state", async () => {
  let submitted: CustomerSignupInput | undefined;
  stubSignupApi(
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

it("requires a fresh tenant account instead of offering cross-organisation account adoption", async () => {
  stubSignupApi(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ state: "email_action_required" }), { status: 200 }),
      ),
  );
  renderWithProviders(<SignupForm />);
  expect(await screen.findByRole("heading", { name: "Check your email" })).toHaveFocus();
  expect(
    screen.queryByRole("button", { name: "Continue with an existing account" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Resend email" })).toBeVisible();
  expect(signinRedirect).not.toHaveBeenCalled();
});

it("opens the newly created workspace using its exact tenant", async () => {
  const tenantId = "11111111-1111-4111-8111-111111111111";
  stubSignupApi(
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
    scope: `openid profile email urn:zitadel:iam:org:id:helivanta-${tenantId}`,
    prompt: "login",
    max_age: 0,
  });
});

it("starts a separate blank signup without deleting the existing workspace", async () => {
  let fresh = false;
  const posts: string[] = [];
  stubSignupApi(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      if (init?.method === "POST") {
        posts.push(path);
        expect(JSON.parse(String(init.body))).toEqual({});
        fresh = true;
      }
      return new Response(
        JSON.stringify(
          fresh
            ? { state: "not_started" }
            : {
                state: "ready",
                name: "Existing hospital",
                kind: "hospital",
                tenantId: "11111111-1111-4111-8111-111111111111",
                resourceId: "0199e000-0000-7000-8000-000000000010",
              },
        ),
        { status: 200 },
      );
    }),
  );
  renderWithProviders(<SignupForm />);
  await userEvent
    .setup()
    .click(await screen.findByRole("button", { name: "Create another workspace" }));
  expect(await screen.findByRole("heading", { name: "Create your workspace" })).toBeVisible();
  expect(screen.getByLabelText("Owner email")).toHaveValue("");
  expect(screen.getByLabelText("Organisation name")).toHaveValue("");
  expect(posts).toEqual(["/api/v1/auth/signup/new"]);
});

it("a disabled signup flag prevents new registration", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) =>
      path === "/api/v1/auth/release-capabilities"
        ? publicRelease(false)
        : new Response(JSON.stringify({ state: "not_started" })),
    ),
  );
  renderWithProviders(<SignupForm />);
  expect(await screen.findByText(/New workspace registration is not available/)).toBeVisible();
  expect(screen.queryByRole("button", { name: "Create workspace" })).not.toBeInTheDocument();
});

it("unavailable signup decisions do not show a registration form", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) =>
      path === "/api/v1/auth/release-capabilities"
        ? new Response("unavailable", { status: 503 })
        : new Response(JSON.stringify({ state: "not_started" })),
    ),
  );
  renderWithProviders(<SignupForm />);
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Registration availability could not be verified",
  );
  expect(screen.queryByLabelText("Owner email")).not.toBeInTheDocument();
});

it.each([
  ["email_action_required", "Check your email"],
  ["password_action_required", "Your email is verified"],
])("existing %s journeys continue without requesting a signup decision", async (state, heading) => {
  const fetcher = vi.fn(async () => new Response(JSON.stringify({ state })));
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<SignupForm />);
  expect(await screen.findByRole("heading", { name: heading })).toBeVisible();
  expect(fetcher.mock.calls).toHaveLength(1);
});

it("focuses a new signup after a delayed availability decision permits the form", async () => {
  let fresh = false;
  let allow!: (response: Response) => void;
  const decision = new Promise<Response>((resolve) => {
    allow = resolve;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      if (path.endsWith("/auth/release-capabilities")) return decision;
      if (init?.method === "POST") fresh = true;
      return Response.json(
        fresh
          ? { state: "not_started" }
          : {
              state: "ready",
              name: "Existing hospital",
              kind: "hospital",
              tenantId: "11111111-1111-4111-8111-111111111111",
              resourceId: "0199e000-0000-7000-8000-000000000010",
            },
      );
    }),
  );
  renderWithProviders(<SignupForm />);
  await userEvent
    .setup()
    .click(await screen.findByRole("button", { name: "Create another workspace" }));
  expect(await screen.findByRole("status")).toHaveTextContent("Loading");
  allow(publicRelease());
  await waitFor(() =>
    expect(screen.getByRole("heading", { name: "Create your workspace" })).toHaveFocus(),
  );
});

// #1767: since one-email signup the tab that opens the email link signs in.
// This page waits honestly and never starts a second sign-in of its own.
it("waits without promising or starting a sign-in", async () => {
  const tenantId = "11111111-1111-4111-8111-111111111111";
  let ready = false;
  stubSignupApi(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify(
            ready
              ? {
                  state: "ready",
                  name: "QA",
                  kind: "hospital",
                  tenantId,
                  resourceId: "1aee9771-66c3-445c-8b3e-b18f472ca409",
                }
              : { state: "provisioning" },
          ),
          { status: 200 },
        ),
    ),
  );
  renderWithProviders(<SignupForm />);
  expect(await screen.findByText("This can take up to 2 minutes.")).toBeVisible();
  expect(screen.queryByText(/sign you in/)).not.toBeInTheDocument();
  ready = true;
  expect(
    await screen.findByRole("button", { name: "Open workspace" }, { timeout: 6000 }),
  ).toBeVisible();
  expect(signinRedirect).not.toHaveBeenCalled();
});

// Opening the inbox is the next step, so resending is the secondary action.
it("keeps Resend email secondary while waiting for the email", async () => {
  stubSignupApi(
    "fetch",
    vi.fn(
      async () => new Response(JSON.stringify({ state: "email_action_required" }), { status: 200 }),
    ),
  );
  renderWithProviders(<SignupForm />);
  const resend = await screen.findByRole("button", { name: "Resend email" });
  expect(resend.className).toMatch(/\bborder\b/);
  expect(resend.className).not.toMatch(/\bbg-primary\b/);
});

// Coming back later to a ready workspace still offers the button, so the
// page stays usable for "Create another workspace".
it("does not sign in by itself when the workspace was already ready", async () => {
  stubSignupApi(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            state: "ready",
            name: "QA",
            kind: "hospital",
            tenantId: "11111111-1111-4111-8111-111111111111",
            resourceId: "1aee9771-66c3-445c-8b3e-b18f472ca409",
          }),
          { status: 200 },
        ),
    ),
  );
  renderWithProviders(<SignupForm />);
  expect(await screen.findByRole("button", { name: "Open workspace" })).toBeVisible();
  await new Promise((r) => setTimeout(r, 300));
  expect(signinRedirect).not.toHaveBeenCalled();
});
