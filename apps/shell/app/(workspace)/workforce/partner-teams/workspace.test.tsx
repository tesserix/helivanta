import { act, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { beginTenantTransition, cancelTenantTransition } from "@helivanta/api";
import { NavigationGuardProvider } from "@helivanta/ui";
import { PartnerTeamsWorkspace } from "./workspace";
const tenant = "11111111-1111-4111-8111-111111111111";
const hospital = "22222222-2222-4222-8222-222222222222";
const id = "019ad451-1234-7123-8123-123456789012";
const timestamp = "2026-11-06T09:00:00Z";
const group = {
  id,
  hospital_id: hospital,
  name: "Receiving team",
  purpose: "receiving",
  reason: "Synthetic access preparation",
  valid_from: timestamp,
  expires_at: "2027-01-01T09:00:00Z",
  status: "pending_scope",
  version: 1,
  proposed_by: "team-admin",
  membership_policy_version: 1,
  external_access_enabled: false,
  created_at: timestamp,
  updated_at: timestamp,
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
const account = {
  tenant_id: tenant,
  subject: "team-admin",
  data: ["iam.assignment.manage", "iam.assignment.review"],
};
function renderTeams() {
  return renderWithProviders(
    <NavigationGuardProvider>
      <PartnerTeamsWorkspace />
    </NavigationGuardProvider>,
  );
}
afterEach(() => {
  act(() => cancelTenantTransition());
  vi.unstubAllGlobals();
});
it("shows verified account-scoped teams and never treats publication as supplier access", async () => {
  const request = vi.fn(async (url: string, init?: RequestInit) => {
    if (url === "/api/v1/iam/me/permissions") return Response.json(account);
    if (url.startsWith("/api/v1/iam/partner-groups?")) {
      expect(new Headers(init?.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenant);
      expect(new Headers(init?.headers).get("X-Helivanta-Expected-Subject")).toBe(account.subject);
      return Response.json(page([group]));
    }
    if (url.endsWith(`/partner-groups/${id}`)) return Response.json(group);
    throw new Error(`Unexpected request: ${url}`);
  });
  vi.stubGlobal("fetch", request);
  const { user } = renderTeams();
  await user.click(await screen.findByRole("button", { name: "Open Receiving team" }));
  expect(await screen.findByText("Hospital verification pending")).toBeInTheDocument();
  expect(screen.getByText(/A team alone does not share supplier records/)).toBeInTheDocument();
  expect(
    request.mock.calls.some(([url]) => url.includes("/staff") || url.includes("/pharmacy")),
  ).toBe(false);
});
it("recovers a lost committed save using the same retained team identity", async () => {
  let retained: typeof group | null = null;
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === "/api/v1/iam/me/permissions") return Response.json(account);
      if (url.startsWith("/api/v1/workforce/hospitals"))
        return Response.json(
          page([{ id: hospital, name: "Synthetic Hospital", access_level: "manager" }]),
        );
      if (url.startsWith("/api/v1/iam/partner-groups?"))
        return Response.json(page(retained ? [retained] : []));
      if (url.includes("/iam/partner-groups/") && init?.method === "PUT") {
        writes.push(url);
        retained = { ...group, ...JSON.parse(String(init.body)), id: url.split("/").at(-1) };
        throw new TypeError("Synthetic lost committed response");
      }
      if (retained && url.endsWith(`/partner-groups/${retained.id}`))
        return Response.json(retained);
      throw new Error(`Unexpected request: ${url}`);
    }),
  );
  const { user } = renderTeams();
  await user.click(await screen.findByRole("button", { name: "Prepare a team" }));
  await user.click(await screen.findByRole("combobox", { name: "Hospital" }));
  await user.click(await screen.findByRole("option", { name: "Synthetic Hospital" }));
  await user.type(screen.getByRole("textbox", { name: "Team name" }), "Synthetic receiving team");
  await user.type(
    screen.getByRole("textbox", { name: "Reason" }),
    "Reviewed preparation for supplier receiving",
  );
  await user.click(screen.getByRole("button", { name: "Save team proposal" }));
  expect(await screen.findByText(/The save may have been recorded/)).toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "Team name" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Check saved team" }));
  expect(await screen.findByText(/Team proposal recovered/)).toBeInTheDocument();
  expect(writes).toHaveLength(1);
});
it("removes the old account's team data during a tenant transition", async () => {
  let identities = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === "/api/v1/iam/me/permissions")
        return Response.json(
          ++identities === 1
            ? account
            : {
                ...account,
                tenant_id: "33333333-3333-4333-8333-333333333333",
                subject: "another-account",
                data: [],
              },
        );
      if (url.startsWith("/api/v1/iam/partner-groups?")) return Response.json(page([group]));
      if (url.startsWith("/api/v1/workforce/staff-actor-scopes?")) {
        expect(new Headers(init?.headers).get("X-Helivanta-Expected-Tenant")).toBe(
          "33333333-3333-4333-8333-333333333333",
        );
        expect(new Headers(init?.headers).get("X-Helivanta-Expected-Subject")).toBe(
          "another-account",
        );
        return Response.json(page([]));
      }
      throw new Error(`Unexpected request: ${url}`);
    }),
  );
  renderTeams();
  await screen.findByRole("button", { name: "Open Receiving team" });
  act(() => beginTenantTransition());
  expect(screen.queryByText("Receiving team")).not.toBeInTheDocument();
  act(() => cancelTenantTransition());
  await waitFor(() =>
    expect(
      screen.getByText(/No current Staff hospitals are available for membership management/),
    ).toBeInTheDocument(),
  );
  expect(screen.queryByText("Receiving team")).not.toBeInTheDocument();
});

it("recovers a lost publication decision without issuing another command", async () => {
  let current = {
    ...group,
    status: "pending_review",
    version: 2,
    proposed_by: "another-administrator",
  };
  let receipt: Record<string, unknown> | null = null;
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === "/api/v1/iam/me/permissions") return Response.json(account);
      if (url.startsWith("/api/v1/iam/partner-groups?")) return Response.json(page([current]));
      if (url.includes("/commands/") && init?.method === "PUT") {
        writes.push(url);
        const input = JSON.parse(String(init.body));
        current = { ...current, status: "approved", version: 3 };
        receipt = {
          ...input,
          id: url.split("/").at(-1),
          group_id: id,
          result_version: 3,
          actor_subject: account.subject,
          actor_membership_epoch: 1,
          group: current,
          created_at: timestamp,
        };
        throw new TypeError("Synthetic lost committed decision");
      }
      if (url.includes("/commands/")) return Response.json(receipt);
      if (url.endsWith(`/partner-groups/${id}`)) return Response.json(current);
      throw new Error(`Unexpected request: ${url}`);
    }),
  );
  const { user } = renderTeams();
  await user.click(await screen.findByRole("button", { name: "Open Receiving team" }));
  await user.type(
    await screen.findByRole("textbox", { name: "Review reason" }),
    "Independent review of receiving-team purpose",
  );
  await user.click(screen.getByRole("button", { name: "Record decision" }));
  expect(await screen.findByText(/The decision may have been recorded/)).toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "Review reason" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Refresh current team" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Check recorded decision" }));
  expect(await screen.findByText("Recorded team decision recovered.")).toBeInTheDocument();
  expect(await screen.findByText("Publication approved")).toBeInTheDocument();
  expect(writes).toHaveLength(1);
});

it("withholds the old team when its current authority is refused", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url === "/api/v1/iam/me/permissions") return Response.json(account);
      if (url.startsWith("/api/v1/iam/partner-groups?")) return Response.json(page([group]));
      if (url.endsWith(`/partner-groups/${id}`))
        return Response.json(
          { error: "forbidden", message: "Current authority changed" },
          { status: 403 },
        );
      throw new Error(`Unexpected request: ${url}`);
    }),
  );
  const { user } = renderTeams();
  await user.click(await screen.findByRole("button", { name: "Open Receiving team" }));
  await screen.findByRole("button", { name: "Reopen partner teams" });
  expect(screen.queryByText("Receiving team")).not.toBeInTheDocument();
});
