import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { CountryPolicyWorkspace } from "./workspace";

afterEach(() => vi.unstubAllGlobals());
function fixture(
  permissions = ["workspace.country_policy.read", "workspace.country_policy.manage"],
) {
  const fetcher = vi.fn(async (url: string) => {
    if (url.endsWith("/iam/me/permissions"))
      return new Response(
        JSON.stringify({
          tenant_id: "11111111-1111-1111-1111-111111111111",
          subject: "TEST-owner",
          data: permissions,
        }),
      );
    return new Response(JSON.stringify({ data: [], page: { has_more: false, next_cursor: null } }));
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
it("does not query private country policies without read permission", async () => {
  const fetcher = fixture([]);
  renderWithProviders(<CountryPolicyWorkspace />);
  expect(await screen.findByText("Country policy access required")).toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => url.includes("country-policies"))).toBe(false);
});

it("keeps a UTC datetime valid after the control loses focus", async () => {
  fixture();
  HTMLElement.prototype.scrollIntoView = vi.fn();
  const { user } = renderWithProviders(<CountryPolicyWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Create policy" }));
  await user.click(screen.getByLabelText("Effective from (UTC)"));
  fireEvent.change(screen.getByLabelText("Effective from (UTC)"), {
    target: { value: "2026-10-16T00:00" },
  });
  await user.click(screen.getByLabelText("Jurisdiction"));
  await user.type(screen.getByLabelText("Jurisdiction"), "T");
  expect(screen.queryByText("Invalid datetime")).not.toBeInTheDocument();
});

it("submits all Pakistan requirements with PKR and replays an unchanged failed submission", async () => {
  HTMLElement.prototype.scrollIntoView = vi.fn();
  const posts: {
    id: string;
    scope_id: string;
    country: string;
    currency: string;
    sources: unknown[];
    requirements: unknown[];
  }[] = [];
  let stored: Record<string, unknown> | null = null;
  const tenant = "11111111-1111-1111-1111-111111111111";
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("/iam/me/permissions"))
      return new Response(
        JSON.stringify({
          tenant_id: tenant,
          subject: "TEST-owner",
          data: ["workspace.country_policy.read", "workspace.country_policy.manage"],
        }),
      );
    if (init?.method === "POST") {
      expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenant);
      const input = JSON.parse(String(init.body));
      posts.push(input);
      if (posts.length === 1)
        return new Response(
          JSON.stringify({
            error: "unavailable",
            message: "TEST submission temporarily unavailable",
          }),
          { status: 503 },
        );
      stored = input;
      return new Response(
        JSON.stringify({
          id: input.id,
          scope_id: input.scope_id,
          version_id: input.id,
          actor_subject: "TEST-owner",
          action: "submitted",
          reason: "",
          expected_revision: 0,
          result_revision: 1,
          created_at: "2026-10-16T12:00:00Z",
        }),
        { status: 202 },
      );
    }
    if (stored && url.endsWith(String(stored.scope_id)))
      return new Response(
        JSON.stringify({
          ...stored,
          id: stored.scope_id,
          owner_subject: "TEST-owner",
          access_role: "owner",
          status: "unapproved",
          current_version: null,
          revision: 1,
          created_at: "2026-10-16T12:00:00Z",
        }),
      );
    return new Response(JSON.stringify({ data: [], page: { has_more: false, next_cursor: null } }));
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(<CountryPolicyWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Create policy" }));
  await user.click(screen.getByRole("combobox", { name: "Country" }));
  await user.click(await screen.findByRole("option", { name: "Pakistan" }));
  await user.type(screen.getByLabelText("Jurisdiction"), "TEST-PROVINCE");
  fireEvent.change(screen.getByLabelText("Effective from (UTC)"), {
    target: { value: "2026-10-16T00:00" },
  });
  fireEvent.blur(screen.getByLabelText("Effective from (UTC)"));
  fireEvent.change(screen.getByLabelText("Effective until (UTC)"), {
    target: { value: "2027-10-16T00:00" },
  });
  fireEvent.blur(screen.getByLabelText("Effective until (UTC)"));
  await user.click(screen.getByLabelText("Source 1 title"));
  await user.paste("TEST ONLY fictional source");
  await user.type(
    screen.getByLabelText("Source 1 HTTPS citation"),
    "https://regulator.example.invalid/test",
  );
  fireEvent.change(screen.getByLabelText("Source 1 reviewed at (UTC)"), {
    target: { value: "2026-10-15T00:00" },
  });
  fireEvent.blur(screen.getByLabelText("Source 1 reviewed at (UTC)"));
  for (const field of screen.getAllByRole("textbox", { name: /rationale$/ })) {
    await user.click(field);
    await user.paste("TEST ONLY reviewed requirement");
  }
  for (const field of screen.getAllByRole("checkbox")) await user.click(field);
  await user.click(screen.getByRole("button", { name: "Submit policy for review" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "TEST submission temporarily unavailable",
  );
  expect(screen.getByLabelText("Jurisdiction")).toHaveValue("TEST-PROVINCE");
  await user.click(screen.getByRole("button", { name: "Submit policy for review" }));
  await waitFor(() => expect(posts).toHaveLength(2));
  expect(posts[1]).toEqual(posts[0]);
  expect(posts[1]).toMatchObject({
    country: "PK",
    currency: "PKR",
    requirements: expect.any(Array),
  });
  expect(posts[1].requirements).toHaveLength(11);
  expect(
    await screen.findByRole("heading", { name: "Pakistan · TEST-PROVINCE" }),
  ).toBeInTheDocument();
});
it("shows the empty state and focuses the new policy form", async () => {
  fixture();
  HTMLElement.prototype.scrollIntoView = vi.fn();
  const { user } = renderWithProviders(<CountryPolicyWorkspace />);
  expect(
    await screen.findByText("No country policies have been configured for your access."),
  ).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Create policy" }));
  await waitFor(() =>
    expect(screen.getByRole("heading", { name: "New country policy" })).toHaveFocus(),
  );
  expect(screen.getByLabelText("Jurisdiction")).toBeInTheDocument();
  expect(screen.getAllByRole("group", { name: /requirement/ })).toHaveLength(11);
});
