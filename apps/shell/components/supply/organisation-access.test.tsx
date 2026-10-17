import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { OrganisationAccess } from "./organisation-access";
const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  subject: "TEST-owner",
  partyId: "0199e000-0000-7000-8000-000000000001",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
function fixture(perms: string[], roles: string[]) {
  const fetcher = vi.fn(async (url: string) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({ tenant_id: scope.tenantId, subject: scope.subject, data: perms }),
      );
    if (url.endsWith("/access")) return new Response(JSON.stringify({ roles }));
    return new Response(JSON.stringify(page([])));
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
it("never loads private access data for an auditor despite global permission", async () => {
  const fetcher = fixture(["supply.party.access.manage"], ["auditor"]);
  renderWithProviders(<OrganisationAccess scope={scope} roles={["auditor"]} />);
  await waitFor(() => expect(fetcher).toHaveBeenCalled());
  expect(fetcher.mock.calls).toHaveLength(1);
  expect(screen.queryByRole("region", { name: "Organisation access" })).not.toBeInTheDocument();
});
it("explains missing tenant member selection permission and does not browse IAM", async () => {
  const fetcher = fixture(["supply.party.access.manage"], ["administrator"]);
  renderWithProviders(<OrganisationAccess scope={scope} roles={["administrator"]} />);
  await userEvent.click(await screen.findByRole("button", { name: "Add time-limited grant" }));
  expect(
    await screen.findByText(/Tenant administrator permission is required to select a new member/),
  ).toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => url.includes("/iam/members"))).toBe(false);
});

it("uses trusted members and keeps grant identity and payload stable when retrying a failed save", async () => {
  const writes: Array<{ url: string; body: string }> = [];
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          data: ["supply.party.access.manage", "iam.member.manage", "supply.premise.read"],
        }),
      );
    if (init?.method === "PUT") {
      writes.push({ url, body: String(init.body) });
      return new Response(
        JSON.stringify({ error: "unavailable", message: "TEST save unavailable" }),
        { status: 500 },
      );
    }
    if (url.includes("/iam/members"))
      return new Response(
        JSON.stringify(
          page([
            {
              id: "TEST-membership",
              subject: "TEST-sales",
              role_key: "member",
              created_at: "2026-10-17T00:00:00Z",
            },
          ]),
        ),
      );
    return new Response(JSON.stringify(page([])));
  });
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<OrganisationAccess scope={scope} roles={["administrator"]} />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Add time-limited grant" }));
  await user.click(screen.getByLabelText("Tenant member"));
  await user.click(await screen.findByRole("option", { name: "TEST-sales" }));
  const { fireEvent } = await import("@testing-library/react");
  fireEvent.change(screen.getByLabelText("Valid from"), { target: { value: "2026-10-17T12:00" } });
  fireEvent.change(screen.getByLabelText("Expires at"), { target: { value: "2026-10-18T12:00" } });
  await user.type(screen.getByLabelText("Reason for access"), "TEST approved audit review");
  await user.click(screen.getByRole("button", { name: "Save access grant" }));
  expect(await screen.findByText("TEST save unavailable")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Save access grant" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(JSON.parse(writes[0].body)).toMatchObject({
    expected_version: 0,
    subject: "TEST-sales",
    role: "auditor",
    premise_id: null,
    status: "active",
  });
  expect(writes[0].url).toMatch(/\/grants\/[0-9a-f]{8}-[0-9a-f]{4}-7/);
});

it("displays the server effective status and prevents expiry before the start", async () => {
  const writes: unknown[] = [];
  const grant = {
    id: "0199e000-0000-7000-8000-000000000030",
    party_id: scope.partyId,
    subject: "TEST-auditor",
    role: "auditor",
    premise_id: null,
    valid_from: "2026-10-17T00:00:00Z",
    expires_at: "2026-10-18T00:00:00Z",
    reason: "TEST approved audit",
    status: "active",
    effective_status: "scheduled",
    version: 1,
    created_at: "2026-10-17T00:00:00Z",
    updated_at: "2026-10-17T00:00:00Z",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["supply.party.access.manage"],
          }),
        );
      if (init?.method === "PUT") writes.push(init.body);
      if (url.endsWith("/grants") && !url.endsWith("/access/grants"))
        return new Response(JSON.stringify(page([grant])));
      return new Response(JSON.stringify(page([])));
    }),
  );
  renderWithProviders(<OrganisationAccess scope={scope} roles={["administrator"]} />);
  expect(await screen.findByText(/auditor · scheduled · Version 1/)).toBeInTheDocument();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Manage TEST-auditor auditor grant" }));
  const { fireEvent } = await import("@testing-library/react");
  fireEvent.change(screen.getByLabelText("Expires at"), { target: { value: "2026-10-16T00:00" } });
  await user.click(screen.getByRole("button", { name: "Save access grant" }));
  expect(await screen.findByText(/Expiry must be after the start/)).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  expect(screen.getByLabelText("Tenant member")).toBeDisabled();
  expect(screen.getByLabelText("Organisation role")).toBeDisabled();
});

it("clears a selected location when choosing a role that is organisation-wide only", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["supply.party.access.manage", "supply.premise.read"],
          }),
        );
      if (url.endsWith("/premises"))
        return new Response(
          JSON.stringify(
            page([
              {
                id: "0199e000-0000-7000-8000-000000000070",
                code: "TEST-WH",
                name: "TEST Warehouse",
                kind: "warehouse",
                status: "active",
              },
            ]),
          ),
        );
      if (url.includes("/premises/"))
        return new Response(
          JSON.stringify({
            id: "0199e000-0000-7000-8000-000000000070",
            code: "TEST-WH",
            name: "TEST Warehouse",
            kind: "warehouse",
            status: "active",
          }),
        );
      return new Response(JSON.stringify(page([])));
    }),
  );
  renderWithProviders(<OrganisationAccess scope={scope} roles={["administrator"]} />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Add time-limited grant" }));
  await user.click(screen.getByLabelText("Location scope"));
  await user.click(await screen.findByRole("option", { name: "TEST-WH · TEST Warehouse" }));
  await user.click(screen.getByLabelText("Organisation role"));
  await user.click(await screen.findByRole("option", { name: "finance" }));
  expect(screen.getByLabelText("Location scope")).toBeDisabled();
  expect(screen.getByLabelText("Location scope")).toHaveTextContent("Entire organisation");
});

it("allows revoking a historical grant even when its immutable location is no longer available", async () => {
  const grant = {
    id: "0199e000-0000-7000-8000-000000000030",
    party_id: scope.partyId,
    subject: "TEST-regulator",
    role: "regulator",
    premise_id: "0199e000-0000-7000-8000-000000000070",
    valid_from: "2026-10-17T00:00:00Z",
    expires_at: "2026-10-18T00:00:00Z",
    reason: "TEST historical warehouse inspection",
    status: "active",
    effective_status: "active",
    version: 1,
    created_at: "2026-10-17T00:00:00Z",
    updated_at: "2026-10-17T00:00:00Z",
  };
  const writes: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({ data: ["supply.party.access.manage", "supply.premise.read"] }),
        );
      if (init?.method === "PUT") {
        writes.push(JSON.parse(String(init.body)));
        return new Response(
          JSON.stringify({ ...grant, status: "revoked", effective_status: "revoked", version: 2 }),
        );
      }
      if (url.includes("/premises/"))
        return new Response(JSON.stringify({ error: "not_found", message: "Not found" }), {
          status: 404,
        });
      if (url.endsWith("/grants") && !url.endsWith("/access/grants"))
        return new Response(JSON.stringify(page([grant])));
      return new Response(JSON.stringify(page([])));
    }),
  );
  renderWithProviders(<OrganisationAccess scope={scope} roles={["administrator"]} />);
  const user = userEvent.setup();
  await user.click(
    await screen.findByRole("button", { name: "Manage TEST-regulator regulator grant" }),
  );
  expect(await screen.findByText(/The selected location is unavailable/)).toBeInTheDocument();
  await user.click(screen.getByLabelText("Grant status"));
  await user.click(await screen.findByRole("option", { name: "Revoke access" }));
  expect(screen.getByRole("button", { name: "Save access grant" })).toBeEnabled();
  await user.click(screen.getByRole("button", { name: "Save access grant" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toMatchObject({
    expected_version: 1,
    subject: "TEST-regulator",
    premise_id: grant.premise_id,
    status: "revoked",
  });
});

it("offers a regulator only active warehouses or depots and requires a location", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({ data: ["supply.party.access.manage", "supply.premise.read"] }),
        );
      if (url.endsWith("/premises"))
        return new Response(
          JSON.stringify(
            page([
              {
                id: "0199e000-0000-7000-8000-000000000070",
                code: "TEST-WH",
                name: "TEST Warehouse",
                kind: "warehouse",
                status: "active",
              },
              {
                id: "0199e000-0000-7000-8000-000000000071",
                code: "TEST-DEPOT",
                name: "TEST Depot",
                kind: "depot",
                status: "active",
              },
              {
                id: "0199e000-0000-7000-8000-000000000072",
                code: "TEST-BRANCH",
                name: "TEST Branch",
                kind: "branch",
                status: "active",
              },
              {
                id: "0199e000-0000-7000-8000-000000000073",
                code: "TEST-CLOSED",
                name: "TEST Closed Warehouse",
                kind: "warehouse",
                status: "inactive",
              },
            ]),
          ),
        );
      return new Response(JSON.stringify(page([])));
    }),
  );
  renderWithProviders(<OrganisationAccess scope={scope} roles={["administrator"]} />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Add time-limited grant" }));
  await user.click(screen.getByLabelText("Organisation role"));
  await user.click(await screen.findByRole("option", { name: "regulator" }));
  await user.click(screen.getByLabelText("Location scope"));
  expect(
    await screen.findByRole("option", { name: "TEST-WH · TEST Warehouse" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("option", { name: "TEST-DEPOT · TEST Depot" })).toBeInTheDocument();
  expect(screen.queryByRole("option", { name: "Entire organisation" })).not.toBeInTheDocument();
  expect(screen.queryByRole("option", { name: /TEST Branch/ })).not.toBeInTheDocument();
  expect(screen.queryByRole("option", { name: /TEST Closed Warehouse/ })).not.toBeInTheDocument();
});
