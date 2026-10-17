import { fireEvent, screen, waitFor } from "@testing-library/react";
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
const defaults = {
  party_id: scope.partyId,
  max_grant_hours: 8784,
  max_support_hours: 24,
  reason: "",
  version: 0,
  created_at: null,
  updated_at: null,
};
afterEach(() => vi.unstubAllGlobals());

it("saves reasoned access limits with optimistic version and refreshes the current policy", async () => {
  let saved = defaults;
  let reads = 0;
  let historyReads = 0;
  const writes: Array<{ url: string; body: unknown; headers: unknown }> = [];
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
      if (url.endsWith("/access-limits/revisions")) {
        historyReads++;
        return new Response(JSON.stringify(page([])));
      }
      if (url.endsWith("/access-limits")) {
        if (init?.method === "PUT") {
          const body = JSON.parse(String(init.body));
          writes.push({ url, body, headers: init.headers });
          saved = { ...defaults, ...body, version: 1 };
          return new Response(JSON.stringify(saved));
        }
        reads++;
        return new Response(JSON.stringify(saved));
      }
      return new Response(JSON.stringify(page([])));
    }),
  );
  renderWithProviders(<OrganisationAccess scope={scope} roles={["administrator"]} />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "View access limit history" }));
  expect(await screen.findByText("No organisation-specific changes recorded.")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Edit access limits" }));
  expect(screen.getByLabelText("Maximum grant duration (hours)")).toHaveValue(8784);
  expect(screen.getByLabelText("Maximum support duration (hours)")).toHaveValue(24);
  fireEvent.change(screen.getByLabelText("Maximum grant duration (hours)"), {
    target: { value: "72" },
  });
  fireEvent.change(screen.getByLabelText("Maximum support duration (hours)"), {
    target: { value: "4" },
  });
  await user.type(
    screen.getByLabelText("Reason for policy change"),
    "TEST approved reduced access window",
  );
  await user.click(screen.getByRole("button", { name: "Save access limits" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toMatchObject({
    url: expect.stringContaining(`/supply/parties/${scope.partyId}/access-limits`),
    body: {
      expected_version: 0,
      max_grant_hours: 72,
      max_support_hours: 4,
      reason: "TEST approved reduced access window",
    },
  });
  await waitFor(() =>
    expect(screen.queryByLabelText("Reason for policy change")).not.toBeInTheDocument(),
  );
  await waitFor(() => expect(reads).toBeGreaterThan(1));
  await waitFor(() => expect(historyReads).toBeGreaterThan(1));
  expect(new Headers(writes[0].headers as HeadersInit).get("X-Helivanta-Expected-Tenant")).toBe(
    scope.tenantId,
  );
  expect(screen.getByText("72 hours")).toBeInTheDocument();
  expect(screen.getByText("4 hours")).toBeInTheDocument();
  expect(screen.getByText(/Existing grants keep their recorded expiry/)).toBeInTheDocument();
  expect(screen.getByText(/Independent support approval remains mandatory/)).toBeInTheDocument();
});

it("does not load limits without both tenant permission and an organisation administrator grant", async () => {
  const fetcher = vi.fn(
    async (url: string) =>
      new Response(
        JSON.stringify(
          url.endsWith("permissions")
            ? {
                tenant_id: scope.tenantId,
                subject: scope.subject,
                data: ["supply.party.access.manage"],
              }
            : page([]),
        ),
      ),
  );
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<OrganisationAccess scope={scope} roles={["auditor"]} />);
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
  expect(screen.queryByRole("button", { name: "Edit access limits" })).not.toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => url.includes("/access-limits"))).toBe(false);
});

it("rejects a support ceiling above the fixed platform limit before sending any write", async () => {
  const writes: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return new Response(JSON.stringify({ data: ["supply.party.access.manage"] }));
      if (init?.method === "PUT") writes.push(init.body);
      return new Response(JSON.stringify(url.endsWith("/access-limits") ? defaults : page([])));
    }),
  );
  renderWithProviders(<OrganisationAccess scope={scope} roles={["administrator"]} />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Edit access limits" }));
  fireEvent.change(screen.getByLabelText("Maximum support duration (hours)"), {
    target: { value: "25" },
  });
  await user.type(screen.getByLabelText("Reason for policy change"), "TEST approved limit change");
  await user.click(screen.getByRole("button", { name: "Save access limits" }));
  expect(await screen.findByText(/less than or equal to 24/)).toBeInTheDocument();
  expect(screen.getByLabelText("Maximum support duration (hours)")).toHaveAttribute(
    "aria-invalid",
    "true",
  );
  expect(writes).toHaveLength(0);
});

it("retries an ambiguous failed save with identical party, version, reason and limits", async () => {
  const writes: Array<{ url: string; body: string }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return new Response(JSON.stringify({ data: ["supply.party.access.manage"] }));
      if (init?.method === "PUT") {
        writes.push({ url, body: String(init.body) });
        return new Response(
          JSON.stringify({ error: "unavailable", message: "TEST policy save unavailable" }),
          { status: 500 },
        );
      }
      return new Response(
        JSON.stringify(url.endsWith("/access-limits") ? { ...defaults, version: 3 } : page([])),
      );
    }),
  );
  renderWithProviders(<OrganisationAccess scope={scope} roles={["administrator"]} />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Edit access limits" }));
  expect(screen.getByLabelText("Maximum grant duration (hours)")).toHaveValue(8784);
  expect(screen.getByLabelText("Maximum support duration (hours)")).toHaveValue(24);
  await user.type(screen.getByLabelText("Reason for policy change"), "TEST policy security review");
  await user.click(screen.getByRole("button", { name: "Save access limits" }));
  expect(await screen.findByText("TEST policy save unavailable")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Save access limits" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(JSON.parse(writes[0].body)).toMatchObject({
    expected_version: 3,
    reason: "TEST policy security review",
  });
});

it("renders recorded limit changes with actor and timestamp only when history is requested", async () => {
  let historyReads = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("permissions"))
        return new Response(JSON.stringify({ data: ["supply.party.access.manage"] }));
      if (url.endsWith("/access-limits/revisions")) {
        historyReads++;
        return new Response(
          JSON.stringify(
            page([
              {
                id: "TEST-policy-revision",
                party_id: scope.partyId,
                record_id: scope.partyId,
                version: 1,
                actor_subject: "TEST-policy-reviewer",
                created_at: "2026-10-17T01:00:00Z",
                snapshot: {
                  ...defaults,
                  max_grant_hours: 48,
                  max_support_hours: 2,
                  reason: "TEST shorter temporary access",
                  version: 1,
                },
              },
            ]),
          ),
        );
      }
      return new Response(JSON.stringify(url.endsWith("/access-limits") ? defaults : page([])));
    }),
  );
  renderWithProviders(<OrganisationAccess scope={scope} roles={["administrator"]} />);
  const user = userEvent.setup();
  const show = await screen.findByRole("button", { name: "View access limit history" });
  expect(historyReads).toBe(0);
  await user.click(show);
  expect(await screen.findByText(/Actor TEST-policy-reviewer/)).toBeInTheDocument();
  expect(screen.getByText("48 grant hours · 2 support hours")).toBeInTheDocument();
  expect(screen.getByText("TEST shorter temporary access")).toBeInTheDocument();
  expect(historyReads).toBe(1);
  await user.click(screen.getByRole("button", { name: "Hide access limit history" }));
  expect(screen.queryByRole("region", { name: "Access limit history" })).not.toBeInTheDocument();
});

it("hides cached limits and the editor when a refresh is denied", async () => {
  let denied = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("permissions"))
        return new Response(JSON.stringify({ data: ["supply.party.access.manage"] }));
      if (url.endsWith("/access-limits"))
        return denied
          ? new Response(JSON.stringify({ error: "not_found", message: "Not found" }), {
              status: 404,
            })
          : new Response(JSON.stringify(defaults));
      return new Response(JSON.stringify(page([])));
    }),
  );
  renderWithProviders(<OrganisationAccess scope={scope} roles={["administrator"]} />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Edit access limits" }));
  denied = true;
  fireEvent(window, new Event("visibilitychange"));
  expect(await screen.findByText(/Access limits are unavailable/)).toBeInTheDocument();
  expect(screen.queryByLabelText("Maximum grant duration (hours)")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Edit access limits" })).not.toBeInTheDocument();
});

it("does not steal focus from an edited policy reason when a deferred animation frame runs", async () => {
  const frames: FrameRequestCallback[] = [];
  vi.stubGlobal(
    "requestAnimationFrame",
    vi.fn((callback: FrameRequestCallback) => {
      frames.push(callback);
      return frames.length;
    }),
  );
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (url: string) =>
        new Response(
          JSON.stringify(
            url.endsWith("permissions")
              ? { data: ["supply.party.access.manage"] }
              : url.endsWith("/access-limits")
                ? defaults
                : page([]),
          ),
        ),
    ),
  );
  renderWithProviders(<OrganisationAccess scope={scope} roles={["administrator"]} />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Edit access limits" }));
  const reason = screen.getByLabelText("Reason for policy change");
  await user.type(reason, "TEST policy review already in progress");
  const { act } = await import("@testing-library/react");
  act(() => {
    frames.splice(0).forEach((callback) => callback(0));
  });
  expect(reason).toHaveFocus();
  expect(reason).toHaveValue("TEST policy review already in progress");
  expect(screen.getByLabelText("Maximum grant duration (hours)")).toHaveValue(8784);
});
