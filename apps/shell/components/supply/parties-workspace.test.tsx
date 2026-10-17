import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { newSupplyPartyInput } from "@helivanta/api";
import { renderWithProviders } from "@helivanta/api/testing";
import { PartiesWorkspace } from "./parties-workspace";
const id = "0199e000-0000-7000-8000-000000000020";
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
function fixture(
  roles: string[] = [],
  allowed = true,
  owned = false,
  extraPermissions: string[] = [],
  operatingRoles?: string[],
) {
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({
          tenant_id: "11111111-1111-1111-1111-111111111111",
          subject: "TEST-owner",
          data: allowed
            ? [
                "supply.directory.read",
                "supply.territory.read",
                "supply.party.read",
                "supply.party.write",
                "supply.organization.manage",
                "supply.premise.read",
                "supply.premise.write",
                "supply.license.read",
                "supply.license.write",
                ...extraPermissions,
              ]
            : [],
        }),
      );
    if (url.endsWith("/corporate-structure"))
      return new Response(
        JSON.stringify({
          party_id: id,
          parent_party_id: null,
          version: 0,
          created_at: null,
          updated_at: null,
        }),
      );
    if (url.endsWith("/access"))
      return new Response(JSON.stringify({ roles, operating_roles: operatingRoles }));
    if (init?.method === "PUT")
      return new Response(JSON.stringify({ error: "internal", message: "TEST save unavailable" }), {
        status: 500,
      });
    if (url.endsWith(`/supply/parties/${id}`))
      return new Response(
        JSON.stringify({
          ...newSupplyPartyInput(),
          id,
          record_kind: owned ? "owned_organisation" : "counterparty",
          code: "TEST-ONE",
          legal_name: "TEST Private Supplier",
          trading_name: "TEST Trade",
          jurisdiction: "TEST-MH",
          registration_number: "TEST-REG-001",
          version: 1,
          onboarding_status: "draft",
          created_at: "2026-10-16T00:00:00Z",
          updated_at: "2026-10-16T00:00:00Z",
        }),
      );
    if (
      url.endsWith("/premises") ||
      url.endsWith("/licenses") ||
      url.endsWith("/territories") ||
      url.endsWith("/support") ||
      url.endsWith("/operating-locations") ||
      url.endsWith("/bins")
    )
      return new Response(JSON.stringify(page([])));
    if (url.endsWith("/revisions")) return new Response(JSON.stringify(page([])));
    if (url.includes("/supply/parties"))
      return new Response(
        JSON.stringify(
          page([
            {
              id,
              code: "TEST-ONE",
              legal_name: "TEST Private Supplier",
              trading_name: "TEST Trade",
              record_kind: owned ? "owned_organisation" : "counterparty",
              business_kind: "distributor",
              country_code: "IN",
              onboarding_status: "draft",
              access_roles: roles,
              operating_roles: operatingRoles,
            },
          ]),
        ),
      );
    return new Response(JSON.stringify(page([])));
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
it("does not query the directory without permission", async () => {
  const fetcher = fixture([], false);
  renderWithProviders(<PartiesWorkspace />);
  expect(await screen.findByText("Supply access required")).toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => url.includes("/supply/"))).toBe(false);
});
it("keeps private detail requests behind explicit access", async () => {
  const fetcher = fixture();
  renderWithProviders(<PartiesWorkspace />);
  await userEvent.click(await screen.findByRole("button", { name: /TEST Private Supplier/ }));
  expect(await screen.findByText("Private profile access required")).toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => url.endsWith(`/parties/${id}`))).toBe(false);
});
it("focuses the new profile and preserves the aggregate identity after a failed save", async () => {
  const fetcher = fixture();
  const user = userEvent.setup();
  const scroll = vi.fn();
  HTMLElement.prototype.scrollIntoView = scroll;
  renderWithProviders(<PartiesWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Add organisation" }));
  await waitFor(() => expect(screen.getByLabelText("Organisation code")).toHaveFocus());
  expect(scroll).toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Save draft profile" }));
  expect(await screen.findAllByRole("alert")).not.toHaveLength(0);
  expect(fetcher.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(0);
  await user.type(screen.getByLabelText("Organisation code"), "TEST-NEW");
  await user.type(screen.getByLabelText("Legal name"), "TEST New Distributor");
  await user.type(screen.getByLabelText("Registration jurisdiction"), "TEST-MH");
  await user.type(screen.getByLabelText("Business registration number"), "TEST-REG-001");
  await user.click(screen.getByRole("button", { name: "Save draft profile" }));
  expect(await screen.findByText("TEST save unavailable")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Save draft profile" }));
  await waitFor(() =>
    expect(fetcher.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(2),
  );
  const writes = fetcher.mock.calls.filter(([, init]) => init?.method === "PUT");
  expect(writes[0][0]).toBe(writes[1][0]);
  expect(writes[0][1]?.body).toBe(writes[1][1]?.body);
  expect(JSON.parse(writes[0][1]?.body as string)).toMatchObject({
    expected_version: 0,
    legal_name: "TEST New Distributor",
    contacts: [],
    addresses: [],
    tax_registrations: [],
  });
});

it("keeps an explicitly granted auditor read-only despite global write permission", async () => {
  const fetcher = fixture(["auditor"]);
  renderWithProviders(<PartiesWorkspace />);
  await userEvent.click(await screen.findByRole("button", { name: /TEST Private Supplier/ }));
  expect(await screen.findByText("TEST-REG-001")).toBeInTheDocument();
  expect(await screen.findByText("No premises recorded.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Edit profile" })).not.toBeInTheDocument();
  expect(fetcher.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(0);
});

it("adds each structured collection and preserves the remaining contact after removal", async () => {
  const fetcher = fixture();
  const user = userEvent.setup();
  renderWithProviders(<PartiesWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Add organisation" }));
  await user.type(screen.getByLabelText("Organisation code"), "TEST-ARRAYS");
  await user.type(screen.getByLabelText("Legal name"), "TEST Structured Distributor");
  await user.type(screen.getByLabelText("Registration jurisdiction"), "TEST-MH");
  await user.type(screen.getByLabelText("Business registration number"), "TEST-ARRAYS-001");
  await user.click(screen.getByRole("button", { name: "Add contact" }));
  await user.type(screen.getByLabelText("Contact name"), "TEST Removed");
  await user.click(screen.getByRole("button", { name: "Add contact" }));
  await user.type(screen.getAllByLabelText("Contact name")[1], "TEST Retained");
  await user.click(screen.getByRole("button", { name: "Remove contact 1" }));
  expect(screen.getByLabelText("Contact name")).toHaveValue("TEST Retained");
  await user.click(screen.getByRole("button", { name: "Add address" }));
  await user.type(screen.getByLabelText("Address line 1"), "TEST Office");
  await user.type(screen.getByLabelText("City"), "TEST City");
  await user.type(screen.getByLabelText("State or province"), "TEST State");
  await user.type(screen.getByLabelText("Postal code"), "TEST-001");
  await user.click(screen.getByRole("button", { name: "Add tax registration" }));
  await user.type(screen.getByLabelText("Tax identifier"), "TEST-TAX");
  await user.type(screen.getByLabelText("Tax jurisdiction"), "TEST-MH");
  await user.click(screen.getByRole("button", { name: "Save draft profile" }));
  expect(await screen.findByText("TEST save unavailable")).toBeInTheDocument();
  const writes = fetcher.mock.calls.filter(([, init]) => init?.method === "PUT");
  expect(writes).toHaveLength(1);
  const saved = JSON.parse(writes[0][1]?.body as string);
  expect(saved.contacts).toHaveLength(1);
  expect(saved.contacts[0].name).toBe("TEST Retained");
  expect(saved.addresses).toHaveLength(1);
  expect(saved.tax_registrations).toHaveLength(1);
  for (const child of [...saved.contacts, ...saved.addresses, ...saved.tax_registrations]) {
    expect(child.id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7/);
    expect(child).not.toHaveProperty("formKey");
  }
});

it("integrates corporate structure only for an owned organisation", async () => {
  const fetcher = fixture(["auditor"], true, true);
  renderWithProviders(<PartiesWorkspace />);
  await userEvent.click(await screen.findByRole("button", { name: /TEST Private Supplier/ }));
  expect(await screen.findByRole("region", { name: "Organisation structure" })).toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Organisation access" })).not.toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => url.endsWith("/grants"))).toBe(false);
});
it("does not load corporate structure or access management for a counterparty", async () => {
  const fetcher = fixture(["administrator"]);
  renderWithProviders(<PartiesWorkspace />);
  await userEvent.click(await screen.findByRole("button", { name: /TEST Private Supplier/ }));
  expect(await screen.findByText("TEST-REG-001")).toBeInTheDocument();
  expect(
    fetcher.mock.calls.some(
      ([url]) => url.endsWith("/corporate-structure") || url.endsWith("/grants"),
    ),
  ).toBe(false);
});
it("shows assigned sales territories without requesting the private organisation profile", async () => {
  const fetcher = fixture(["sales"], true, true);
  renderWithProviders(<PartiesWorkspace />);
  await userEvent.click(await screen.findByRole("button", { name: /TEST Private Supplier/ }));
  expect(await screen.findByRole("region", { name: "Sales territories" })).toBeInTheDocument();
  expect(
    fetcher.mock.calls.some(
      ([url]) =>
        url.endsWith(`/parties/${id}`) ||
        url.endsWith("/corporate-structure") ||
        url.endsWith("/grants"),
    ),
  ).toBe(false);
  expect(screen.queryByRole("button", { name: "Add sales territory" })).not.toBeInTheDocument();
});

it("opens a support-only workspace without requesting ordinary grants or the private profile", async () => {
  const fetcher = fixture([], true, true, ["supply.support.read"]);
  renderWithProviders(<PartiesWorkspace />);
  await userEvent.click(await screen.findByRole("button", { name: /TEST Private Supplier/ }));
  expect(
    await screen.findByRole("region", { name: "Support access requests" }),
  ).toBeInTheDocument();
  expect(
    fetcher.mock.calls.some(
      ([url]) =>
        url.endsWith(`/parties/${id}`) ||
        url.endsWith("/access") ||
        url.endsWith("/corporate-structure") ||
        url.endsWith("/premises"),
    ),
  ).toBe(false);
});
it("opens driver operations without loading private legal or organisation data", async () => {
  const fetcher = fixture(["driver"], true, true, ["supply.delivery.assigned.read"]);
  renderWithProviders(<PartiesWorkspace />);
  await userEvent.click(await screen.findByRole("button", { name: /TEST Private Supplier/ }));
  expect(
    await screen.findByRole("region", { name: "Organisation operations" }),
  ).toBeInTheDocument();
  expect(
    fetcher.mock.calls.some(
      ([url]) =>
        url.endsWith(`/parties/${id}`) ||
        url.endsWith("/corporate-structure") ||
        url.endsWith("/grants") ||
        url.endsWith("/premises"),
    ),
  ).toBe(false);
});

it("refreshes directory and drops the private profile after its own administrator grant is revoked", async () => {
  const base = fixture(["administrator"], true, true, ["supply.party.access.manage"]);
  let revoked = false;
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "DELETE") {
      revoked = true;
      return new Response(JSON.stringify({}));
    }
    if (url.endsWith("/access") && revoked)
      return new Response(JSON.stringify({ error: "not_found", message: "Not found" }), {
        status: 404,
      });
    if (url.endsWith("/access/grants"))
      return revoked
        ? new Response(JSON.stringify({ error: "not_found", message: "Not found" }), {
            status: 404,
          })
        : new Response(
            JSON.stringify(
              page([
                {
                  id: "TEST-own-grant",
                  party_id: id,
                  subject: "TEST-owner",
                  role: "administrator",
                  created_at: "2026-10-17T00:00:00Z",
                },
              ]),
            ),
          );
    if (url.endsWith("/grants")) return new Response(JSON.stringify(page([])));
    return base(url, init);
  });
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<PartiesWorkspace />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: /TEST Private Supplier/ }));
  expect(await screen.findByText("TEST-REG-001")).toBeInTheDocument();
  await user.click(
    await screen.findByRole("button", { name: "Review revocation TEST-owner administrator" }),
  );
  await user.click(screen.getByRole("button", { name: "Confirm revoke earlier grant" }));
  await waitFor(() =>
    expect(screen.queryByRole("button", { name: "Edit profile" })).not.toBeInTheDocument(),
  );
  expect(screen.queryByText("TEST-REG-001")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add time-limited grant" })).not.toBeInTheDocument();
  await waitFor(() =>
    expect(
      fetcher.mock.calls.filter(([url]) => url.includes("/supply/parties?q=")).length,
    ).toBeGreaterThan(1),
  );
  expect(fetcher.mock.calls.filter(([url]) => url.endsWith("/access")).length).toBeGreaterThan(1);
});

it.each([
  ["inventory", "Bins TEST-SCOPED-WH", ["read_bins"]],
  ["driver", "My deliveries TEST-SCOPED-WH", ["read_assigned_deliveries"]],
  ["regulator", "View deliveries TEST-SCOPED-WH", ["read_bins", "read_deliveries"]],
  ["auditor", "Bins TEST-SCOPED-WH", ["read_bins", "read_deliveries"]],
])(
  "opens scoped %s operations even with tenant support permission without promoting private access",
  async (role, action, allowedActions) => {
    const base = fixture(
      [],
      true,
      true,
      [
        "supply.support.read",
        "supply.operations.read",
        "supply.operations.write",
        "supply.delivery.read",
        "supply.delivery.assigned.read",
      ],
      [role as string],
    );
    const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/operating-locations"))
        return new Response(
          JSON.stringify(
            page([
              {
                id: "0199e000-0000-7000-8000-000000000070",
                code: "TEST-SCOPED-WH",
                name: "TEST Assigned Warehouse",
                kind: "warehouse",
                status: "active",
                country_code: "IN",
                allowed_actions: allowedActions,
              },
            ]),
          ),
        );
      return base(url, init);
    });
    vi.stubGlobal("fetch", fetcher);
    renderWithProviders(<PartiesWorkspace />);
    await userEvent.click(await screen.findByRole("button", { name: /TEST Private Supplier/ }));
    expect(
      await screen.findByRole("region", { name: "Organisation operations" }),
    ).toBeInTheDocument();
    const assignedAction = await screen.findByRole("button", { name: action as string });
    expect(assignedAction).toBeInTheDocument();
    if (action === "Bins TEST-SCOPED-WH") {
      await userEvent.click(assignedAction);
      expect(await screen.findByText("No bins recorded for this premise.")).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "Add bin" })).not.toBeInTheDocument();
    }
    expect(screen.getByText(/Assigned operations/)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Close support workspace" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Edit profile" })).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Organisation access" })).not.toBeInTheDocument();
    expect(fetcher.mock.calls.some(([url]) => url.endsWith("/access"))).toBe(true);
    expect(
      fetcher.mock.calls.some(
        ([url]) =>
          url.endsWith(`/parties/${id}`) ||
          url.endsWith("/corporate-structure") ||
          url.endsWith("/premises") ||
          url.endsWith("/grants"),
      ),
    ).toBe(false);
  },
);

it("does not trust stale directory operating roles after current authority is withdrawn", async () => {
  const base = fixture(
    [],
    true,
    true,
    ["supply.support.read", "supply.delivery.assigned.read"],
    ["driver"],
  );
  const fetcher = vi.fn(async (url: string, init?: RequestInit) =>
    url.endsWith("/access")
      ? new Response(JSON.stringify({ error: "not_found", message: "Not found" }), { status: 404 })
      : base(url, init),
  );
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<PartiesWorkspace />);
  await userEvent.click(await screen.findByRole("button", { name: /TEST Private Supplier/ }));
  expect(await screen.findByText(/Organisation access could not be verified/)).toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Organisation operations" })).not.toBeInTheDocument();
  expect(
    fetcher.mock.calls.some(
      ([url]) => url.endsWith(`/parties/${id}`) || url.endsWith("/operating-locations"),
    ),
  ).toBe(false);
});

it("retains scoped inventory work alongside a global finance profile without expanding profile write access", async () => {
  const base = fixture(
    ["finance"],
    true,
    true,
    ["supply.operations.read", "supply.operations.write"],
    ["inventory"],
  );
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("/operating-locations"))
      return new Response(
        JSON.stringify(
          page([
            {
              id: "0199e000-0000-7000-8000-000000000070",
              code: "TEST-MIXED-WH",
              name: "TEST Mixed Warehouse",
              kind: "warehouse",
              status: "active",
              country_code: "IN",
              allowed_actions: ["read_bins", "manage_bins"],
            },
          ]),
        ),
      );
    return base(url, init);
  });
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<PartiesWorkspace />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: /TEST Private Supplier/ }));
  expect(await screen.findByText("TEST-REG-001")).toBeInTheDocument();
  await user.click(await screen.findByRole("button", { name: "Bins TEST-MIXED-WH" }));
  expect(await screen.findByRole("button", { name: "Add bin" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Edit profile" })).not.toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Organisation access" })).not.toBeInTheDocument();
});

it("focuses and reveals the loaded organisation after delayed authority and profile reads without stealing later focus", async () => {
  const base = fixture(["auditor"], true, true);
  let releaseAccess!: () => void;
  let releaseProfile!: () => void;
  const accessReady = new Promise<void>((resolve) => {
    releaseAccess = resolve;
  });
  const profileReady = new Promise<void>((resolve) => {
    releaseProfile = resolve;
  });
  const frames: FrameRequestCallback[] = [];
  vi.stubGlobal(
    "requestAnimationFrame",
    vi.fn((callback: FrameRequestCallback) => {
      frames.push(callback);
      return frames.length;
    }),
  );
  const scroll = vi.fn();
  HTMLElement.prototype.scrollIntoView = scroll;
  let profileVersion = 1;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/access")) await accessReady;
      if (url.endsWith(`/parties/${id}`)) {
        await profileReady;
        const response = await base(url, init);
        return new Response(
          JSON.stringify({ ...(await response.json()), version: profileVersion }),
        );
      }
      return base(url, init);
    }),
  );
  renderWithProviders(<PartiesWorkspace />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: /TEST Private Supplier/ }));
  expect(await screen.findByText("Checking organisation access…")).toBeInTheDocument();
  const { act, fireEvent } = await import("@testing-library/react");
  act(() => {
    frames.splice(0).forEach((callback) => callback(0));
  });
  await act(async () => {
    releaseAccess();
  });
  expect(await screen.findByText("Loading private profile…")).toBeInTheDocument();
  scroll.mockClear();
  await act(async () => {
    releaseProfile();
  });
  const heading = await screen.findByRole("heading", {
    name: "TEST Private Supplier",
  });
  await waitFor(() => expect(heading).toHaveFocus());
  expect(scroll.mock.contexts).toContain(heading);
  const search = screen.getByLabelText("Search by name or code prefix");
  await user.click(search);
  profileVersion = 2;
  fireEvent(window, new Event("visibilitychange"));
  expect(await screen.findByText(/Private profile · Version 2/)).toBeInTheDocument();
  expect(search).toHaveFocus();
  expect(scroll).toHaveBeenCalledTimes(1);
});

it("pins both organisation authority and private profile reads to the selected tenant", async () => {
  const fetcher = fixture(["auditor"], true, true);
  renderWithProviders(<PartiesWorkspace />);
  await userEvent.click(await screen.findByRole("button", { name: /TEST Private Supplier/ }));
  expect(await screen.findByText("TEST-REG-001")).toBeInTheDocument();
  const reads = fetcher.mock.calls.filter(
    ([url]) => url.endsWith("/access") || url.endsWith(`/parties/${id}`),
  );
  expect(reads).toHaveLength(2);
  for (const [, init] of reads)
    expect(new Headers(init?.headers).get("X-Helivanta-Expected-Tenant")).toBe(
      "11111111-1111-1111-1111-111111111111",
    );
});
