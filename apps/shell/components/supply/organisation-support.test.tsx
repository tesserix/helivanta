import { screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { OrganisationSupport } from "./organisation-support";
const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  subject: "TEST-owner",
  partyId: "0199e000-0000-7000-8000-000000000001",
};
beforeEach(() => {
  vi.spyOn(Date, "now").mockReturnValue(Date.parse("2026-10-17T12:00:00Z"));
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
it("does not request support records without an explicit capability", async () => {
  const fetcher = vi.fn(
    async () =>
      new Response(JSON.stringify({ tenant_id: scope.tenantId, subject: scope.subject, data: [] })),
  );
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<OrganisationSupport scope={scope} roles={["platform_support"]} />);
  await waitFor(() => expect(fetcher).toHaveBeenCalled());
  expect(fetcher.mock.calls).toHaveLength(1);
  expect(screen.queryByRole("region", { name: "Support access requests" })).not.toBeInTheDocument();
});

const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
const request = {
  id: "0199e000-0000-7000-8000-000000000050",
  party_id: scope.partyId,
  premise_id: "0199e000-0000-7000-8000-000000000051",
  beneficiary_subject: scope.subject,
  requester_subject: "TEST-requester",
  approver_subject: "TEST-approver",
  ticket_reference: "TEST-SUPPORT-001",
  reason: "TEST bin count investigation",
  valid_from: "2026-10-17T00:00:00Z",
  expires_at: "2026-10-18T00:00:00Z",
  status: "approved",
  effective_status: "active",
  allowed_actions: ["read_diagnostics"],
  decision_actor: "TEST-approver",
  decision_reason: "TEST approved independently",
  version: 2,
  created_at: "2026-10-17T00:00:00Z",
  updated_at: "2026-10-17T00:00:00Z",
};
it("uses only server allowed actions and displays whitelisted diagnostics without private profile reads", async () => {
  const fetcher = vi.fn(async (url: string) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          data: ["supply.support.read", "supply.support.manage"],
        }),
      );
    if (url.endsWith("/diagnostics"))
      return new Response(
        JSON.stringify({
          support_id: request.id,
          party_id: scope.partyId,
          premise_id: request.premise_id,
          premise_kind: "warehouse",
          premise_status: "active",
          bin_count: 4,
          active_bin_count: 3,
          quarantine_bin_count: 1,
          private_contact: "TEST MUST NEVER DISPLAY",
        }),
      );
    return new Response(JSON.stringify(page([request])));
  });
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<OrganisationSupport scope={scope} roles={[]} />);
  const user = (await import("@testing-library/user-event")).default.setup();
  await user.click(
    await screen.findByRole("button", { name: "Open diagnostics TEST-SUPPORT-001" }),
  );
  expect(
    await screen.findByRole("region", { name: "Read-only support diagnostics" }),
  ).toBeInTheDocument();
  expect(await screen.findByText(/quarantine bin count/i)).toBeInTheDocument();
  expect(screen.queryByText("TEST MUST NEVER DISPLAY")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Request support access" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /Review support/ })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /History support/ })).not.toBeInTheDocument();
  expect(
    fetcher.mock.calls.some(
      ([url]) => url.includes("/premises") || url.endsWith(`/parties/${scope.partyId}`),
    ),
  ).toBe(false);
});
it("keeps rejected or expired diagnostics unavailable rather than showing cached details", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["supply.support.read"],
          }),
        );
      if (url.endsWith("/diagnostics"))
        return new Response(JSON.stringify({ error: "not_found", message: "Not found" }), {
          status: 404,
        });
      return new Response(JSON.stringify(page([request])));
    }),
  );
  renderWithProviders(<OrganisationSupport scope={scope} roles={[]} />);
  const user = (await import("@testing-library/user-event")).default;
  await user.click(
    await screen.findByRole("button", { name: "Open diagnostics TEST-SUPPORT-001" }),
  );
  expect(await screen.findByText(/diagnostic session is no longer available/)).toBeInTheDocument();
  expect(
    screen.queryByRole("region", { name: "Read-only support diagnostics" }),
  ).not.toBeInTheDocument();
});
it("records only an allowed independent decision and retries the same version and payload", async () => {
  const writes: Array<{ url: string; body: string }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["supply.support.read", "supply.support.manage"],
          }),
        );
      if (url.endsWith("/access-limits"))
        return new Response(JSON.stringify({ max_grant_hours: 8784, max_support_hours: 24 }));
      if (init?.method === "POST") {
        writes.push({ url, body: String(init.body) });
        return new Response(
          JSON.stringify({ error: "unavailable", message: "TEST decision unavailable" }),
          { status: 500 },
        );
      }
      return new Response(
        JSON.stringify(
          page([
            {
              ...request,
              status: "pending",
              effective_status: "pending",
              allowed_actions: ["approve"],
            },
          ]),
        ),
      );
    }),
  );
  renderWithProviders(<OrganisationSupport scope={scope} roles={["administrator"]} />);
  const user = (await import("@testing-library/user-event")).default.setup();
  await user.click(await screen.findByRole("button", { name: "Review support TEST-SUPPORT-001" }));
  await user.type(screen.getByLabelText("Decision reason"), "TEST independent approval");
  await user.click(screen.getByRole("button", { name: "Record support decision" }));
  expect(await screen.findByText(/TEST decision unavailable/)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Record support decision" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(JSON.parse(writes[0].body)).toEqual({
    expected_version: 2,
    action: "approve",
    reason: "TEST independent approval",
  });
});
it("terminates an own approved session through the narrow revoke route without an approval action", async () => {
  const writes: Array<{ url: string; body: string }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["supply.support.read", "supply.support.revoke"],
          }),
        );
      if (init?.method === "POST") {
        writes.push({ url, body: String(init.body) });
        return new Response(
          JSON.stringify({ error: "unavailable", message: "TEST revoke unavailable" }),
          { status: 500 },
        );
      }
      return new Response(
        JSON.stringify(page([{ ...request, allowed_actions: ["revoke", "read_diagnostics"] }])),
      );
    }),
  );
  renderWithProviders(<OrganisationSupport scope={scope} roles={[]} />);
  const user = (await import("@testing-library/user-event")).default.setup();
  await user.click(await screen.findByRole("button", { name: "Review support TEST-SUPPORT-001" }));
  await user.type(screen.getByLabelText("Decision reason"), "TEST investigation completed");
  await user.click(screen.getByRole("button", { name: "Record support decision" }));
  expect(await screen.findByText(/TEST revoke unavailable/)).toBeInTheDocument();
  expect(writes).toHaveLength(1);
  expect(writes[0].url).toMatch(/\/support\/[^/]+\/revoke$/);
  expect(JSON.parse(writes[0].body)).toEqual({
    expected_version: 2,
    reason: "TEST investigation completed",
  });
  expect(screen.queryByRole("option", { name: "Approve" })).not.toBeInTheDocument();
});
it("creates a scoped request from trusted options with one stable identity across retries", async () => {
  const writes: Array<{ url: string; body: string }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: [
              "supply.support.read",
              "supply.support.manage",
              "iam.member.manage",
              "supply.premise.read",
            ],
          }),
        );
      if (url.endsWith("/access-limits"))
        return new Response(JSON.stringify({ max_grant_hours: 8784, max_support_hours: 24 }));
      if (init?.method === "PUT") {
        writes.push({ url, body: String(init.body) });
        return new Response(
          JSON.stringify({ error: "unavailable", message: "TEST support request unavailable" }),
          { status: 500 },
        );
      }
      if (url.includes("/iam/members"))
        return new Response(
          JSON.stringify(
            page([
              {
                id: "TEST-member",
                subject: "TEST-beneficiary",
                role_key: "support",
                created_at: "2026-10-17T00:00:00Z",
              },
            ]),
          ),
        );
      if (url.endsWith("/premises"))
        return new Response(
          JSON.stringify(
            page([
              {
                id: request.premise_id,
                code: "TEST-WAREHOUSE",
                name: "TEST Warehouse",
                kind: "warehouse",
                status: "active",
              },
            ]),
          ),
        );
      return new Response(JSON.stringify(page([])));
    }),
  );
  renderWithProviders(<OrganisationSupport scope={scope} roles={["administrator"]} />);
  const user = (await import("@testing-library/user-event")).default.setup();
  await user.click(await screen.findByRole("button", { name: "Request support access" }));
  await user.click(screen.getByLabelText("Support beneficiary"));
  await user.click(await screen.findByRole("option", { name: "TEST-beneficiary" }));
  await user.click(screen.getByLabelText("Location to investigate"));
  await user.click(await screen.findByRole("option", { name: "TEST-WAREHOUSE · TEST Warehouse" }));
  await user.type(screen.getByLabelText("Support ticket reference"), "TEST-CASE-02");
  await user.type(
    screen.getByLabelText("Reason for investigation"),
    "TEST location count mismatch",
  );
  const { fireEvent } = await import("@testing-library/react");
  fireEvent.change(screen.getByLabelText("Support starts at"), {
    target: { value: "2026-10-17T12:00" },
  });
  fireEvent.change(screen.getByLabelText("Support expires at"), {
    target: { value: "2026-10-18T11:00" },
  });
  await user.click(screen.getByRole("button", { name: "Submit support request" }));
  expect(await screen.findByText("TEST support request unavailable")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Submit support request" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(JSON.parse(writes[0].body)).toMatchObject({
    expected_version: 0,
    beneficiary_subject: "TEST-beneficiary",
    premise_id: request.premise_id,
    ticket_reference: "TEST-CASE-02",
  });
  expect(writes[0].url).toMatch(/\/support\/[0-9a-f]{8}-[0-9a-f]{4}-7/);
});

it("hides approved diagnostic counts at the expiry boundary without a focus event", async () => {
  const expires = new Date(Date.now() + 60_000).toISOString();
  let reads = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["supply.support.read"],
          }),
        );
      if (url.endsWith("/diagnostics")) {
        reads++;
        return Date.now() >= Date.parse(expires)
          ? new Response(JSON.stringify({ error: "not_found", message: "Not found" }), {
              status: 404,
            })
          : new Response(
              JSON.stringify({
                support_id: request.id,
                party_id: scope.partyId,
                premise_id: request.premise_id,
                premise_kind: "warehouse",
                premise_status: "active",
                bin_count: 4,
                active_bin_count: 3,
                quarantine_bin_count: 1,
              }),
            );
      }
      return new Response(JSON.stringify(page([{ ...request, expires_at: expires }])));
    }),
  );
  const { act, fireEvent } = await import("@testing-library/react");
  const { unmount } = renderWithProviders(<OrganisationSupport scope={scope} roles={[]} />);
  const open = await screen.findByRole("button", { name: "Open diagnostics TEST-SUPPORT-001" });
  vi.restoreAllMocks();
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-10-17T12:00:00Z"));
  try {
    fireEvent.click(open);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(
      screen.getByRole("region", { name: "Read-only support diagnostics" }),
    ).toBeInTheDocument();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_001);
    });
    expect(
      screen.queryByRole("region", { name: "Read-only support diagnostics" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText(/Support session has expired/)).toBeInTheDocument();
    expect(reads).toBeGreaterThan(1);
  } finally {
    vi.useRealTimers();
    unmount();
  }
});

it("offers support diagnostics only for active warehouses and depots", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            data: ["supply.support.read", "supply.support.manage", "supply.premise.read"],
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
  renderWithProviders(<OrganisationSupport scope={scope} roles={["administrator"]} />);
  const user = (await import("@testing-library/user-event")).default.setup();
  await user.click(await screen.findByRole("button", { name: "Request support access" }));
  await user.click(screen.getByLabelText("Location to investigate"));
  expect(
    await screen.findByRole("option", { name: "TEST-WH · TEST Warehouse" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("option", { name: "TEST-DEPOT · TEST Depot" })).toBeInTheDocument();
  expect(screen.queryByRole("option", { name: /TEST Branch/ })).not.toBeInTheDocument();
  expect(screen.queryByRole("option", { name: /TEST Closed Warehouse/ })).not.toBeInTheDocument();
});
