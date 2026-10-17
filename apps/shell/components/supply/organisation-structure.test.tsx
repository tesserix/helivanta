import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { OrganisationStructure } from "./organisation-structure";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  subject: "TEST-owner",
  partyId: "0199e000-0000-7000-8000-000000000001",
};
afterEach(() => vi.unstubAllGlobals());
it("does not request private organisation structure without permission", async () => {
  const fetcher = vi.fn(
    async () =>
      new Response(JSON.stringify({ tenant_id: scope.tenantId, subject: scope.subject, data: [] })),
  );
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(
    <OrganisationStructure scope={scope} roles={["administrator"]} country="IN" />,
  );
  await waitFor(() => expect(fetcher).toHaveBeenCalled());
  expect(fetcher.mock.calls).toHaveLength(1);
  expect(screen.queryByRole("region", { name: "Organisation structure" })).not.toBeInTheDocument();
});

it("shows the parent name in the saved relationship without exposing opaque IDs", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["supply.party.read"],
          }),
        );
      if (url.endsWith("/corporate-structure"))
        return new Response(
          JSON.stringify({
            party_id: scope.partyId,
            parent_party_id: "0199e000-0000-7000-8000-000000000002",
            version: 2,
            created_at: "2026-10-17T00:00:00Z",
            updated_at: "2026-10-17T00:00:00Z",
          }),
        );
      return new Response(
        JSON.stringify({
          id: "0199e000-0000-7000-8000-000000000002",
          code: "TEST-PARENT",
          legal_name: "TEST Parent Organisation",
        }),
      );
    }),
  );
  renderWithProviders(<OrganisationStructure scope={scope} roles={["auditor"]} country="IN" />);
  expect(await screen.findByText(/TEST Parent Organisation/)).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Edit corporate relationship" }),
  ).not.toBeInTheDocument();
});

it("keeps a denied corporate read unavailable instead of presenting a new root", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["supply.party.read", "supply.organization.manage"],
          }),
        );
      return new Response(JSON.stringify({ error: "not_found", message: "Not found" }), {
        status: 404,
      });
    }),
  );
  renderWithProviders(
    <OrganisationStructure scope={scope} roles={["administrator"]} country="IN" />,
  );
  expect(await screen.findByText(/corporate relationship is unavailable/)).toBeInTheDocument();
  expect(screen.queryByText(/not been configured/)).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Edit corporate relationship" }),
  ).not.toBeInTheDocument();
});

it("shows territory creation with labelled fields and no permission inheritance", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["supply.party.read", "supply.territory.read", "supply.territory.write"],
          }),
        );
      if (url.endsWith("/corporate-structure"))
        return new Response(
          JSON.stringify({
            party_id: scope.partyId,
            parent_party_id: null,
            version: 0,
            created_at: null,
            updated_at: null,
          }),
        );
      return new Response(
        JSON.stringify({ data: [], page: { has_more: false, next_cursor: null } }),
      );
    }),
  );
  renderWithProviders(
    <OrganisationStructure scope={scope} roles={["administrator"]} country="IN" />,
  );
  const user = (await import("@testing-library/user-event")).default;
  await user.click(await screen.findByRole("button", { name: "Add sales territory" }));
  expect(screen.getByLabelText("Territory code")).toHaveFocus();
  expect(screen.getByLabelText("Territory name")).toBeInTheDocument();
  expect(screen.getByText(/Assignments describe sales coverage/)).toBeInTheDocument();
});

it("preserves an explicit null parent and expected version across failed retries", async () => {
  const writes: Array<{ url: string; body: string }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["supply.party.read", "supply.organization.manage"],
          }),
        );
      if (init?.method === "PUT") {
        writes.push({ url, body: String(init.body) });
        return new Response(
          JSON.stringify({ error: "unavailable", message: "TEST relationship unavailable" }),
          { status: 500 },
        );
      }
      if (url.endsWith("/corporate-structure"))
        return new Response(
          JSON.stringify({
            party_id: scope.partyId,
            parent_party_id: null,
            version: 3,
            created_at: "2026-10-17T00:00:00Z",
            updated_at: "2026-10-17T00:00:00Z",
          }),
        );
      return new Response(
        JSON.stringify({ data: [], page: { has_more: false, next_cursor: null } }),
      );
    }),
  );
  const user = (await import("@testing-library/user-event")).default.setup();
  renderWithProviders(
    <OrganisationStructure scope={scope} roles={["administrator"]} country="IN" />,
  );
  await user.click(await screen.findByRole("button", { name: "Edit corporate relationship" }));
  await user.click(await screen.findByRole("button", { name: "Save corporate relationship" }));
  expect(await screen.findByText(/TEST relationship unavailable/)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Save corporate relationship" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(JSON.parse(writes[0].body)).toEqual({ expected_version: 3, parent_party_id: null });
  expect(screen.getByRole("button", { name: "Reload corporate relationship" })).toBeInTheDocument();
});

it("keeps a new territory identity stable and validates mandatory fields before retry", async () => {
  const writes: Array<{ url: string; body: string }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["supply.territory.read", "supply.territory.write"],
          }),
        );
      if (init?.method === "PUT") {
        writes.push({ url, body: String(init.body) });
        return new Response(
          JSON.stringify({ error: "unavailable", message: "TEST territory unavailable" }),
          { status: 500 },
        );
      }
      return new Response(
        JSON.stringify({ data: [], page: { has_more: false, next_cursor: null } }),
      );
    }),
  );
  const user = (await import("@testing-library/user-event")).default.setup();
  renderWithProviders(
    <OrganisationStructure scope={scope} roles={["administrator"]} country="PK" />,
  );
  await user.click(await screen.findByRole("button", { name: "Add sales territory" }));
  await user.click(screen.getByRole("button", { name: "Save sales territory" }));
  expect(await screen.findAllByRole("alert")).not.toHaveLength(0);
  expect(writes).toHaveLength(0);
  await user.type(screen.getByLabelText("Territory code"), "TEST-LHR");
  await user.type(screen.getByLabelText("Territory name"), "TEST Lahore territory");
  await user.type(screen.getByLabelText("State or province"), "Punjab");
  await user.click(screen.getByRole("button", { name: "Save sales territory" }));
  expect(await screen.findByText("TEST territory unavailable")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Save sales territory" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(JSON.parse(writes[0].body)).toMatchObject({
    expected_version: 0,
    country_code: "PK",
    branch_premise_id: null,
    assigned_sales_subjects: [],
  });
});

it("keeps purchasing read-only and sales restricted to assigned coverage without history or other staff", async () => {
  const fetcher = vi.fn(async (url: string) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          data: ["supply.territory.read", "supply.territory.write"],
        }),
      );
    return new Response(
      JSON.stringify({
        data: [
          {
            id: "0199e000-0000-7000-8000-000000000040",
            code: "TEST-TERRITORY",
            name: "TEST Regional coverage",
            country_code: "IN",
            region: "TEST Region",
            city: "",
            description: "",
            status: "active",
            branch_premise_id: null,
            assigned_sales_subjects: ["TEST-other-sales"],
            version: 1,
          },
        ],
        page: { has_more: false, next_cursor: null },
      }),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  const { unmount } = renderWithProviders(
    <OrganisationStructure scope={scope} roles={["purchasing"]} country="IN" />,
  );
  expect(await screen.findByText("TEST-TERRITORY · TEST Regional coverage")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add sales territory" })).not.toBeInTheDocument();
  unmount();
  renderWithProviders(<OrganisationStructure scope={scope} roles={["sales"]} country="IN" />);
  expect(await screen.findByText("Assigned to you")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /History territory/ })).not.toBeInTheDocument();
  expect(screen.queryByText(/TEST-other-sales/)).not.toBeInTheDocument();
});
