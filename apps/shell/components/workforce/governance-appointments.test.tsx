import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { GovernanceAppointments } from "./governance-appointments";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-owner" };
const hospital = "22222222-2222-4222-8222-222222222222";
const member = {
  id: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa",
  subject: "synthetic-reviewer",
  role_key: "staff_member",
  membership_epoch: 7,
  created_at: "2026-11-17T00:00:00Z",
};
const state = vi.hoisted(() => ({ disabled: new Set<string>() }));
vi.mock("@helivanta/api", async (original) => ({
  ...(await original<typeof import("@helivanta/api")>()),
  useReleaseCapabilities: () => ({
    available: (key: string) => !state.disabled.has(key) && key !== "supplier_collaboration",
  }),
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/workforce/access",
  useSearchParams: () => new URLSearchParams(),
}));
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => {
  vi.unstubAllGlobals();
  state.disabled.clear();
});

function setup(options: { failFirst?: boolean; membersRefused?: boolean; recover?: boolean } = {}) {
  const commands: { url: string; body: Record<string, unknown> }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const headers = new Headers(init?.headers);
      expect(headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
      expect(headers.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
      if (url.includes("membership-candidates"))
        return options.membersRefused
          ? Response.json({ error: "forbidden" }, { status: 403 })
          : Response.json(page([member]));
      if (url.endsWith("member-presentations"))
        return Response.json({
          tenant_id: scope.tenantId,
          actor_subject: scope.subject,
          data: [
            {
              subject: member.subject,
              available: true,
              name: "QA Reviewer",
              email: "reviewer@example.test",
              roles: [{ key: "staff_member", label: "Staff member" }],
            },
          ],
        });
      if (url.includes("/workforce/hospitals?"))
        return Response.json(
          page([{ id: hospital, name: "Main branch", access_level: "manager" }]),
        );
      if (url.includes("governance-grants")) {
        const body = JSON.parse(init?.body as string);
        commands.push({ url, body });
        if (options.failFirst && commands.length === 1)
          return Response.json({ error: "unavailable" }, { status: 503 });
        return Response.json({
          id: url.split("/").at(-1),
          tenant_id: scope.tenantId,
          input: body,
          status: "provisional_active",
          version: 1,
          issued_by: scope.subject,
          owner_count: 1,
          authorization_mode: "single_owner_provisional",
          created_at: "2026-11-17T00:00:00Z",
          updated_at: "2026-11-17T00:00:00Z",
        });
      }
      throw new Error("Unexpected request: " + url);
    }),
  );
  return {
    ...renderWithProviders(
      <NavigationGuardProvider>
        <GovernanceAppointments scope={scope} />
      </NavigationGuardProvider>,
    ),
    commands,
  };
}

async function prepare(user: ReturnType<typeof setup>["user"]) {
  await user.click(await screen.findByRole("combobox", { name: "Reviewer account" }));
  await user.click(
    await screen.findByRole("option", { name: /QA Reviewer.*reviewer@example.test.*Staff member/ }),
  );
  await user.click(screen.getByRole("combobox", { name: "Hospital" }));
  await user.click(await screen.findByRole("option", { name: "Main branch" }));
  await user.type(screen.getByLabelText("Starts at (UTC)"), "2026-11-17T15:00");
  await user.type(screen.getByLabelText("Expires at (UTC)"), "2026-11-17T16:00");
  await user.type(
    screen.getByLabelText("Appointment reason"),
    "Review synthetic Staff account links",
  );
  await user.click(screen.getByLabelText(/I have reviewed the account/));
}

it("appoints a named member with the selected scope and original membership epoch", async () => {
  const { user, commands } = setup();
  await prepare(user);
  await user.click(screen.getByRole("button", { name: "Submit appointment" }));
  await waitFor(() => expect(commands).toHaveLength(1));
  expect(commands[0].body).toEqual({
    subject: member.subject,
    expected_membership_epoch: 7,
    template_key: "workforce.account_link_reviewer.v1",
    scope_kind: "hospital",
    scope_id: hospital,
    reason: "Review synthetic Staff account links",
    valid_from: "2026-11-17T15:00:00.000Z",
    expires_at: "2026-11-17T16:00:00.000Z",
  });
  expect(await screen.findByText(/Provisional authority/)).toBeInTheDocument();
  expect(screen.queryByText(member.subject)).not.toBeInTheDocument();
});

it("retries an uncertain response with the same command, payload, epoch and validity", async () => {
  const { user, commands } = setup({ failFirst: true });
  await prepare(user);
  await user.click(screen.getByRole("button", { name: "Submit appointment" }));
  await user.click(await screen.findByRole("button", { name: "Retry original appointment" }));
  await screen.findByText(/Provisional authority/);
  expect(commands).toHaveLength(2);
  expect(commands[1]).toEqual(commands[0]);
});

it("keeps unreleased operating roles and supplier scopes out of the form", async () => {
  const { user, commands } = setup();
  await user.click(await screen.findByRole("combobox", { name: "Review role" }));
  expect(
    screen.queryByRole("option", { name: "Independent employer operating-access reviewer" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("option", { name: "Staff account-link reviewer" }));
  await user.click(screen.getByRole("combobox", { name: "Appointment scope" }));
  expect(screen.queryByRole("option", { name: "Supplier" })).not.toBeInTheDocument();
  expect(commands).toHaveLength(0);
});

it("withholds private account choices on authority refusal", async () => {
  const { commands } = setup({ membersRefused: true });
  expect((await screen.findAllByRole("alert")).length).toBeGreaterThan(0);
  expect(screen.queryByRole("combobox", { name: "Reviewer account" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Submit appointment" })).toBeDisabled();
  expect(commands).toHaveLength(0);
});

it("does not offer pharmacy scope when Pharmacy Staff is disabled", async () => {
  state.disabled.add("pharmacy_staff");
  const { user } = setup();
  await user.click(await screen.findByRole("combobox", { name: "Appointment scope" }));
  expect(screen.queryByRole("option", { name: "Pharmacy" })).not.toBeInTheDocument();
  expect(screen.getByRole("option", { name: "Hospital" })).toBeInTheDocument();
});

it("refuses an expiry before the start without creating a command", async () => {
  const { user, commands } = setup();
  await prepare(user);
  await user.clear(screen.getByLabelText("Expires at (UTC)"));
  await user.type(screen.getByLabelText("Expires at (UTC)"), "2026-11-17T14:00");
  await user.click(screen.getByRole("button", { name: "Submit appointment" }));
  expect(await screen.findByText(/Expiry must be after/)).toBeInTheDocument();
  expect(commands).toHaveLength(0);
});

it("shows an inline error for an overlong appointment reason without creating a command", async () => {
  const { user, commands } = setup();
  await prepare(user);
  const reason = screen.getByLabelText("Appointment reason");
  await user.clear(reason);
  await user.click(reason);
  await user.paste("x".repeat(501));
  await user.click(screen.getByRole("button", { name: "Submit appointment" }));
  expect(await screen.findByText(/500 characters/)).toBeInTheDocument();
  expect(commands).toHaveLength(0);
});
